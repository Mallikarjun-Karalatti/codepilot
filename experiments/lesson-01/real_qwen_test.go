package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRealCodePilotQuery(t *testing.T) {
	if os.Getenv("CODEPILOT_REAL_OLLAMA") != "1" {
		t.Skip("set CODEPILOT_REAL_OLLAMA=1 to run the local Ollama integration")
	}

	questions := []string{
		"Where is authentication handled?",
		"How does authentication work?",
		"Where is a user's email updated?",
		"How does the application find a user?",
		"Where are password reset emails sent?",
	}
	embedder := NewEmbeddingClient("http://localhost:11434", "qwen3-embedding")
	indexer := &CodeIndexer{Embedder: embedder}
	engine, err := indexer.IndexRepository(filepath.Join("sample-project"))
	if err != nil {
		t.Fatalf("IndexRepository() error = %v", err)
	}
	if len(engine.Documents) == 0 {
		t.Fatal("indexer produced no code documents")
	}
	t.Logf("indexed %d chunks; embedding dimensions=%d", len(engine.Documents), len(engine.Documents[0].Embedding))

	chunks := make([]CodeChunk, len(engine.Documents))
	for i, document := range engine.Documents {
		chunks[i] = document.Chunk
	}
	builder := &ContextBuilder{
		Documents: chunks,
		Tokenizer: embedder,
		MaxTokens: 6000,
	}
	formatter := &ContextFormatter{}

	assistant := &CodeAssistant{
		SearchEngine:   engine,
		ContextBuilder: builder,
		Formatter:      formatter,
		LLM:            NewLLMClient("http://localhost:11434", "qwen3:8b"),
	}

	for _, question := range questions {
		t.Logf("\nQuestion:\n%s", question)

		results, err := engine.Search(question, 5)
		if err != nil {
			t.Fatalf("Search(%q) error = %v", question, err)
		}
		t.Log("Top results:")
		for i, result := range results {
			t.Logf("%d. %.6f %s (%s)", i+1, result.Score, result.Chunk.Name, result.Chunk.SourceFile)
		}

		answer, err := assistant.Ask(question)
		if err != nil {
			t.Fatalf("Ask(%q) error = %v", question, err)
		}
		t.Logf("Final answer:\n%s", answer)
		if answer == "" {
			t.Errorf("Ask(%q) returned an empty answer", question)
		}
	}
}
