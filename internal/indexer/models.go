package indexer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"sort"
)

// ChunkKind represents the AST syntactic type of a code block.
type ChunkKind string

const (
	ChunkKindFunction ChunkKind = "function"
	ChunkKindMethod   ChunkKind = "method"
	ChunkKindStruct   ChunkKind = "struct"
)

// CodeChunk represents a chunk of source code derived from AST parsing.
type CodeChunk struct {
	ID         int       `json:"id"`
	Text       string    `json:"text"`
	SourceFile string    `json:"source_file"`
	StartLine  int       `json:"start_line"`
	EndLine    int       `json:"end_line"`
	Kind       ChunkKind `json:"kind"`
	Name       string    `json:"name"`
	ParentName string    `json:"parent_name,omitempty"`
	ParentID   int       `json:"parent_id,omitempty"`
}

// Document represents an embedded text document.
type Document struct {
	ID        int
	Text      string
	Embedding []float64
}

// CodeDocument binds a CodeChunk to its dense vector representation.
type CodeDocument struct {
	Chunk     CodeChunk `json:"chunk"`
	Embedding []float64 `json:"embedding"`
}

// CodeSearchResult represents an embedded search result.
type CodeSearchResult struct {
	Chunk CodeChunk
	Score float64
}

// CosineSimilarity computes cosine similarity between two float vectors.
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

// CodeSearchEngine stores embedded code chunks.
type CodeSearchEngine struct {
	Embedder  Embedder
	Documents []CodeDocument
}

func (e *CodeSearchEngine) Add(chunks []CodeChunk) error {
	newDocs := make([]CodeDocument, len(chunks))
	for i, chunk := range chunks {
		vec, err := e.Embedder.Embed(chunk.Text)
		if err != nil {
			return err
		}
		newDocs[i] = CodeDocument{
			Chunk:     chunk,
			Embedding: vec,
		}
	}
	e.Documents = append(e.Documents, newDocs...)
	return nil
}

func (e *CodeSearchEngine) Search(query string, limit int) ([]CodeSearchResult, error) {
	if e == nil || e.Embedder == nil {
		return nil, fmt.Errorf("search engine and embedder must not be nil")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be greater than 0")
	}
	if len(e.Documents) == 0 {
		return []CodeSearchResult{}, nil
	}
	qVec, err := e.Embedder.Embed(query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	results := make([]CodeSearchResult, len(e.Documents))
	for i, doc := range e.Documents {
		score, err := CosineSimilarity(qVec, doc.Embedding)
		if err != nil {
			return nil, err
		}
		results[i] = CodeSearchResult{
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

// Embedder generates dense vector embeddings for input strings.
type Embedder interface {
	Embed(text string) ([]float64, error)
}

// ReceiverTypeName extracts the base identifier name of a method receiver.
func ReceiverTypeName(field *ast.Field) (string, error) {
	return receiverTypeName(field)
}

// CodeChunker chunks Go source files using standard library AST parsing.
type CodeChunker struct{}

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

// Chunk chunks a single Go source file without cross-file context.
func (c *CodeChunker) Chunk(sourceFile string, source string) ([]CodeChunk, error) {
	return c.ChunkWithKnownStructs(sourceFile, source, nil)
}

func (c *CodeChunker) chunk(sourceFile string, source string, knownStructNames map[string]struct{}) ([]CodeChunk, error) {
	return c.ChunkWithKnownStructs(sourceFile, source, knownStructNames)
}

// ChunkWithKnownStructs chunks a Go source file with directory-level symbol resolution.
func (c *CodeChunker) ChunkWithKnownStructs(
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
