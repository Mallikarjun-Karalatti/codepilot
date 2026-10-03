package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"codepilot/internal/indexer"
	"codepilot/internal/retrieval"
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

type EmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
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
		return "", fmt.Errorf("request failed with status code: %d", response.StatusCode)
	}

	var chatResponse ChatResponse
	err = json.NewDecoder(response.Body).Decode(&chatResponse)
	if err != nil {
		return "", err
	}

	return chatResponse.Message.Content, nil
}

type EmbeddingClient struct {
	BaseURL    string
	Model      string
	HTTPClient HTTPClient
}

// NewEmbeddingClient initializes an HTTP client targeting Ollama's /api/embed endpoint.
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
	embedding, _, err := client.embed(text)
	return embedding, err
}

func (client *EmbeddingClient) CountTokens(text string) (int, error) {
	if text == "" {
		return 0, nil
	}

	_, tokenCount, err := client.embed(text)
	if err != nil {
		return 0, err
	}
	if tokenCount == nil {
		return 0, fmt.Errorf("embedding response did not include prompt_eval_count")
	}
	if *tokenCount < 0 {
		return 0, fmt.Errorf("embedding response returned invalid prompt_eval_count: %d", *tokenCount)
	}
	return *tokenCount, nil
}

func (client *EmbeddingClient) embed(text string) ([]float64, *int, error) {
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
		return nil, nil, err
	}

	request, err := http.NewRequest(
		http.MethodPost,
		client.BaseURL+"/api/embed",
		bytes.NewBuffer(data),
	)
	if err != nil {
		return nil, nil, err
	}

	request.Header.Set("Content-Type", "application/json")

	response, err := httpClient.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()

	if !(response.StatusCode >= 200 && response.StatusCode <= 299) {
		return nil, nil, fmt.Errorf("request failed with status code: %d", response.StatusCode)
	}

	var embeddingResponse struct {
		Embeddings      [][]float64 `json:"embeddings"`
		PromptEvalCount *int        `json:"prompt_eval_count"`
	}

	err = json.NewDecoder(response.Body).Decode(&embeddingResponse)
	if err != nil {
		return nil, nil, err
	}

	if len(embeddingResponse.Embeddings) != 1 {
		return nil, nil, fmt.Errorf("expected exactly one embedding, got %d", len(embeddingResponse.Embeddings))
	}

	return embeddingResponse.Embeddings[0], embeddingResponse.PromptEvalCount, nil
}

type TokenCounter interface {
	CountTokens(text string) (int, error)
}

type ContextBuilder struct {
	Documents []indexer.CodeChunk
	Tokenizer TokenCounter
	MaxTokens int
}

// BuildEvidence enforces the context token budget without converting evidence
// back to search results or discarding its retrieval provenance.
func (builder *ContextBuilder) BuildEvidence(evidence []retrieval.EvidenceCandidate) ([]retrieval.EvidenceCandidate, error) {
	if len(evidence) == 0 {
		return []retrieval.EvidenceCandidate{}, nil
	}
	if builder == nil {
		return nil, fmt.Errorf("context builder must not be nil")
	}
	if builder.Tokenizer == nil {
		return nil, fmt.Errorf("tokenizer must not be nil")
	}
	if builder.MaxTokens <= 0 {
		return nil, fmt.Errorf("max tokens must be greater than 0")
	}
	selected := make([]retrieval.EvidenceCandidate, 0, len(evidence))
	seen := make(map[int]struct{}, len(evidence))
	tokensUsed := 0
	for _, candidate := range evidence {
		if _, exists := seen[candidate.Chunk.ID]; exists {
			continue
		}
		tokens, err := builder.Tokenizer.CountTokens(candidate.Chunk.Text)
		if err != nil {
			return nil, fmt.Errorf("count tokens for chunk %d: %w", candidate.Chunk.ID, err)
		}
		if tokens < 0 {
			return nil, fmt.Errorf("token counter returned negative count for chunk %d", candidate.Chunk.ID)
		}
		if tokens > builder.MaxTokens-tokensUsed {
			continue
		}
		selected = append(selected, candidate)
		seen[candidate.Chunk.ID] = struct{}{}
		tokensUsed += tokens
	}
	return selected, nil
}

func (builder *ContextBuilder) Build(results []retrieval.CodeSearchResult) ([]indexer.CodeChunk, error) {
	if len(results) == 0 {
		return []indexer.CodeChunk{}, nil
	}
	if builder.Tokenizer == nil {
		return nil, fmt.Errorf("tokenizer must not be nil")
	}
	if builder.MaxTokens <= 0 {
		return nil, fmt.Errorf("max tokens must be greater than 0")
	}

	documentsByID := make(map[int]indexer.CodeChunk, len(builder.Documents))
	for _, document := range builder.Documents {
		documentsByID[document.ID] = document
	}

	chunksByID := make(map[int]indexer.CodeChunk, len(results)*2)
	tokenCounts := make(map[int]int, len(results)*2)
	tokensUsed := 0
	for _, result := range results {
		chunk := result.Chunk
		if _, included := chunksByID[chunk.ID]; included {
			continue
		}

		candidates := make([]indexer.CodeChunk, 0, 2)
		if chunk.ParentID != 0 {
			if _, parentIncluded := chunksByID[chunk.ParentID]; !parentIncluded {
				parent, ok := documentsByID[chunk.ParentID]
				if !ok {
					return nil, fmt.Errorf(
						"parent chunk %d not found for chunk %d",
						chunk.ParentID,
						chunk.ID,
					)
				}
				candidates = append(candidates, parent)
			}
		}
		candidates = append(candidates, chunk)

		candidateTokens := 0
		for _, candidate := range candidates {
			tokens, cached := tokenCounts[candidate.ID]
			if !cached {
				var err error
				tokens, err = builder.Tokenizer.CountTokens(candidate.Text)
				if err != nil {
					return nil, fmt.Errorf("count tokens for chunk %d: %w", candidate.ID, err)
				}
				if tokens < 0 {
					return nil, fmt.Errorf("token counter returned negative count for chunk %d", candidate.ID)
				}
				tokenCounts[candidate.ID] = tokens
			}
			candidateTokens += tokens
		}

		if candidateTokens > builder.MaxTokens-tokensUsed {
			continue
		}
		for _, candidate := range candidates {
			chunksByID[candidate.ID] = candidate
		}
		tokensUsed += candidateTokens
	}

	chunks := make([]indexer.CodeChunk, 0, len(chunksByID))
	for _, chunk := range chunksByID {
		chunks = append(chunks, chunk)
	}
	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].ID < chunks[j].ID
	})

	return chunks, nil
}

type ContextFormatter struct{}

func (formatter *ContextFormatter) Format(chunks []indexer.CodeChunk) string {
	if len(chunks) == 0 {
		return ""
	}

	sections := make([]string, len(chunks))
	for i, chunk := range chunks {
		sections[i] = formatCodeChunk(chunk, nil)
	}
	return strings.Join(sections, "\n\n")
}

// FormatEvidence keeps retrieval provenance visible to the model alongside
// each chunk's citation metadata.
func (formatter *ContextFormatter) FormatEvidence(evidence []retrieval.EvidenceCandidate) string {
	if len(evidence) == 0 {
		return ""
	}
	sections := make([]string, len(evidence))
	for i, candidate := range evidence {
		sections[i] = formatCodeChunk(candidate.Chunk, &candidate)
	}
	return strings.Join(sections, "\n\n")
}

func formatCodeChunk(chunk indexer.CodeChunk, evidence *retrieval.EvidenceCandidate) string {
	lines := []string{
		fmt.Sprintf("[%s: %s]", strings.ToUpper(string(chunk.Kind)), chunk.Name),
		fmt.Sprintf("Source: %s:%d-%d", chunk.SourceFile, chunk.StartLine, chunk.EndLine),
	}
	if chunk.ParentName != "" {
		lines = append(lines, "Parent: "+chunk.ParentName)
	}
	if evidence != nil {
		provenance := fmt.Sprintf("Evidence: %s", evidence.Origin)
		if evidence.AnchorID != 0 {
			provenance += fmt.Sprintf(" (anchor chunk %d)", evidence.AnchorID)
		}
		lines = append(lines, provenance)
	}
	lines = append(lines, chunk.Text)
	return strings.Join(lines, "\n")
}

type CodeAssistant struct {
	Retriever      *retrieval.HybridEvidenceRetriever
	ContextBuilder *ContextBuilder
	Formatter      *ContextFormatter
	LLM            *LLMClient
}

func NewCodeAssistant(repo *retrieval.IndexedRepository, llm *LLMClient, tokenizer TokenCounter, maxTokens int) (*CodeAssistant, error) {
	if repo == nil {
		return nil, fmt.Errorf("indexed repository must not be nil")
	}
	if llm == nil {
		return nil, fmt.Errorf("LLM client must not be nil")
	}
	if tokenizer == nil {
		return nil, fmt.Errorf("tokenizer must not be nil")
	}
	if maxTokens <= 0 {
		return nil, fmt.Errorf("max tokens must be greater than 0")
	}
	return &CodeAssistant{
		Retriever: repo.Retriever(),
		ContextBuilder: &ContextBuilder{
			Documents: repo.Chunks,
			Tokenizer: tokenizer,
			MaxTokens: maxTokens,
		},
		Formatter: &ContextFormatter{},
		LLM:       llm,
	}, nil
}

func buildPrompt(question, context string) string {
	return fmt.Sprintf(
		"Answer the question using only the code context below. Return the response in exactly this format:\n\nAnswer:\n<your answer>\n\nSources:\n- <source file>:<start line>-<end line> — <function, method, or type name>\n\nCite the context entries that support the important claims in your answer. Copy each source file, line range, and name from the provided context; never invent or infer a source reference. List each source once. If the context does not contain enough information to answer, state that in the Answer section and write `- None` under Sources.\n\nCode context:\n%s\n\nQuestion: %s",
		context,
		question,
	)
}

func (assistant *CodeAssistant) Ask(question string) (string, error) {
	answer, _, err := assistant.AskWithEvidence(question)
	return answer, err
}

func (assistant *CodeAssistant) AskWithEvidence(question string) (string, []retrieval.EvidenceCandidate, error) {
	answer, evidence, _, err := assistant.askWithContext(question)
	return answer, evidence, err
}

func (assistant *CodeAssistant) askWithContext(question string) (string, []retrieval.EvidenceCandidate, string, error) {
	if assistant == nil {
		return "", nil, "", fmt.Errorf("code assistant must not be nil")
	}
	if strings.TrimSpace(question) == "" {
		return "", nil, "", fmt.Errorf("question must not be empty")
	}
	if assistant.Retriever == nil {
		return "", nil, "", fmt.Errorf("hybrid evidence retriever must not be nil")
	}
	if assistant.ContextBuilder == nil {
		return "", nil, "", fmt.Errorf("context builder must not be nil")
	}
	if assistant.Formatter == nil {
		return "", nil, "", fmt.Errorf("context formatter must not be nil")
	}
	if assistant.LLM == nil {
		return "", nil, "", fmt.Errorf("LLM client must not be nil")
	}

	evidence, err := assistant.Retriever.Retrieve(question)
	if err != nil {
		return "", nil, "", fmt.Errorf("retrieve code evidence: %w", err)
	}
	if len(evidence) == 0 {
		return "I couldn't find relevant code for that question.", evidence, "", nil
	}

	evidence, err = assistant.ContextBuilder.BuildEvidence(evidence)
	if err != nil {
		return "", nil, "", fmt.Errorf("build code context: %w", err)
	}
	if len(evidence) == 0 {
		return "I couldn't find relevant code for that question.", evidence, "", nil
	}

	formattedContext := assistant.Formatter.FormatEvidence(evidence)
	answer, err := assistant.LLM.Chat([]Message{{Role: "user", Content: buildPrompt(question, formattedContext)}})
	if err != nil {
		return "", evidence, formattedContext, fmt.Errorf("ask LLM: %w", err)
	}

	return answer, evidence, formattedContext, nil
}
