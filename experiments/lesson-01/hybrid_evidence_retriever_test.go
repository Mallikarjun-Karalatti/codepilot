package main

import (
	"os"
	"path/filepath"
	"strings"
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
		SemanticSearch: &CodeSearchEngine{Embedder: assistantTestEmbedder{embedding: []float64{1, 0}}, Documents: documents},
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
	builder := &ContextBuilder{Tokenizer: assistantTestTokenCounter{count: 1}, MaxTokens: 3}
	evidence, err = builder.BuildEvidence(evidence)
	if err != nil {
		t.Fatalf("BuildEvidence() error = %v", err)
	}
	formatted := (&ContextFormatter{}).FormatEvidence(evidence)
	for _, expected := range []string{
		"[METHOD: AuthenticateUser]",
		"Evidence: direct",
		"[STRUCT: AuthService]",
		"Evidence: parent (anchor chunk 2)",
		"[FUNCTION: ValidateToken]",
		"Evidence: callee (anchor chunk 2)",
	} {
		if !strings.Contains(formatted, expected) {
			t.Errorf("formatted evidence %q does not contain %q", formatted, expected)
		}
	}
}
