package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
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

type LLMClient struct {
	BaseURL    string
	Model      string
	Options    map[string]any
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

type Document struct {
	ID        int
	Text      string
	Embedding []float64
}

type Embedder interface {
	Embed(text string) ([]float64, error)
}

type SearchEngine struct {
	Embedder  Embedder
	Documents []Document
}

type SearchResult struct {
	Document Document
	Score    float64
}

type Chunker struct {
	MaxLines int
}

type Chunk struct {
	ID         int
	Text       string
	SourceFile string
	StartLine  int
	EndLine    int
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
	embedding, _, err := client.embed(text)
	return embedding, err
}

// CountTokens returns the input token count reported by the configured local
// embedding model. Ollama reports this alongside the embedding response.
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
		return nil, nil, fmt.Errorf(
			"request failed with status code: %d",
			response.StatusCode,
		)
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
		return nil, nil, fmt.Errorf("expected exactly one embedding, got %d",
			len(embeddingResponse.Embeddings))
	}

	return embeddingResponse.Embeddings[0], embeddingResponse.PromptEvalCount, nil

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

func (engine *SearchEngine) Add(text string) error {
	embedding, err := engine.Embedder.Embed(text)
	if err != nil {
		return err
	}

	document := Document{
		ID:        len(engine.Documents) + 1,
		Text:      text,
		Embedding: embedding,
	}

	engine.Documents = append(engine.Documents, document)
	return nil
}

func (engine *SearchEngine) Search(
	query string,
	limit int,
) ([]SearchResult, error) {

	if limit <= 0 {
		return nil, fmt.Errorf("limit must be greater than 0")
	}

	embedding, err := engine.Embedder.Embed(query)
	if err != nil {
		return nil, err
	}

	var results []SearchResult

	for _, doc := range engine.Documents {
		score, err := CosineSimilarity(embedding, doc.Embedding)
		if err != nil {
			return nil, err
		}

		results = append(results, SearchResult{
			Document: doc,
			Score:    score,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func (c *Chunker) Chunk(text string) ([]Chunk, error) {
	if c.MaxLines <= 0 {
		return nil, fmt.Errorf("max lines must be greater than 0")
	}
	if text == "" {
		return []Chunk{}, nil
	}

	// SplitAfter keeps each newline attached to its line, so joining lines back
	// together preserves the original text exactly.
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	chunks := make([]Chunk, 0, (len(lines)+c.MaxLines-1)/c.MaxLines)
	for start := 0; start < len(lines); start += c.MaxLines {
		end := start + c.MaxLines
		if end > len(lines) {
			end = len(lines)
		}
		chunks = append(chunks, Chunk{
			ID:        len(chunks) + 1,
			Text:      strings.Join(lines[start:end], ""),
			StartLine: start + 1,
			EndLine:   end,
		})
	}

	return chunks, nil
}

type CodeChunk struct {
	ID         int    `json:"id"`
	ParentID   int    `json:"parent_id,omitempty"`
	Text       string `json:"text"`
	SourceFile string `json:"source_file"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	ParentName string `json:"parent_name,omitempty"`
}

const (
	ChunkKindFunction = "function"
	ChunkKindMethod   = "method"
	ChunkKindStruct   = "struct"
)

type CodeChunker struct{}

type CodeDocument struct {
	Chunk     CodeChunk `json:"chunk"`
	Embedding []float64 `json:"embedding"`
}

type CodeSearchEngine struct {
	Embedder  Embedder
	Documents []CodeDocument
}

type CodeSearchResult struct {
	Chunk CodeChunk
	Score float64
}

type ContextBuilder struct {
	Documents []CodeChunk
	Tokenizer TokenCounter
	MaxTokens int
}

type ContextFormatter struct{}

func (formatter *ContextFormatter) Format(chunks []CodeChunk) string {
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
func (formatter *ContextFormatter) FormatEvidence(evidence []EvidenceCandidate) string {
	if len(evidence) == 0 {
		return ""
	}
	sections := make([]string, len(evidence))
	for i, candidate := range evidence {
		sections[i] = formatCodeChunk(candidate.Chunk, &candidate)
	}
	return strings.Join(sections, "\n\n")
}

func formatCodeChunk(chunk CodeChunk, evidence *EvidenceCandidate) string {
	lines := []string{
		fmt.Sprintf("[%s: %s]", strings.ToUpper(chunk.Kind), chunk.Name),
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
	Retriever      *HybridEvidenceRetriever
	ContextBuilder *ContextBuilder
	Formatter      *ContextFormatter
	LLM            *LLMClient
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

// AskWithEvidence runs the same assistant flow as Ask and returns the evidence
// used to produce the answer for callers that need to inspect its sources.
func (assistant *CodeAssistant) AskWithEvidence(question string) (string, []EvidenceCandidate, error) {
	answer, evidence, _, err := assistant.askWithContext(question)
	return answer, evidence, err
}

// askWithContext is shared by Ask and the answer-quality evaluator so the
// evaluator can inspect the exact evidence and prompt context used in an answer.
func (assistant *CodeAssistant) askWithContext(question string) (string, []EvidenceCandidate, string, error) {
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

type TokenCounter interface {
	CountTokens(text string) (int, error)
}

// BuildEvidence enforces the context token budget without converting evidence
// back to search results or discarding its retrieval provenance.
func (builder *ContextBuilder) BuildEvidence(evidence []EvidenceCandidate) ([]EvidenceCandidate, error) {
	if len(evidence) == 0 {
		return []EvidenceCandidate{}, nil
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
	selected := make([]EvidenceCandidate, 0, len(evidence))
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

func (builder *ContextBuilder) Build(results []CodeSearchResult) ([]CodeChunk, error) {
	if len(results) == 0 {
		return []CodeChunk{}, nil
	}
	if builder.Tokenizer == nil {
		return nil, fmt.Errorf("tokenizer must not be nil")
	}
	if builder.MaxTokens <= 0 {
		return nil, fmt.Errorf("max tokens must be greater than 0")
	}

	documentsByID := make(map[int]CodeChunk, len(builder.Documents))
	for _, document := range builder.Documents {
		documentsByID[document.ID] = document
	}

	chunksByID := make(map[int]CodeChunk, len(results)*2)
	tokenCounts := make(map[int]int, len(results)*2)
	tokensUsed := 0
	for _, result := range results {
		chunk := result.Chunk
		if _, included := chunksByID[chunk.ID]; included {
			continue
		}

		candidates := make([]CodeChunk, 0, 2)
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

	chunks := make([]CodeChunk, 0, len(chunksByID))
	for _, chunk := range chunksByID {
		chunks = append(chunks, chunk)
	}
	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].ID < chunks[j].ID
	})

	return chunks, nil
}

func (engine *CodeSearchEngine) Add(chunks []CodeChunk) error {
	if engine.Embedder == nil {
		return fmt.Errorf("embedder must not be nil")
	}

	documents := make([]CodeDocument, len(chunks))
	for i, chunk := range chunks {
		embedding, err := engine.Embedder.Embed(chunk.Text)
		if err != nil {
			return fmt.Errorf("embed code chunk %d: %w", chunk.ID, err)
		}
		documents[i] = CodeDocument{
			Chunk:     chunk,
			Embedding: embedding,
		}
	}

	engine.Documents = append(engine.Documents, documents...)
	return nil
}

func (engine *CodeSearchEngine) Search(query string, limit int) ([]CodeSearchResult, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be greater than 0")
	}
	if engine.Embedder == nil {
		return nil, fmt.Errorf("embedder must not be nil")
	}

	queryEmbedding, err := engine.Embedder.Embed(query)
	if err != nil {
		return nil, err
	}

	results := make([]CodeSearchResult, 0, len(engine.Documents))
	for _, document := range engine.Documents {
		score, err := CosineSimilarity(queryEmbedding, document.Embedding)
		if err != nil {
			return nil, err
		}
		results = append(results, CodeSearchResult{
			Chunk: document.Chunk,
			Score: score,
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func receiverTypeName(field *ast.Field) (string, error) {
	if field == nil || field.Type == nil {
		return "", fmt.Errorf("receiver field and type must not be nil")
	}

	typ := field.Type
	for {
		switch t := typ.(type) {
		case *ast.Ident:
			if t.Name == "" {
				return "", fmt.Errorf("receiver type name must not be empty")
			}
			return t.Name, nil
		case *ast.StarExpr:
			typ = t.X
		case *ast.IndexExpr:
			typ = t.X
		case *ast.IndexListExpr:
			typ = t.X
		case *ast.ParenExpr:
			typ = t.X
		default:
			return "", fmt.Errorf("unsupported receiver type %T", typ)
		}
	}
}

func (c *CodeChunker) Chunk(sourceFile string, source string) ([]CodeChunk, error) {
	return c.chunk(sourceFile, source, nil)
}

func (c *CodeChunker) chunk(
	sourceFile string,
	source string,
	knownStructNames map[string]struct{},
) ([]CodeChunk, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, sourceFile, source, 0)
	if err != nil {
		return nil, err
	}

	type positionedChunk struct {
		chunk CodeChunk
		pos   token.Pos
	}

	structChunks := make([]positionedChunk, 0)
	functionChunks := make([]positionedChunk, 0)

	// Collect struct declarations first so their IDs can be looked up after
	// every chunk has been placed in source order.
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if _, ok := typeSpec.Type.(*ast.StructType); !ok {
				continue
			}

			startPos := typeSpec.Pos()
			if len(genDecl.Specs) == 1 {
				startPos = genDecl.Pos()
			}
			fileToken := fset.File(startPos)
			start := fileToken.Offset(startPos)
			end := fileToken.Offset(typeSpec.End())
			structChunks = append(structChunks, positionedChunk{
				pos: startPos,
				chunk: CodeChunk{
					SourceFile: sourceFile,
					Text:       source[start:end],
					StartLine:  fset.Position(startPos).Line,
					EndLine:    fset.Position(typeSpec.End()).Line,
					Kind:       ChunkKindStruct,
					Name:       typeSpec.Name.Name,
				},
			})
		}
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		kind := ChunkKindFunction
		parentName := ""
		if fn.Recv != nil {
			kind = ChunkKindMethod
			if len(fn.Recv.List) != 1 {
				return nil, fmt.Errorf(
					"method %s has %d receiver fields, want exactly one",
					fn.Name.Name,
					len(fn.Recv.List),
				)
			}
			parentName, err = receiverTypeName(fn.Recv.List[0])
			if err != nil {
				return nil, err
			}
		}

		fileToken := fset.File(fn.Pos())
		start := fileToken.Offset(fn.Pos())
		end := fileToken.Offset(fn.End())
		functionChunks = append(functionChunks, positionedChunk{
			pos: fn.Pos(),
			chunk: CodeChunk{
				SourceFile: sourceFile,
				Text:       source[start:end],
				StartLine:  fset.Position(fn.Pos()).Line,
				EndLine:    fset.Position(fn.End()).Line,
				Kind:       kind,
				Name:       fn.Name.Name,
				ParentName: parentName,
			},
		})
	}

	allChunks := append(structChunks, functionChunks...)
	sort.SliceStable(allChunks, func(i, j int) bool {
		return allChunks[i].pos < allChunks[j].pos
	})

	structIDs := make(map[string]int, len(structChunks))
	chunks := make([]CodeChunk, len(allChunks))
	for i, positioned := range allChunks {
		chunk := positioned.chunk
		chunk.ID = i + 1
		chunks[i] = chunk
		if chunk.Kind == ChunkKindStruct {
			structIDs[chunk.Name] = chunk.ID
		}
	}
	for i := range chunks {
		if chunks[i].Kind != ChunkKindMethod {
			continue
		}

		parentID, ok := structIDs[chunks[i].ParentName]
		if !ok {
			if knownStructNames != nil {
				// During multi-file repository indexing, if receiver type is a custom slice,
				// type alias, or decoupled, gracefully leave ParentID = 0.
				continue
			}
			return nil, fmt.Errorf(
				"parent struct %q not found for method %q",
				chunks[i].ParentName,
				chunks[i].Name,
			)
		}

		chunks[i].ParentID = parentID
	}

	return chunks, nil
}
