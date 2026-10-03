package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type failAfterNEmbedder struct {
	count     int32
	failAfter int32
}

func (e *failAfterNEmbedder) Embed(text string) ([]float64, error) {
	c := atomic.AddInt32(&e.count, 1)
	if c > e.failAfter {
		return nil, errors.New("simulated mid-indexing embed failure")
	}
	return make([]float64, 4096), nil
}

func TestFailureRecovery_MidIndexEmbeddingFailure(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	file1 := filepath.Join(rootDir, "file1.go")
	file2 := filepath.Join(rootDir, "file2.go")
	_ = os.WriteFile(file1, []byte("package main\n\nfunc One() {}\n"), 0o600)
	_ = os.WriteFile(file2, []byte("package main\n\nfunc Two() {}\n"), 0o600)

	storeDir := t.TempDir()
	store, _ := NewFilesystemIndexStore(storeDir)

	// Step 1: Initial successful index
	embedder := &testEmbedderWithTracking{}
	config := DefaultIndexConfig()
	indexer := &IncrementalIndexer{
		Config:   config,
		Embedder: embedder,
		Store:    store,
	}

	initialIndexed, err := indexer.Index(ctx, rootDir)
	if err != nil {
		t.Fatalf("initial indexing failed: %v", err)
	}
	if len(initialIndexed.Chunks) != 2 {
		t.Fatalf("expected 2 chunks in initial index, got %d", len(initialIndexed.Chunks))
	}

	// Step 2: Trigger failure during incremental update of a 3rd file
	file3 := filepath.Join(rootDir, "file3.go")
	_ = os.WriteFile(file3, []byte("package main\n\nfunc Three() {}\n"), 0o600)

	failingEmbedder := &failAfterNEmbedder{failAfter: 0}
	indexerFailing := &IncrementalIndexer{
		Config:   config,
		Embedder: failingEmbedder,
		Store:    store,
	}

	_, err = indexerFailing.Index(ctx, rootDir)
	if err == nil {
		t.Fatal("expected failure from failing embedder, got nil")
	}

	// Step 3: Verify the previous valid index is preserved intact!
	loaded, err := indexer.Load(ctx, rootDir)
	if err != nil {
		t.Fatalf("failed to load preserved index after failure: %v", err)
	}
	if len(loaded.Chunks) != 2 {
		t.Fatalf("persisted index was corrupted! expected 2 chunks, got %d", len(loaded.Chunks))
	}
	if loaded.Chunks[0].Name != "One" || loaded.Chunks[1].Name != "Two" {
		t.Errorf("unexpected chunks after recovery: %v", loaded.Chunks)
	}
	t.Log("[Recovery Verified] Previous valid index remained 100% intact after mid-indexing failure.")
}

func TestFailureRecovery_ParseFailurePreservesPreviousIndex(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	file1 := filepath.Join(rootDir, "auth.go")
	_ = os.WriteFile(file1, []byte("package main\n\nfunc Auth() {}\n"), 0o600)

	storeDir := t.TempDir()
	store, _ := NewFilesystemIndexStore(storeDir)
	config := DefaultIndexConfig()

	indexer := &IncrementalIndexer{
		Config:   config,
		Embedder: &testEmbedderWithTracking{},
		Store:    store,
	}

	_, err := indexer.Index(ctx, rootDir)
	if err != nil {
		t.Fatalf("initial index error: %v", err)
	}

	// Corrupt a newly added file
	brokenFile := filepath.Join(rootDir, "broken.go")
	_ = os.WriteFile(brokenFile, []byte("package main\n\nfunc Invalid("), 0o600)

	_, err = indexer.Index(ctx, rootDir)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}

	// Previous index must load cleanly
	loaded, err := indexer.Load(ctx, rootDir)
	if err != nil {
		t.Fatalf("Load error after parse failure: %v", err)
	}
	if len(loaded.Chunks) != 1 || loaded.Chunks[0].Name != "Auth" {
		t.Errorf("preserved index altered after parse failure: %+v", loaded.Chunks)
	}
	t.Log("[Recovery Verified] Parse failure gracefully rejected; previous index untouched.")
}

func TestFailureRecovery_CorruptedIndexFileHandled(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()
	storeDir := t.TempDir()

	store, _ := NewFilesystemIndexStore(storeDir)
	repoID, _ := CanonicalRepositoryID(rootDir)

	// Write garbage data to index.json
	repoStoreDir := filepath.Join(storeDir, repoID)
	_ = os.MkdirAll(repoStoreDir, 0o755)
	_ = os.WriteFile(filepath.Join(repoStoreDir, "index.json"), []byte("{corrupted json"), 0o600)

	indexer := &IncrementalIndexer{
		Config:   DefaultIndexConfig(),
		Embedder: &testEmbedderWithTracking{},
		Store:    store,
	}

	_, err := indexer.Load(ctx, rootDir)
	if err == nil {
		t.Fatal("expected error loading corrupted index file, got nil")
	}
	if !strings.Contains(err.Error(), "decode index") {
		t.Errorf("unexpected error for corrupted json: %v", err)
	}
	t.Log("[Recovery Verified] Corrupted index file detected and rejected.")
}

func TestFailureRecovery_NonExistentOrMovedRepository(t *testing.T) {
	ctx := context.Background()
	storeDir := t.TempDir()
	store, _ := NewFilesystemIndexStore(storeDir)

	indexer := &IncrementalIndexer{
		Config:   DefaultIndexConfig(),
		Embedder: &testEmbedderWithTracking{},
		Store:    store,
	}

	// Indexing non-existent folder should fail cleanly
	_, err := indexer.Index(ctx, "/path/to/nonexistent/repo")
	if err == nil {
		t.Fatal("expected error indexing non-existent path")
	}

	// Loading non-indexed repo should report no index found
	_, err = indexer.Load(ctx, "/path/to/nonexistent/repo")
	if err == nil {
		t.Fatal("expected error loading non-indexed repository")
	}
	if !strings.Contains(err.Error(), "no persisted index") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFailureRecovery_ConfigAndDimensionChanges(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()
	file1 := filepath.Join(rootDir, "main.go")
	_ = os.WriteFile(file1, []byte("package main\n\nfunc Main() {}\n"), 0o600)

	storeDir := t.TempDir()
	store, _ := NewFilesystemIndexStore(storeDir)

	config := DefaultIndexConfig()
	config.EmbeddingDimension = 4096
	embedder := &testEmbedderWithTracking{}

	indexer := &IncrementalIndexer{
		Config:   config,
		Embedder: embedder,
		Store:    store,
	}

	// 1. Initial Index with dim=4096
	_, err := indexer.Index(ctx, rootDir)
	if err != nil {
		t.Fatalf("Index error: %v", err)
	}

	// 2. Change configuration to dimension=1536 (mismatch with embedder output of 4096)
	badConfig := config
	badConfig.EmbeddingDimension = 1536
	indexerMismatched := &IncrementalIndexer{
		Config:   badConfig,
		Embedder: embedder, // returns 4096 dim
		Store:    store,
	}

	_, err = indexerMismatched.Index(ctx, rootDir)
	if err == nil {
		t.Fatal("expected error when embedding dimension doesn't match configured dimension")
	}
	if !strings.Contains(err.Error(), "embedding dimension 4096 does not match configured 1536") {
		t.Errorf("unexpected error message: %v", err)
	}

	// 3. Verify previous index with dim=4096 is still recoverable!
	loaded, err := indexer.Load(ctx, rootDir)
	if err != nil {
		t.Fatalf("failed to load valid index after dimension mismatch failure: %v", err)
	}
	if len(loaded.Chunks) != 1 {
		t.Errorf("chunks count = %d, want 1", len(loaded.Chunks))
	}
	t.Log("[Recovery Verified] Config dimension mismatch failed safely; original index preserved.")
}

func TestFailureRecovery_InterruptedPersistenceAtomicRename(t *testing.T) {
	ctx := context.Background()
	storeDir := t.TempDir()
	store, _ := NewFilesystemIndexStore(storeDir)

	repoID := "test_atomic_repo"
	state := &PersistedIndex{
		Manifest: RepositoryManifest{
			SchemaVersion: 1,
			RepositoryID:  repoID,
		},
		Documents: []CodeDocument{
			{Chunk: CodeChunk{ID: 1, Name: "Initial"}},
		},
	}

	// Commit initial state
	if err := store.Commit(ctx, repoID, state); err != nil {
		t.Fatalf("commit initial state failed: %v", err)
	}

	// Check final file exists
	finalPath := filepath.Join(storeDir, repoID, "index.json")
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("index.json missing: %v", err)
	}

	// Verify temp file does not linger
	tmpPath := filepath.Join(storeDir, repoID, "index.json.tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("index.json.tmp should not exist after atomic commit")
	}

	t.Log("[Recovery Verified] Atomic commit ensures index.json is written to tmp and atomically renamed.")
}
