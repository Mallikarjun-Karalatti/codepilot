package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIndexMetricsAndTelemetry(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	file1 := filepath.Join(rootDir, "main.go")
	if err := os.WriteFile(file1, []byte("package main\n\nfunc Run() {}\n"), 0o600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	storeDir := t.TempDir()
	store, _ := NewFilesystemIndexStore(storeDir)

	var logBuf bytes.Buffer
	logger := NewJSONLogger(&logBuf)

	indexer := &IncrementalIndexer{
		Config:   DefaultIndexConfig(),
		Embedder: &testEmbedderWithTracking{},
		Store:    store,
		Logger:   logger,
	}

	indexed, err := indexer.Index(ctx, rootDir)
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}

	// Verify IndexMetrics
	m := indexed.Metrics
	if m.FilesScanned != 1 {
		t.Errorf("FilesScanned = %d, want 1", m.FilesScanned)
	}
	if m.ChunksEmbedded != 1 {
		t.Errorf("ChunksEmbedded = %d, want 1", m.ChunksEmbedded)
	}
	if m.TotalDuration <= 0 {
		t.Errorf("TotalDuration = %v, want > 0", m.TotalDuration)
	}
	summary := m.String()
	if !strings.Contains(summary, "Indexing completed") {
		t.Errorf("summary string missing expected header: %s", summary)
	}

	// Verify structured JSON log output
	logLine := logBuf.String()
	if logLine == "" {
		t.Fatal("expected JSON log output, got empty")
	}

	var entry map[string]any
	if err := json.Unmarshal(logBuf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to decode JSON log entry: %v", err)
	}
	if entry["event"] != "index_completed" {
		t.Errorf("event = %v, want 'index_completed'", entry["event"])
	}
	if _, ok := entry["timestamp"]; !ok {
		t.Error("missing timestamp in log entry")
	}
	if entry["chunks_embedded"].(float64) != 1 {
		t.Errorf("chunks_embedded = %v, want 1", entry["chunks_embedded"])
	}
}

func TestQueryMetricsStringFormatting(t *testing.T) {
	qm := QueryMetrics{
		TotalDuration:          150 * time.Millisecond,
		TotalRetrievalDuration: 40 * time.Millisecond,
		SemanticDuration:       20 * time.Millisecond,
		LexicalDuration:        10 * time.Millisecond,
		RRFDuration:            5 * time.Millisecond,
		ExpansionDuration:      5 * time.Millisecond,
		ContextBuildDuration:   10 * time.Millisecond,
		LLMDuration:            100 * time.Millisecond,
		SemanticCandidates:     5,
		LexicalCandidates:      5,
		ExpandedCandidates:     2,
		ContextChunks:          3,
		ContextTokens:          240,
	}

	str := qm.String()
	if !strings.Contains(str, "Query completed") {
		t.Errorf("unexpected string: %s", str)
	}
	if !strings.Contains(str, "semantic=") {
		t.Errorf("missing semantic latency: %s", str)
	}
}
