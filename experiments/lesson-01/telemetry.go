package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// IndexMetrics tracks stage-by-stage timings and volume metrics during repository indexing.
type IndexMetrics struct {
	StartTime       time.Time     `json:"start_time"`
	TotalDuration   time.Duration `json:"total_duration_ms"`
	ScanDuration    time.Duration `json:"scan_duration_ms"`
	ParseDuration   time.Duration `json:"parse_duration_ms"`
	ChunkDuration   time.Duration `json:"chunk_duration_ms"`
	EmbedDuration   time.Duration `json:"embed_duration_ms"`
	PersistDuration time.Duration `json:"persist_duration_ms"`

	FilesScanned   int  `json:"files_scanned"`
	FilesAdded     int  `json:"files_added"`
	FilesModified  int  `json:"files_modified"`
	FilesDeleted   int  `json:"files_deleted"`
	FilesUnchanged int  `json:"files_unchanged"`
	FilesParsed    int  `json:"files_parsed"`
	ChunksEmbedded int  `json:"chunks_embedded"`
	ChunksKept     int  `json:"chunks_kept"`
	ChunksTotal    int  `json:"chunks_total"`
	FullReindex    bool `json:"full_reindex"`
}

func (m IndexMetrics) String() string {
	return fmt.Sprintf(
		"Indexing completed in %v\n"+
			"  Stages: scan=%v parse=%v chunk=%v embed=%v persist=%v\n"+
			"  Files: scanned=%d added=%d modified=%d deleted=%d unchanged=%d parsed=%d\n"+
			"  Chunks: embedded=%d kept=%d total=%d full_reindex=%t",
		m.TotalDuration.Round(time.Millisecond),
		m.ScanDuration.Round(time.Millisecond),
		m.ParseDuration.Round(time.Millisecond),
		m.ChunkDuration.Round(time.Millisecond),
		m.EmbedDuration.Round(time.Millisecond),
		m.PersistDuration.Round(time.Millisecond),
		m.FilesScanned,
		m.FilesAdded,
		m.FilesModified,
		m.FilesDeleted,
		m.FilesUnchanged,
		m.FilesParsed,
		m.ChunksEmbedded,
		m.ChunksKept,
		m.ChunksTotal,
		m.FullReindex,
	)
}

// QueryMetrics captures telemetry across the retrieval, context assembly, and LLM generation phases.
type QueryMetrics struct {
	StartTime              time.Time     `json:"start_time"`
	TotalDuration          time.Duration `json:"total_duration_ms"`
	TotalRetrievalDuration time.Duration `json:"retrieval_duration_ms"`
	SemanticDuration       time.Duration `json:"semantic_duration_ms"`
	LexicalDuration        time.Duration `json:"lexical_duration_ms"`
	RRFDuration            time.Duration `json:"rrf_duration_ms"`
	ExpansionDuration      time.Duration `json:"expansion_duration_ms"`
	ContextBuildDuration   time.Duration `json:"context_build_duration_ms"`
	LLMDuration            time.Duration `json:"llm_duration_ms"`

	SemanticCandidates int `json:"semantic_candidates"`
	LexicalCandidates  int `json:"lexical_candidates"`
	ExpandedCandidates int `json:"expanded_candidates"`
	ContextChunks      int `json:"context_chunks"`
	ContextTokens      int `json:"context_tokens"`
}

func (m QueryMetrics) String() string {
	return fmt.Sprintf(
		"Query completed in %v (retrieval=%v context=%v llm=%v)\n"+
			"  Retrieval breakdown: semantic=%v (%d) lexical=%v (%d) rrf=%v expansion=%v (%d)\n"+
			"  Context: chunks=%d tokens=%d",
		m.TotalDuration.Round(time.Millisecond),
		m.TotalRetrievalDuration.Round(time.Millisecond),
		m.ContextBuildDuration.Round(time.Millisecond),
		m.LLMDuration.Round(time.Millisecond),
		m.SemanticDuration.Round(time.Millisecond),
		m.SemanticCandidates,
		m.LexicalDuration.Round(time.Millisecond),
		m.LexicalCandidates,
		m.RRFDuration.Round(time.Millisecond),
		m.ExpansionDuration.Round(time.Millisecond),
		m.ExpandedCandidates,
		m.ContextChunks,
		m.ContextTokens,
	)
}

// StructuredLogger defines an observability contract for emitting structured JSON log entries.
type StructuredLogger interface {
	Log(event string, fields map[string]any)
}

// NopLogger discards all telemetry events.
type NopLogger struct{}

func (NopLogger) Log(event string, fields map[string]any) {}

// JSONLogger emits single-line JSON log messages to an underlying io.Writer.
type JSONLogger struct {
	writer io.Writer
	mu     sync.Mutex
}

func NewJSONLogger(w io.Writer) *JSONLogger {
	if w == nil {
		w = os.Stderr
	}
	return &JSONLogger{writer: w}
}

func (l *JSONLogger) Log(event string, fields map[string]any) {
	if l == nil || l.writer == nil {
		return
	}
	payload := make(map[string]any, len(fields)+2)
	for k, v := range fields {
		payload[k] = v
	}
	payload["event"] = event
	payload["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.writer.Write(append(data, '\n'))
}
