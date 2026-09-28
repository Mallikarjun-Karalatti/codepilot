package main

import (
	"os"
	"path/filepath"
	"testing"
)

type codeIndexerTestEmbedder struct{}

func (codeIndexerTestEmbedder) Embed(string) ([]float64, error) {
	return []float64{1, 0}, nil
}

func TestCodeIndexerIndexRepository(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"service.go": `package sample

func (s *Service) Run() {}
func Health() {}`,
		"types.go": `package sample

type Service struct{}`,
		"other/types.go": `package other

type Service struct{}
func (s *Service) RunOther() {}`,
		"ignored_test.go": `package sample
func (s *Service) TestOnly() {}`,
	}
	for name, source := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", name, err)
		}
	}

	engine, err := (&CodeIndexer{Embedder: codeIndexerTestEmbedder{}}).IndexRepository(root)
	if err != nil {
		t.Fatalf("IndexRepository() error = %v", err)
	}
	if len(engine.Documents) != 5 {
		t.Fatalf("indexed %d documents, want 5", len(engine.Documents))
	}

	seenIDs := make(map[int]bool)
	chunkByNameAndFile := make(map[string]CodeChunk)
	for _, document := range engine.Documents {
		chunk := document.Chunk
		if seenIDs[chunk.ID] {
			t.Errorf("duplicate chunk ID %d", chunk.ID)
		}
		seenIDs[chunk.ID] = true
		if len(document.Embedding) != 2 {
			t.Errorf("chunk %q embedding length = %d, want 2", chunk.Name, len(document.Embedding))
		}
		chunkByNameAndFile[chunk.SourceFile+":"+chunk.Name] = chunk
	}
	for id := 1; id <= len(engine.Documents); id++ {
		if !seenIDs[id] {
			t.Errorf("chunk ID %d missing from indexed documents", id)
		}
	}

	service := chunkByNameAndFile["types.go:Service"]
	run := chunkByNameAndFile["service.go:Run"]
	if service.ID == 0 || run.ParentID != service.ID {
		t.Errorf("cross-file method parent ID = %d, want Service ID %d", run.ParentID, service.ID)
	}
	otherService := chunkByNameAndFile["other/types.go:Service"]
	runOther := chunkByNameAndFile["other/types.go:RunOther"]
	if otherService.ID == 0 || runOther.ParentID != otherService.ID {
		t.Errorf("other package method parent ID = %d, want Service ID %d", runOther.ParentID, otherService.ID)
	}
	if _, included := chunkByNameAndFile["ignored_test.go:TestOnly"]; included {
		t.Error("test file was indexed")
	}
}
