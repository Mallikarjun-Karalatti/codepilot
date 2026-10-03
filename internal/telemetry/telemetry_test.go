package telemetry

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestJSONLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewJSONLogger(&buf)

	logger.Log("test_event", map[string]any{
		"files": 42,
		"status": "ok",
	})

	output := buf.String()
	if !strings.Contains(output, "test_event") {
		t.Fatalf("expected test_event in log, got: %s", output)
	}

	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("failed to parse log json: %v", err)
	}

	if parsed["event"] != "test_event" {
		t.Errorf("event = %v, want test_event", parsed["event"])
	}
	if parsed["files"] != float64(42) {
		t.Errorf("files = %v, want 42", parsed["files"])
	}
}

func TestIndexMetricsString(t *testing.T) {
	m := IndexMetrics{
		TotalDuration: 100 * time.Millisecond,
		ScanDuration:  10 * time.Millisecond,
		ParseDuration: 20 * time.Millisecond,
		ChunkDuration: 20 * time.Millisecond,
		EmbedDuration: 40 * time.Millisecond,
		PersistDuration: 10 * time.Millisecond,
		FilesScanned:  10,
		ChunksTotal:   50,
	}

	s := m.String()
	if !strings.Contains(s, "Indexing completed") {
		t.Errorf("expected string to contain 'Indexing completed', got: %s", s)
	}
}
