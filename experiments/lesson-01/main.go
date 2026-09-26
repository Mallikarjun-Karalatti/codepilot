package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type ChatResponse struct {
	Model   string  `json:"model"`
	Message Message `json:"message"`
	Done    bool    `json:"done"`
}

type EmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type LLMClient struct {
	BaseURL    string
	Model      string
	HTTPClient HTTPClient
}

type HTTPClient interface {
	Do(request *http.Request) (*http.Response, error)
}

type EmbeddingClient struct {
	BaseURL    string
	Model      string
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

func NewEmbeddingClient(baseURL string, model string) *EmbeddingClient {
	// your implementation
	return &EmbeddingClient{
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
	}

	data, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}

	request, err := http.NewRequest(
		http.MethodPost,
		client.BaseURL+"/api/chat",
		bytes.NewBuffer(data),
	)
	if err != nil {
		return "", err
	}

	request.Header.Set("Content-Type", "application/json")

	response, err := httpClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	if !(response.StatusCode >= 200 && response.StatusCode <= 299) {
		return "", fmt.Errorf(
			"request failed with status code: %d",
			response.StatusCode,
		)
	}

	var chatResponse ChatResponse

	err = json.NewDecoder(response.Body).Decode(&chatResponse)
	if err != nil {
		return "", err
	}

	return chatResponse.Message.Content, nil
}

func (client *EmbeddingClient) Embed(text string) ([]float64, error) {
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	requestBody := EmbedRequest{
		Model: client.Model,
		Input: text,
	}

	data, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequest(
		http.MethodPost,
		client.BaseURL+"/api/embed",
		bytes.NewBuffer(data),
	)
	if err != nil {
		return nil, err
	}

	request.Header.Set("Content-Type", "application/json")

	response, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if !(response.StatusCode >= 200 && response.StatusCode <= 299) {
		return nil, fmt.Errorf(
			"request failed with status code: %d",
			response.StatusCode,
		)
	}

	var embeddingResponse struct {
		Embeddings [][]float64 `json:"embeddings"`
	}

	err = json.NewDecoder(response.Body).Decode(&embeddingResponse)
	if err != nil {
		return nil, err
	}

	if len(embeddingResponse.Embeddings) != 1 {
		return nil, fmt.Errorf("expected exactly one embedding, got %d",
			len(embeddingResponse.Embeddings))
	}

	return embeddingResponse.Embeddings[0], nil

}

func CosineSimilarity(a []float64, b []float64) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("vectors must have the same length")
	}

	var dotProduct float64
	var normA float64
	var normB float64

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

func main() {
	embeddingClient := NewEmbeddingClient(
		"http://localhost:11434",
		"qwen3-embedding",
	)

	texts := []string{
		"How do I reset my password?",
		"I forgot my password. How can I change it?",
		"The weather is very hot today.",
	}

	embeddings := make([][]float64, len(texts))

	for i, text := range texts {
		embedding, err := embeddingClient.Embed(text)
		if err != nil {
			fmt.Println("Error:", err)
			return
		}

		embeddings[i] = embedding
	}

	similarityAB, err := CosineSimilarity(embeddings[0], embeddings[1])
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	similarityAC, err := CosineSimilarity(embeddings[0], embeddings[2])
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("A vs B:", similarityAB)
	fmt.Println("A vs C:", similarityAC)
}
