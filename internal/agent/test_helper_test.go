package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"codepilot/internal/indexer"
	"codepilot/internal/retrieval"
)

type mockTestEmbedder struct {
	vec []float64
}

func (m *mockTestEmbedder) Embed(string) ([]float64, error) {
	if len(m.vec) == 0 {
		return []float64{0.1, 0.2, 0.3}, nil
	}
	return m.vec, nil
}

func setupTestRepositoryForTools(t *testing.T) (*retrieval.IndexedRepository, *CodePilotTools) {
	ctx := context.Background()
	rootDir := t.TempDir()

	authContent := `package main

type AuthService struct {
	DB *Database
}

func (a *AuthService) AuthenticateUser(token string) bool {
	return a.DB.ValidateToken(token)
}
`
	dbContent := `package main

type Database struct{}

func (d *Database) ValidateToken(token string) bool {
	return token != ""
}
`
	if err := os.WriteFile(filepath.Join(rootDir, "auth.go"), []byte(authContent), 0o600); err != nil {
		t.Fatalf("failed to write auth.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "db.go"), []byte(dbContent), 0o600); err != nil {
		t.Fatalf("failed to write db.go: %v", err)
	}

	store, _ := indexer.NewFilesystemIndexStore(t.TempDir())
	config := indexer.DefaultIndexConfig()
	config.EmbeddingDimension = 3
	embedder := &mockTestEmbedder{}
	idx := &indexer.IncrementalIndexer{
		Config:   config,
		Embedder: embedder,
		Store:    store,
	}

	state, err := idx.Index(ctx, rootDir)
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}

	repo, err := retrieval.NewIndexedRepository(state, embedder)
	if err != nil {
		t.Fatalf("NewIndexedRepository() error = %v", err)
	}

	tools := NewCodePilotTools(repo)
	return repo, tools
}
