package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// maxConcurrencyLimitingEmbedder tracks the maximum number of concurrent Embed calls.
type maxConcurrencyLimitingEmbedder struct {
	active     int32
	maxActive  int32
	totalCalls int32
	delay      time.Duration
	failOnText string
}

func (e *maxConcurrencyLimitingEmbedder) Embed(text string) ([]float64, error) {
	current := atomic.AddInt32(&e.active, 1)
	defer atomic.AddInt32(&e.active, -1)

	for {
		oldMax := atomic.LoadInt32(&e.maxActive)
		if current <= oldMax {
			break
		}
		if atomic.CompareAndSwapInt32(&e.maxActive, oldMax, current) {
			break
		}
	}

	atomic.AddInt32(&e.totalCalls, 1)

	if e.failOnText != "" && text == e.failOnText {
		return nil, errors.New("simulated embedding error")
	}

	if e.delay > 0 {
		time.Sleep(e.delay)
	}

	return make([]float64, 4096), nil
}

func TestConcurrentEmbeddingRespectsWorkerBounds(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	// Create 8 files with 2 functions each = 16 chunks total
	for i := 1; i <= 8; i++ {
		path := filepath.Join(rootDir, fmt.Sprintf("module_%02d.go", i))
		content := fmt.Sprintf("package main\n\nfunc FuncA%02d() {}\nfunc FuncB%02d() {}\n", i, i)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write file error: %v", err)
		}
	}

	storeDir := t.TempDir()
	store, err := NewFilesystemIndexStore(storeDir)
	if err != nil {
		t.Fatalf("store error: %v", err)
	}

	embedder := &maxConcurrencyLimitingEmbedder{delay: 20 * time.Millisecond}
	config := DefaultIndexConfig()
	config.EmbedWorkers = 3 // Strictly bound to 3 concurrent workers

	indexer := &IncrementalIndexer{
		Config:   config,
		Embedder: embedder,
		Hasher:   &SHA256FileHasher{},
		Store:    store,
		Scanner:  &RepositoryScanner{Options: config.scannerOptions()},
	}

	indexed, err := indexer.Index(ctx, rootDir)
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}

	if indexed.Stats.ChunksEmbedded != 16 {
		t.Errorf("ChunksEmbedded = %d, want 16", indexed.Stats.ChunksEmbedded)
	}

	maxConcurrent := atomic.LoadInt32(&embedder.maxActive)
	if maxConcurrent > 3 {
		t.Errorf("max active concurrent embeddings was %d, expected <= 3", maxConcurrent)
	}
	if maxConcurrent < 2 {
		t.Errorf("max active concurrent embeddings was %d, expected concurrency > 1", maxConcurrent)
	}
}

func TestConcurrentIndexingPreservesDeterministicOrder(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	for i := 1; i <= 6; i++ {
		path := filepath.Join(rootDir, fmt.Sprintf("file_%02d.go", i))
		content := fmt.Sprintf("package main\n\ntype S%02d struct{}\n\nfunc (s *S%02d) M%02d() {}\nfunc F%02d() {}\n", i, i, i, i)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write file error: %v", err)
		}
	}

	// 1. Run with 1 worker (Sequential)
	storeSeq, _ := NewFilesystemIndexStore(t.TempDir())
	configSeq := DefaultIndexConfig()
	configSeq.EmbedWorkers = 1
	configSeq.ParseWorkers = 1

	indexerSeq := &IncrementalIndexer{
		Config:   configSeq,
		Embedder: &testEmbedderWithTracking{},
		Store:    storeSeq,
	}
	indexedSeq, err := indexerSeq.Index(ctx, rootDir)
	if err != nil {
		t.Fatalf("sequential Index() error = %v", err)
	}

	// 2. Run with 8 workers (Concurrent)
	storeConc, _ := NewFilesystemIndexStore(t.TempDir())
	configConc := DefaultIndexConfig()
	configConc.EmbedWorkers = 8
	configConc.ParseWorkers = 8

	indexerConc := &IncrementalIndexer{
		Config:   configConc,
		Embedder: &testEmbedderWithTracking{},
		Store:    storeConc,
	}
	indexedConc, err := indexerConc.Index(ctx, rootDir)
	if err != nil {
		t.Fatalf("concurrent Index() error = %v", err)
	}

	// Verify exact chunk list equivalence
	if len(indexedSeq.Chunks) != len(indexedConc.Chunks) {
		t.Fatalf("chunk count mismatch: seq=%d conc=%d", len(indexedSeq.Chunks), len(indexedConc.Chunks))
	}
	for i := range indexedSeq.Chunks {
		seqChunk := indexedSeq.Chunks[i]
		concChunk := indexedConc.Chunks[i]
		if seqChunk.ID != concChunk.ID || seqChunk.Name != concChunk.Name || seqChunk.SourceFile != concChunk.SourceFile {
			t.Fatalf("chunk %d mismatch:\nseq:  %+v\nconc: %+v", i, seqChunk, concChunk)
		}
	}
}

func TestConcurrentIndexingHandlesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rootDir := t.TempDir()

	for i := 1; i <= 4; i++ {
		path := filepath.Join(rootDir, fmt.Sprintf("file_%02d.go", i))
		content := fmt.Sprintf("package main\n\nfunc LongRunning%02d() {}\n", i)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write file error: %v", err)
		}
	}

	store, _ := NewFilesystemIndexStore(t.TempDir())
	embedder := &maxConcurrencyLimitingEmbedder{delay: 50 * time.Millisecond}
	config := DefaultIndexConfig()
	config.EmbedWorkers = 2

	indexer := &IncrementalIndexer{
		Config:   config,
		Embedder: embedder,
		Store:    store,
	}

	// Cancel shortly after starting
	go func() {
		time.Sleep(15 * time.Millisecond)
		cancel()
	}()

	_, err := indexer.Index(ctx, rootDir)
	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Logf("got error on cancel: %v", err)
	}
}

func TestConcurrentEmbeddingPropagatesWorkerError(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	for i := 1; i <= 3; i++ {
		path := filepath.Join(rootDir, fmt.Sprintf("mod_%02d.go", i))
		content := fmt.Sprintf("package main\n\nfunc TargetFunc%02d() {}\n", i)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write file error: %v", err)
		}
	}

	store, _ := NewFilesystemIndexStore(t.TempDir())
	embedder := &maxConcurrencyLimitingEmbedder{
		failOnText: "func TargetFunc02() {}",
	}
	config := DefaultIndexConfig()
	config.EmbedWorkers = 4

	indexer := &IncrementalIndexer{
		Config:   config,
		Embedder: embedder,
		Store:    store,
	}

	_, err := indexer.Index(ctx, rootDir)
	if err == nil {
		t.Fatal("expected error from failing embedder, got nil")
	}
}

func TestConcurrentParsingEarlyWorkerCancellation(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	for i := 1; i <= 8; i++ {
		path := filepath.Join(rootDir, fmt.Sprintf("file_%02d.go", i))
		content := fmt.Sprintf("package main\n\nfunc Valid%02d() {}\n", i)
		if i == 3 {
			// Deliberate syntax error to fail parsing
			content = "package main\n\nfunc Broken("
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write file error: %v", err)
		}
	}

	store, _ := NewFilesystemIndexStore(t.TempDir())
	config := DefaultIndexConfig()
	config.ParseWorkers = 4

	indexer := &IncrementalIndexer{
		Config:   config,
		Embedder: &testEmbedderWithTracking{},
		Store:    store,
	}

	_, err := indexer.Index(ctx, rootDir)
	if err == nil {
		t.Fatal("expected error from syntax error in file_03.go, got nil")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("expected parse error, got: %v", err)
	}
}
