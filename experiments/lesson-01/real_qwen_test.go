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

	const question = "Where is authentication handled?"
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

	results, err := engine.Search(question, 5)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	t.Log("Search results:")
	for i, result := range results {
		t.Logf("%d. %.6f %s (%s)", i+1, result.Score, result.Chunk.Name, result.Chunk.SourceFile)
	}

	chunks := make([]CodeChunk, len(engine.Documents))
	for i, document := range engine.Documents {
		chunks[i] = document.Chunk
	}
	builder := &ContextBuilder{
		Documents: chunks,
		Tokenizer: embedder,
		MaxTokens: 6000,
	}
	selected, err := builder.Build(results)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	formatter := &ContextFormatter{}
	formattedContext := formatter.Format(selected)
	prompt := buildPrompt(question, formattedContext)
	t.Logf("Formatted context and prompt:\n%s", prompt)

	assistant := &CodeAssistant{
		SearchEngine:   engine,
		ContextBuilder: builder,
		Formatter:      formatter,
		LLM:            NewLLMClient("http://localhost:11434", "qwen3:8b"),
	}
	answer, err := assistant.Ask(question)
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	t.Logf("Qwen answer:\n%s", answer)
	if answer == "" {
		t.Fatal("Ask() returned an empty answer")
	}
}
