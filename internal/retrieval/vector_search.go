package retrieval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	"codepilot/internal/indexer"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string         `json:"model"`
	Messages []Message      `json:"messages"`
	Stream   bool           `json:"stream"`
	Options  map[string]any `json:"options,omitempty"`
}

type ChatResponse struct {
	Model   string  `json:"model"`
	Message Message `json:"message"`
	Done    bool    `json:"done"`
}

type HTTPClient interface {
	Do(request *http.Request) (*http.Response, error)
}

type LLMClient struct {
	BaseURL    string
	Model      string
	Options    map[string]any
	HTTPClient HTTPClient
}

func NewLLMClient(baseURL string, model string) *LLMClient {
	return &LLMClient{
		BaseURL: baseURL,
		Model:   model,
		HTTPClient: &http.Client{
			Timeout: 100 * time.Second,
		},
	}
}

func (client *LLMClient) Chat(messages []Message) (string, error) {
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	requestBody := ChatRequest{
		Model:    client.Model,
		Messages: messages,
		Stream:   false,
		Options:  client.Options,
	}
	data, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, client.BaseURL+"/api/chat", bytes.NewBuffer(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}
	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", err
	}
	return chatResp.Message.Content, nil
}

type EmbeddingClient struct {
	BaseURL    string
	Model      string
	HTTPClient HTTPClient
}

func NewEmbeddingClient(baseURL string, model string) *EmbeddingClient {
	return &EmbeddingClient{
		BaseURL: baseURL,
		Model:   model,
		HTTPClient: &http.Client{
			Timeout: 100 * time.Second,
		},
	}
}

func (client *EmbeddingClient) Embed(text string) ([]float64, error) {
	emb, _, err := client.EmbedWithEvalCount(text)
	return emb, err
}

func (client *EmbeddingClient) EmbedWithEvalCount(text string) ([]float64, *int, error) {
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	reqBody := map[string]string{
		"model": client.Model,
		"input": text,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequest(http.MethodPost, client.BaseURL+"/api/embed", bytes.NewBuffer(data))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, nil, fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}
	var res struct {
		Embeddings      [][]float64 `json:"embeddings"`
		PromptEvalCount *int        `json:"prompt_eval_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, nil, err
	}
	if len(res.Embeddings) != 1 {
		return nil, nil, fmt.Errorf("expected 1 embedding, got %d", len(res.Embeddings))
	}
	return res.Embeddings[0], res.PromptEvalCount, nil
}

func CosineSimilarity(a []float64, b []float64) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("vectors must have the same length")
	}
	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	normA = math.Sqrt(normA)
	normB = math.Sqrt(normB)
	if normA == 0 || normB == 0 {
		return 0, fmt.Errorf("vectors must not be zero")
	}
	return dotProduct / (normA * normB), nil
}

// VectorSearch searches an indexer.CodeSearchEngine by dense cosine similarity.
func VectorSearch(engine *indexer.CodeSearchEngine, query string, limit int) ([]indexer.CodeSearchResult, error) {
	if engine == nil || engine.Embedder == nil {
		return nil, fmt.Errorf("search engine and embedder must not be nil")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be greater than 0")
	}
	if len(engine.Documents) == 0 {
		return []indexer.CodeSearchResult{}, nil
	}
	qVec, err := engine.Embedder.Embed(query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	results := make([]indexer.CodeSearchResult, len(engine.Documents))
	for i, doc := range engine.Documents {
		score, err := CosineSimilarity(qVec, doc.Embedding)
		if err != nil {
			return nil, err
		}
		results[i] = indexer.CodeSearchResult{
			Chunk: doc.Chunk,
			Score: score,
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}
