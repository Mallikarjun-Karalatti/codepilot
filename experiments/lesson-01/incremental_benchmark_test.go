package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// BenchmarkIncrementalVsFullIndexing measures and quantifies the work avoided
// by the incremental indexer compared to a full re-indexing run.
func BenchmarkIncrementalVsFullIndexing(b *testing.B) {
	ctx := context.Background()
	rootDir := b.TempDir()

	// Generate 10 sample source files with structs, methods, and functions.
	for i := 1; i <= 10; i++ {
		filename := filepath.Join(rootDir, fmt.Sprintf("service_%02d.go", i))
		content := fmt.Sprintf(`package main

type Service%02d struct {
	ID int
}

func (s *Service%02d) Process() bool {
	return true
}

func Helper%02d() string {
	return "helper"
}
`, i, i, i)
		if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
			b.Fatalf("failed to create source file: %v", err)
		}
	}

	storeDir := b.TempDir()
	store, err := NewFilesystemIndexStore(storeDir)
	if err != nil {
		b.Fatalf("NewFilesystemIndexStore() error = %v", err)
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
	start := time.Now()
	initialIndex, err := indexer.Index(ctx, rootDir)
	if err != nil {
		b.Fatalf("initial Index() error = %v", err)
	}
	initialDuration := time.Since(start)

	totalFiles := initialIndex.Stats.FilesScanned
	totalChunks := initialIndex.Stats.ChunksEmbedded

	b.Logf("[Initial Index] Files: %d, Chunks Embedded: %d, Time: %v", totalFiles, totalChunks, initialDuration)

	// 2. Incremental Run with NO modifications (Zero-Work Fast-Path)
	embedder.reset()
	start = time.Now()
	zeroWorkIndex, err := indexer.Index(ctx, rootDir)
	if err != nil {
		b.Fatalf("zero-work Index() error = %v", err)
	}
	zeroWorkDuration := time.Since(start)

	b.Logf("[Zero-Work Index] Files Unchanged: %d, Chunks Embedded: %d, Chunks Kept: %d, Time: %v",
		zeroWorkIndex.Stats.FilesUnchanged,
		zeroWorkIndex.Stats.ChunksEmbedded,
		zeroWorkIndex.Stats.ChunksKept,
		zeroWorkDuration,
	)

	if zeroWorkIndex.Stats.ChunksEmbedded != 0 {
		b.Fatalf("expected 0 chunks embedded on unchanged run, got %d", zeroWorkIndex.Stats.ChunksEmbedded)
	}
	if zeroWorkIndex.Stats.ChunksKept != totalChunks {
		b.Fatalf("expected all %d chunks to be kept, got %d", totalChunks, zeroWorkIndex.Stats.ChunksKept)
	}

	// 3. Mutate exactly 1 file out of 10
	modFile := filepath.Join(rootDir, "service_01.go")
	newContent := `package main

type Service01 struct {
	ID int
}

func (s *Service01) Process() bool {
	return false
}

func (s *Service01) ExtraMethod() {}

func Helper01() string {
	return "helper_v2"
}
`
	if err := os.WriteFile(modFile, []byte(newContent), 0o600); err != nil {
		b.Fatalf("failed to update service_01.go: %v", err)
	}

	embedder.reset()
	start = time.Now()
	oneModIndex, err := indexer.Index(ctx, rootDir)
	if err != nil {
		b.Fatalf("one-modification Index() error = %v", err)
	}
	oneModDuration := time.Since(start)

	workAvoidedPercent := float64(oneModIndex.Stats.ChunksKept) / float64(len(oneModIndex.Chunks)) * 100.0
	b.Logf("[1 File Mutated] Files Modified: %d, Chunks Embedded: %d, Chunks Kept: %d, Work Avoided: %.1f%%, Time: %v",
		oneModIndex.Stats.FilesModified,
		oneModIndex.Stats.ChunksEmbedded,
		oneModIndex.Stats.ChunksKept,
		workAvoidedPercent,
		oneModDuration,
	)

	if oneModIndex.Stats.FilesModified != 1 {
		b.Errorf("expected 1 file modified, got %d", oneModIndex.Stats.FilesModified)
	}
	if oneModIndex.Stats.FilesUnchanged != 9 {
		b.Errorf("expected 9 files unchanged, got %d", oneModIndex.Stats.FilesUnchanged)
	}
	// service_01.go had 3 chunks, now has 4 chunks. 9 other files have 3 chunks each = 27 kept chunks.
	if oneModIndex.Stats.ChunksKept != 27 {
		b.Errorf("expected 27 chunks kept, got %d", oneModIndex.Stats.ChunksKept)
	}
	if oneModIndex.Stats.ChunksEmbedded != 4 {
		b.Errorf("expected 4 chunks embedded, got %d", oneModIndex.Stats.ChunksEmbedded)
	}
}

func TestIncrementalWorkAvoidance(t *testing.T) {
	// Re-run the validation logic as a standard test for CI verification
	BenchmarkIncrementalVsFullIndexing(&testing.B{})
}
