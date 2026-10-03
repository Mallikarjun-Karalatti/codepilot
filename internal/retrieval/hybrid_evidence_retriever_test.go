package retrieval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHybridRetrieverPreservesStructuralProvenanceThroughFormatting(t *testing.T) {
	root := t.TempDir()
	source := "package example\n\ntype AuthService struct{}\nfunc (s *AuthService) AuthenticateUser() { ValidateToken() }\nfunc ValidateToken() {}\n"
	if err := os.WriteFile(filepath.Join(root, "auth.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	chunks := []CodeChunk{
		{ID: 1, Text: "type AuthService struct{}", SourceFile: "auth.go", StartLine: 3, EndLine: 3, Kind: ChunkKindStruct, Name: "AuthService"},
		{ID: 2, ParentID: 1, Text: "func (s *AuthService) AuthenticateUser() { ValidateToken() }", SourceFile: "auth.go", StartLine: 4, EndLine: 4, Kind: ChunkKindMethod, Name: "AuthenticateUser", ParentName: "AuthService"},
		{ID: 3, Text: "func ValidateToken() {}", SourceFile: "auth.go", StartLine: 5, EndLine: 5, Kind: ChunkKindFunction, Name: "ValidateToken"},
	}
	graph, err := BuildCodeRelationshipGraph(root, chunks)
	if err != nil {
		t.Fatalf("BuildCodeRelationshipGraph() error = %v", err)
	}
	documents := []CodeDocument{
		{Chunk: chunks[0], Embedding: []float64{0, 1}},
		{Chunk: chunks[1], Embedding: []float64{1, 0}},
		{Chunk: chunks[2], Embedding: []float64{0, 1}},
	}
	tokenizer := CodeAwareTokenizer{}
	index, err := BuildLexicalIndex(chunks, tokenizer)
	if err != nil {
		t.Fatalf("BuildLexicalIndex() error = %v", err)
	}
	retriever := &HybridEvidenceRetriever{
		SemanticSearch: &CodeSearchEngine{Embedder: mockTestEmbedder{vec: []float64{1, 0}}, Documents: documents},
		LexicalScorer:  &LexicalScorer{Tokenizer: tokenizer, Index: index},
		Chunks:         chunks,
		Graph:          graph,
		Selector:       &EvidenceSelector{MaxChunks: 3, Policy: EvidencePolicyDirectFirst},
		CandidateLimit: 1,
		RRFK:           60,
	}
	evidence, err := retriever.Retrieve("AuthenticateUser")
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}

	if len(evidence) != 3 {
		t.Fatalf("got %d evidence items, want 3", len(evidence))
	}
	if evidence[0].Chunk.Name != "AuthenticateUser" || evidence[0].Origin != EvidenceDirect {
		t.Errorf("evidence[0] = %s (origin %s), want AuthenticateUser direct", evidence[0].Chunk.Name, evidence[0].Origin)
	}
	if evidence[1].Chunk.Name != "AuthService" || evidence[1].Origin != EvidenceParent {
		t.Errorf("evidence[1] = %s (origin %s), want AuthService parent", evidence[1].Chunk.Name, evidence[1].Origin)
	}
	if evidence[2].Chunk.Name != "ValidateToken" || evidence[2].Origin != EvidenceCallee {
		t.Errorf("evidence[2] = %s (origin %s), want ValidateToken callee", evidence[2].Chunk.Name, evidence[2].Origin)
	}
}

type mockTestEmbedder struct {
	vec []float64
}

func (m mockTestEmbedder) Embed(string) ([]float64, error) {
	return m.vec, nil
}
