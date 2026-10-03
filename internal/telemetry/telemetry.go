package telemetry

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
	StartTime         time.Time     `json:"start_time"`
	TotalDuration     time.Duration `json:"total_duration_ms"`
	RetrievalDuration time.Duration `json:"retrieval_duration_ms"`
	AssemblyDuration  time.Duration `json:"assembly_duration_ms"`
	GenerationDuration time.Duration `json:"generation_duration_ms"`

	CandidatesRetrieved int `json:"candidates_retrieved"`
	ChunksSelected      int `json:"chunks_selected"`
	TokensBudgeted      int `json:"tokens_budgeted"`
	TokensUsed          int `json:"tokens_used"`
}

func (m QueryMetrics) String() string {
	return fmt.Sprintf(
		"Query completed in %v\n"+
			"  Stages: retrieval=%v assembly=%v generation=%v\n"+
			"  Evidence: candidates=%d selected=%d tokens=%d/%d",
		m.TotalDuration.Round(time.Millisecond),
		m.RetrievalDuration.Round(time.Millisecond),
		m.AssemblyDuration.Round(time.Millisecond),
		m.GenerationDuration.Round(time.Millisecond),
		m.CandidatesRetrieved,
		m.ChunksSelected,
		m.TokensUsed,
		m.TokensBudgeted,
	)
}

// StructuredLogger defines an interface for emitting machine-readable telemetry logs.
type StructuredLogger interface {
	Log(event string, fields map[string]any)
}

// JSONLogger emits single-line JSON logs to the configured writer.
type JSONLogger struct {
	mu  sync.Mutex
	out io.Writer
}

func NewJSONLogger(out io.Writer) *JSONLogger {
	if out == nil {
		out = os.Stderr
	}
	return &JSONLogger{out: out}
}

func (l *JSONLogger) Log(event string, fields map[string]any) {
	if l == nil || l.out == nil {
		return
	}
	payload := make(map[string]any, len(fields)+2)
	for k, v := range fields {
		payload[k] = v
	}
	payload["event"] = event
	payload["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)

	l.mu.Lock()
	defer l.mu.Unlock()
	bytes, err := json.Marshal(payload)
	if err == nil {
		fmt.Fprintln(l.out, string(bytes))
	}
}

// DiscardLogger discards all logs.
type DiscardLogger struct{}

func (d DiscardLogger) Log(event string, fields map[string]any) {}
