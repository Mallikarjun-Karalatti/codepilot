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
	ID         int
	ParentID   int
	Text       string
	StartLine  int
	EndLine    int
	Kind       string
	Name       string
	ParentName string
}

const (
	ChunkKindFunction = "function"
	ChunkKindMethod   = "method"
	ChunkKindStruct   = "struct"
)

type CodeChunker struct{}

type CodeDocument struct {
	Chunk     CodeChunk
	Embedding []float64
}

type CodeSearchEngine struct {
	Embedder  Embedder
	Documents []CodeDocument
}

type CodeSearchResult struct {
	Chunk CodeChunk
	Score float64
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

func (c *CodeChunker) Chunk(source string) ([]CodeChunk, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", source, 0)
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
					Text:      source[start:end],
					StartLine: fset.Position(startPos).Line,
					EndLine:   fset.Position(typeSpec.End()).Line,
					Kind:      ChunkKindStruct,
					Name:      typeSpec.Name.Name,
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

func main() {
	source := `
package main

type UserService struct {
    DB   *Database
    Name string
}

type Database struct {
    URL string
}
`

	fset := token.NewFileSet()

	file, err := parser.ParseFile(
		fset,
		"example.go",
		source,
		0,
	)
	if err != nil {
		panic(err)
	}

	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}

			fmt.Println("Struct:", typeSpec.Name.Name)

			for _, field := range structType.Fields.List {
				fmt.Printf("Field: %+v\n", field)
				fmt.Printf("Field type: %T\n", field.Type)
			}
		}
	}
}
