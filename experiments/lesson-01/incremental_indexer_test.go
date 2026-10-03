package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type testEmbedderWithTracking struct {
	mu    sync.Mutex
	calls []string
}

func (e *testEmbedderWithTracking) Embed(text string) ([]float64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, text)
	return make([]float64, 4096), nil
}

func (e *testEmbedderWithTracking) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.calls)
}

func (e *testEmbedderWithTracking) reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = nil
}

func TestIncrementalIndexerLifecycle(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	file1 := filepath.Join(rootDir, "auth.go")
	file2 := filepath.Join(rootDir, "user.go")

	if err := os.WriteFile(file1, []byte("package main\n\nfunc Authenticate() bool { return true }\n"), 0o600); err != nil {
		t.Fatalf("failed to write auth.go: %v", err)
	}
	if err := os.WriteFile(file2, []byte("package main\n\nfunc GetUser() string { return \"user\" }\n"), 0o600); err != nil {
		t.Fatalf("failed to write user.go: %v", err)
	}

	storeDir := t.TempDir()
	store, err := NewFilesystemIndexStore(storeDir)
	if err != nil {
		t.Fatalf("NewFilesystemIndexStore() error = %v", err)
	}

	embedder := &testEmbedderWithTracking{}
	config := DefaultIndexConfig()

	indexer := &IncrementalIndexer{
		Config:   config,
		Embedder: embedder,
		Hasher:   &SHA256FileHasher{},
		Store:    store,
		Scanner:  &RepositoryScanner{Options: config.scannerOptions()},
	}

	// 1. Initial Indexing
	t.Run("initial index embeds all chunks", func(t *testing.T) {
		indexed, err := indexer.Index(ctx, rootDir)
		if err != nil {
			t.Fatalf("Index() error = %v", err)
		}

		if indexed.Stats.FilesAdded != 2 {
			t.Errorf("FilesAdded = %d, want 2", indexed.Stats.FilesAdded)
		}
		if indexed.Stats.ChunksEmbedded != 2 {
			t.Errorf("ChunksEmbedded = %d, want 2", indexed.Stats.ChunksEmbedded)
		}
		if indexed.Stats.ChunksKept != 0 {
			t.Errorf("ChunksKept = %d, want 0", indexed.Stats.ChunksKept)
		}
		if indexed.Stats.FullReindex {
			t.Errorf("FullReindex = true, want false for initial run")
		}
	})

	// 2. Zero-Work Run (No changes)
	t.Run("immediate re-index performs zero embeddings", func(t *testing.T) {
		embedder.reset() // reset counter
		indexed, err := indexer.Index(ctx, rootDir)
		if err != nil {
			t.Fatalf("Index() error = %v", err)
		}

		if indexed.Stats.FilesUnchanged != 2 {
			t.Errorf("FilesUnchanged = %d, want 2", indexed.Stats.FilesUnchanged)
		}
		if indexed.Stats.ChunksEmbedded != 0 {
			t.Errorf("ChunksEmbedded = %d, want 0", indexed.Stats.ChunksEmbedded)
		}
		if indexed.Stats.ChunksKept != 2 {
			t.Errorf("ChunksKept = %d, want 2", indexed.Stats.ChunksKept)
		}
		if embedder.callCount() != 0 {
			t.Errorf("Embed() called %d times, want 0", embedder.callCount())
		}
	})

	// 3. Modify One File
	t.Run("modify one file re-embeds only that file", func(t *testing.T) {
		embedder.reset()
		newContent := "package main\n\nfunc Authenticate() bool { return false }\nfunc NewHelper() {}\n"
		if err := os.WriteFile(file1, []byte(newContent), 0o600); err != nil {
			t.Fatalf("failed to update auth.go: %v", err)
		}

		indexed, err := indexer.Index(ctx, rootDir)
		if err != nil {
			t.Fatalf("Index() error = %v", err)
		}

		if indexed.Stats.FilesModified != 1 {
			t.Errorf("FilesModified = %d, want 1", indexed.Stats.FilesModified)
		}
		if indexed.Stats.FilesUnchanged != 1 {
			t.Errorf("FilesUnchanged = %d, want 1", indexed.Stats.FilesUnchanged)
		}
		if indexed.Stats.ChunksEmbedded != 2 { // auth.go now has 2 functions
			t.Errorf("ChunksEmbedded = %d, want 2", indexed.Stats.ChunksEmbedded)
		}
		if indexed.Stats.ChunksKept != 1 { // user.go had 1 function
			t.Errorf("ChunksKept = %d, want 1", indexed.Stats.ChunksKept)
		}
	})

	// 4. Delete One File
	t.Run("delete one file removes its chunks", func(t *testing.T) {
		embedder.reset()
		if err := os.Remove(file1); err != nil {
			t.Fatalf("failed to remove auth.go: %v", err)
		}

		indexed, err := indexer.Index(ctx, rootDir)
		if err != nil {
			t.Fatalf("Index() error = %v", err)
		}

		if indexed.Stats.FilesDeleted != 1 {
			t.Errorf("FilesDeleted = %d, want 1", indexed.Stats.FilesDeleted)
		}
		if indexed.Stats.ChunksEmbedded != 0 {
			t.Errorf("ChunksEmbedded = %d, want 0", indexed.Stats.ChunksEmbedded)
		}
		if indexed.Stats.ChunksKept != 1 {
			t.Errorf("ChunksKept = %d, want 1", indexed.Stats.ChunksKept)
		}
		if len(indexed.Chunks) != 1 || indexed.Chunks[0].Name != "GetUser" {
			t.Errorf("surviving chunks = %v, want only GetUser", indexed.Chunks)
		}
	})

	// 5. Config Change Triggers Full Re-index
	t.Run("changing config fingerprint triggers clean full re-index", func(t *testing.T) {
		embedder.reset()
		newConfig := DefaultIndexConfig()
		newConfig.EmbeddingModel = "qwen-next-gen" // changes fingerprint

		reindexIndexer := &IncrementalIndexer{
			Config:   newConfig,
			Embedder: embedder,
			Hasher:   &SHA256FileHasher{},
			Store:    store,
			Scanner:  &RepositoryScanner{Options: newConfig.scannerOptions()},
		}

		indexed, err := reindexIndexer.Index(ctx, rootDir)
		if err != nil {
			t.Fatalf("Index() error = %v", err)
		}

		if !indexed.Stats.FullReindex {
			t.Errorf("FullReindex = false, want true")
		}
		if indexed.Stats.ChunksEmbedded != 1 {
			t.Errorf("ChunksEmbedded = %d, want 1", indexed.Stats.ChunksEmbedded)
		}
		if indexed.Stats.ChunksKept != 0 {
			t.Errorf("ChunksKept = %d, want 0", indexed.Stats.ChunksKept)
		}
	})
}
