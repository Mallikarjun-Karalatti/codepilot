package main

import (
	"go/ast"
	"go/parser"
	"testing"
)

func TestReceiverTypeName(t *testing.T) {
	tests := []struct {
		name    string
		typ     ast.Expr
		field   *ast.Field
		want    string
		wantErr bool
	}{
		{
			name: "value receiver",
			typ:  mustParseReceiverType(t, "User"),
			want: "User",
		},
		{
			name: "pointer receiver",
			typ:  mustParseReceiverType(t, "*User"),
			want: "User",
		},
		{
			name: "generic receiver",
			typ:  mustParseReceiverType(t, "User[T]"),
			want: "User",
		},
		{
			name: "multi-type generic receiver",
			typ:  mustParseReceiverType(t, "User[T, U]"),
			want: "User",
		},
		{
			name: "parenthesized receiver",
			typ:  mustParseReceiverType(t, "(User)"),
			want: "User",
		},
		{
			name:    "nil field",
			field:   nil,
			wantErr: true,
		},
		{
			name:    "nil field type",
			field:   &ast.Field{},
			wantErr: true,
		},
		{
			name:    "unsupported receiver",
			typ:     mustParseReceiverType(t, "pkg.User"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := tt.field
			if field == nil && tt.typ != nil {
				field = &ast.Field{Type: tt.typ}
			}

			got, err := receiverTypeName(field)
			if (err != nil) != tt.wantErr {
				t.Fatalf("receiverTypeName() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("receiverTypeName() = %q, want %q", got, tt.want)
			}
		})
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
		chunks, err := chunker.Chunk(source)
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
		chunks, err := chunker.Chunk(source)
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
		chunks, err := chunker.Chunk(source)
		if err != nil {
			t.Fatalf("Chunk() error = %v", err)
		}
		if len(chunks) != 4 {
			t.Fatalf("Chunk() returned %d chunks, want 4", len(chunks))
		}

		want := []struct {
			id         int
			name       string
			kind       string
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
		chunks, err := chunker.Chunk(source)
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
		if _, err := chunker.Chunk(source); err == nil {
			t.Fatal("Chunk() error = nil, want error for multiple receiver fields")
		}
	})

	t.Run("method with unknown receiver returns error", func(t *testing.T) {
		source := `package example

func (s *UnknownService) GetUser() {}`
		if _, err := chunker.Chunk(source); err == nil {
			t.Fatal("Chunk() error = nil, want error for unknown receiver")
		}
	})

	t.Run("extracts struct with exact source and line numbers", func(t *testing.T) {
		source := `package main

type UserService struct {
    DB *Database
}`
		chunks, err := chunker.Chunk(source)
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
		chunks, err := chunker.Chunk(source)
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
		chunks, err := chunker.Chunk(source)
		if err != nil {
			t.Fatalf("Chunk() error = %v", err)
		}
		want := []struct {
			id       int
			name     string
			kind     string
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
		chunks, err := chunker.Chunk(source)
		if err != nil {
			t.Fatalf("Chunk() error = %v", err)
		}
		if chunks == nil || len(chunks) != 0 {
			t.Errorf("Chunk() = %#v, want []CodeChunk{}", chunks)
		}
	})

	t.Run("invalid Go returns error", func(t *testing.T) {
		if _, err := chunker.Chunk("package main\n\nfunc Add( {\n"); err == nil {
			t.Fatal("Chunk() error = nil, want parse error")
		}
	})

}
