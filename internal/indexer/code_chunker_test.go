package indexer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"reflect"
	"testing"
)

type codeChunkTestEmbedder struct {
	embeddings map[string][]float64
	calls      []string
}

type mapTokenCounter map[string]int

func (counter mapTokenCounter) CountTokens(text string) (int, error) {
	count, ok := counter[text]
	if !ok {
		return 0, fmt.Errorf("token count not configured for %q", text)
	}
	return count, nil
}

func (e *codeChunkTestEmbedder) Embed(text string) ([]float64, error) {
	e.calls = append(e.calls, text)
	embedding, ok := e.embeddings[text]
	if !ok {
		return nil, fmt.Errorf("embedding not found for %q", text)
	}
	return embedding, nil
}

func TestCodeSearchEngineAdd(t *testing.T) {
	chunks := []CodeChunk{
		{ID: 1, Text: "func Add(a, b int) int { return a + b }", Kind: ChunkKindFunction, Name: "Add"},
		{ID: 2, Text: "type User struct { ID int }", Kind: ChunkKindStruct, Name: "User"},
	}
	embedder := &codeChunkTestEmbedder{
		embeddings: map[string][]float64{
			chunks[0].Text: {0.1, 0.2},
			chunks[1].Text: {0.3, 0.4},
		},
	}
	engine := &CodeSearchEngine{Embedder: embedder}

	if err := engine.Add(chunks); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if len(engine.Documents) != len(chunks) {
		t.Fatalf("Documents length = %d, want %d", len(engine.Documents), len(chunks))
	}
	for i, chunk := range chunks {
		document := engine.Documents[i]
		if document.Chunk != chunk {
			t.Errorf("document %d chunk = %+v, want %+v", i, document.Chunk, chunk)
		}
		wantEmbedding := embedder.embeddings[chunk.Text]
		if len(document.Embedding) != len(wantEmbedding) {
			t.Errorf("document %d embedding length = %d, want %d", i, len(document.Embedding), len(wantEmbedding))
			continue
		}
		for j := range wantEmbedding {
			if document.Embedding[j] != wantEmbedding[j] {
				t.Errorf("document %d embedding[%d] = %v, want %v", i, j, document.Embedding[j], wantEmbedding[j])
			}
		}
	}
	if len(embedder.calls) != len(chunks) || embedder.calls[0] != chunks[0].Text || embedder.calls[1] != chunks[1].Text {
		t.Errorf("Embed() calls = %q, want chunk texts in order", embedder.calls)
	}
}

func TestCodeSearchEngineAddFailurePreservesDocuments(t *testing.T) {
	firstChunk := CodeChunk{ID: 1, Text: "first chunk", Name: "First"}
	failedChunk := CodeChunk{ID: 2, Text: "chunk with no embedding", Name: "Second"}
	initialDocuments := []CodeDocument{{
		Chunk:     CodeChunk{ID: 9, Text: "existing document", Name: "Existing"},
		Embedding: []float64{0.7, 0.8},
	}}
	engine := &CodeSearchEngine{
		Embedder: &codeChunkTestEmbedder{
			embeddings: map[string][]float64{
				firstChunk.Text: {0.1, 0.2},
			},
		},
		Documents: append([]CodeDocument(nil), initialDocuments...),
	}

	err := engine.Add([]CodeChunk{firstChunk, failedChunk})
	if err == nil {
		t.Fatal("Add() error = nil, want embedding error")
	}
	if !reflect.DeepEqual(engine.Documents, initialDocuments) {
		t.Errorf("Documents after failed Add() = %#v, want unchanged %#v", engine.Documents, initialDocuments)
	}
}

func mustParseReceiverType(t *testing.T, source string) ast.Expr {
	t.Helper()
	typ, err := parser.ParseExpr(source)
	if err != nil {
		t.Fatalf("ParseExpr(%q) error = %v", source, err)
	}
	return typ
}

func TestCodeChunker(t *testing.T) {
	chunker := &CodeChunker{}

	t.Run("one function and exact source", func(t *testing.T) {
		source := `package main

func Add(a int, b int) int {
    return a + b
}`
		chunks, err := chunker.Chunk("example.go", source)
		if err != nil {
			t.Fatalf("Chunk() error = %v", err)
		}
		if len(chunks) != 1 {
			t.Fatalf("Chunk() returned %d chunks, want 1", len(chunks))
		}

		wantText := `func Add(a int, b int) int {
    return a + b
}`
		chunk := chunks[0]
		if chunk.Text != wantText {
			t.Errorf("Text = %q, want exact source %q", chunk.Text, wantText)
		}
		if chunk.Kind != "function" {
			t.Errorf("Kind = %q, want %q", chunk.Kind, "function")
		}
		if chunk.Name != "Add" {
			t.Errorf("Name = %q, want %q", chunk.Name, "Add")
		}
		if chunk.StartLine != 3 || chunk.EndLine != 5 {
			t.Errorf("line range = %d-%d, want 3-5", chunk.StartLine, chunk.EndLine)
		}
		if chunk.ID != 1 {
			t.Errorf("ID = %d, want 1", chunk.ID)
		}
	})

	t.Run("multiple functions have sequential IDs", func(t *testing.T) {
		source := `package main

func Add(a, b int) int { return a + b }
func Subtract(a, b int) int { return a - b }
func Multiply(a, b int) int { return a * b }`
		chunks, err := chunker.Chunk("example.go", source)
		if err != nil {
			t.Fatalf("Chunk() error = %v", err)
		}
		wantNames := []string{"Add", "Subtract", "Multiply"}
		if len(chunks) != len(wantNames) {
			t.Fatalf("Chunk() returned %d chunks, want %d", len(chunks), len(wantNames))
		}
		for i, wantName := range wantNames {
			if chunks[i].ID != i+1 {
				t.Errorf("chunk %d ID = %d, want %d", i, chunks[i].ID, i+1)
			}
			if chunks[i].Name != wantName {
				t.Errorf("chunk %d Name = %q, want %q", i, chunks[i].Name, wantName)
			}
			if chunks[i].Kind != "function" {
				t.Errorf("chunk %d Kind = %q, want %q", i, chunks[i].Kind, "function")
			}
		}
	})

	t.Run("functions and receiver methods have correct metadata and sequential IDs", func(t *testing.T) {
		source := `package main

type UserService struct{}

func Add(a, b int) int { return a + b }
func (s UserService) GetUser() {}
func (s *UserService) SaveUser() {}`
		chunks, err := chunker.Chunk("example.go", source)
		if err != nil {
			t.Fatalf("Chunk() error = %v", err)
		}
		if len(chunks) != 4 {
			t.Fatalf("Chunk() returned %d chunks, want 4", len(chunks))
		}

		want := []struct {
			id         int
			name       string
			kind       ChunkKind
			parentName string
			parentID   int
		}{
			{1, "UserService", ChunkKindStruct, "", 0},
			{2, "Add", ChunkKindFunction, "", 0},
			{3, "GetUser", ChunkKindMethod, "UserService", 1},
			{4, "SaveUser", ChunkKindMethod, "UserService", 1},
		}
		for i, expected := range want {
			chunk := chunks[i]
			if chunk.ID != expected.id {
				t.Errorf("chunk %d ID = %d, want %d", i, chunk.ID, expected.id)
			}
			if chunk.Name != expected.name {
				t.Errorf("chunk %d Name = %q, want %q", i, chunk.Name, expected.name)
			}
			if chunk.Kind != expected.kind {
				t.Errorf("chunk %d Kind = %q, want %q", i, chunk.Kind, expected.kind)
			}
			if chunk.ParentName != expected.parentName {
				t.Errorf("chunk %d ParentName = %q, want %q", i, chunk.ParentName, expected.parentName)
			}
			if chunk.ParentID != expected.parentID {
				t.Errorf("chunk %d ParentID = %d, want %d", i, chunk.ParentID, expected.parentID)
			}
		}
	})

	t.Run("preserves source order and resolves method parent declared later", func(t *testing.T) {
		source := `package main

func (u User) GetID() int { return u.ID }
type User struct { ID int }
func NewUser() User { return User{} }`
		chunks, err := chunker.Chunk("example.go", source)
		if err != nil {
			t.Fatalf("Chunk() error = %v", err)
		}
		wantNames := []string{"GetID", "User", "NewUser"}
		if len(chunks) != len(wantNames) {
			t.Fatalf("Chunk() returned %d chunks, want %d", len(chunks), len(wantNames))
		}
		for i, wantName := range wantNames {
			if chunks[i].ID != i+1 || chunks[i].Name != wantName {
				t.Errorf("chunk %d = ID %d, Name %q; want ID %d, Name %q", i, chunks[i].ID, chunks[i].Name, i+1, wantName)
			}
		}
		if chunks[0].ParentName != "User" || chunks[0].ParentID != chunks[1].ID {
			t.Errorf("method parent = %q (ID %d), want User (ID %d)", chunks[0].ParentName, chunks[0].ParentID, chunks[1].ID)
		}
	})

	t.Run("method with multiple receiver fields returns error", func(t *testing.T) {
		source := `package main

type Service struct{}
type Other struct{}

func (s Service, o Other) Run() {}`
		if _, err := chunker.Chunk("example.go", source); err == nil {
			t.Fatal("Chunk() error = nil, want error for multiple receiver fields")
		}
	})

	t.Run("method with unknown receiver returns error", func(t *testing.T) {
		source := `package example

func (s *UnknownService) GetUser() {}`
		if _, err := chunker.Chunk("example.go", source); err == nil {
			t.Fatal("Chunk() error = nil, want error for unknown receiver")
		}
	})

	t.Run("extracts struct with exact source and line numbers", func(t *testing.T) {
		source := `package main

type UserService struct {
    DB *Database
}`
		chunks, err := chunker.Chunk("example.go", source)
		if err != nil {
			t.Fatalf("Chunk() error = %v", err)
		}
		if len(chunks) != 1 {
			t.Fatalf("Chunk() returned %d chunks, want 1", len(chunks))
		}

		chunk := chunks[0]
		wantText := `type UserService struct {
    DB *Database
}`
		if chunk.Text != wantText {
			t.Errorf("Text = %q, want exact source %q", chunk.Text, wantText)
		}
		if chunk.Name != "UserService" {
			t.Errorf("Name = %q, want %q", chunk.Name, "UserService")
		}
		if chunk.Kind != ChunkKindStruct {
			t.Errorf("Kind = %q, want %q", chunk.Kind, ChunkKindStruct)
		}
		if chunk.StartLine != 3 || chunk.EndLine != 5 {
			t.Errorf("line range = %d-%d, want 3-5", chunk.StartLine, chunk.EndLine)
		}
	})

	t.Run("extracts multiple structs from grouped declaration", func(t *testing.T) {
		source := `package main

type (
    UserService struct {
        DB *Database
    }

    Database struct {
        URL string
    }
)`
		chunks, err := chunker.Chunk("example.go", source)
		if err != nil {
			t.Fatalf("Chunk() error = %v", err)
		}
		if len(chunks) != 2 {
			t.Fatalf("Chunk() returned %d chunks, want 2", len(chunks))
		}

		want := []struct {
			name      string
			text      string
			startLine int
			endLine   int
		}{
			{
				name: "UserService",
				text: `UserService struct {
        DB *Database
    }`,
				startLine: 4,
				endLine:   6,
			},
			{
				name: "Database",
				text: `Database struct {
        URL string
    }`,
				startLine: 8,
				endLine:   10,
			},
		}
		for i, expected := range want {
			chunk := chunks[i]
			if chunk.ID != i+1 {
				t.Errorf("chunk %d ID = %d, want %d", i, chunk.ID, i+1)
			}
			if chunk.Name != expected.name {
				t.Errorf("chunk %d Name = %q, want %q", i, chunk.Name, expected.name)
			}
			if chunk.Text != expected.text {
				t.Errorf("chunk %d Text = %q, want exact source %q", i, chunk.Text, expected.text)
			}
			if chunk.StartLine != expected.startLine || chunk.EndLine != expected.endLine {
				t.Errorf("chunk %d line range = %d-%d, want %d-%d", i, chunk.StartLine, chunk.EndLine, expected.startLine, expected.endLine)
			}
		}
	})

	t.Run("mixed functions structs and methods have sequential IDs", func(t *testing.T) {
		source := `package main

type UserID int
type User struct { ID UserID }
func NewUser() User { return User{} }
func (u User) GetID() UserID { return u.ID }
type Settings struct{}`
		chunks, err := chunker.Chunk("example.go", source)
		if err != nil {
			t.Fatalf("Chunk() error = %v", err)
		}
		want := []struct {
			id       int
			name     string
			kind     ChunkKind
			parentID int
		}{
			{1, "User", ChunkKindStruct, 0},
			{2, "NewUser", ChunkKindFunction, 0},
			{3, "GetID", ChunkKindMethod, 1},
			{4, "Settings", ChunkKindStruct, 0},
		}
		if len(chunks) != len(want) {
			t.Fatalf("Chunk() returned %d chunks, want %d", len(chunks), len(want))
		}
		for i, expected := range want {
			if chunks[i].ID != expected.id {
				t.Errorf("chunk %d ID = %d, want %d", i, chunks[i].ID, expected.id)
			}
			if chunks[i].Name != expected.name {
				t.Errorf("chunk %d Name = %q, want %q", i, chunks[i].Name, expected.name)
			}
			if chunks[i].Kind != expected.kind {
				t.Errorf("chunk %d Kind = %q, want %q", i, chunks[i].Kind, expected.kind)
			}
			if chunks[i].ParentID != expected.parentID {
				t.Errorf("chunk %d ParentID = %d, want %d", i, chunks[i].ParentID, expected.parentID)
			}
		}
	})

	t.Run("non-struct types are ignored", func(t *testing.T) {
		source := `package main

type UserID int
type UserName = string
type Handler func()`
		chunks, err := chunker.Chunk("example.go", source)
		if err != nil {
			t.Fatalf("Chunk() error = %v", err)
		}
		if chunks == nil || len(chunks) != 0 {
			t.Errorf("Chunk() = %#v, want []CodeChunk{}", chunks)
		}
	})

	t.Run("invalid Go returns error", func(t *testing.T) {
		if _, err := chunker.Chunk("invalid.go", "package main\n\nfunc Add( {\n"); err == nil {
			t.Fatal("Chunk() error = nil, want parse error")
		}
	})

}

func TestCodeChunkerPreservesSourceFile(t *testing.T) {
	source := `package main

type User struct{}
func Ping() {}
func (u User) Get() {}`
	const filename = "services/user.go"

	chunks, err := (&CodeChunker{}).Chunk(filename, source)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(chunks) != 3 {
		t.Fatalf("Chunk() returned %d chunks, want 3", len(chunks))
	}
	for i, chunk := range chunks {
		if chunk.SourceFile != filename {
			t.Errorf("chunk %d SourceFile = %q, want %q", i, chunk.SourceFile, filename)
		}
	}
}
