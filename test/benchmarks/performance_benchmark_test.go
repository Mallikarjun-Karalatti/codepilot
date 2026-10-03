package benchmarks

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"codepilot/internal/agent"
	"codepilot/internal/indexer"
	"codepilot/internal/retrieval"
)

// simulatedNetworkLatencyEmbedder simulates real local inference latency (e.g. 2ms per chunk).
type simulatedNetworkLatencyEmbedder struct {
	latency time.Duration
}

func (e *simulatedNetworkLatencyEmbedder) Embed(text string) ([]float64, error) {
	if e.latency > 0 {
		time.Sleep(e.latency)
	}
	v := make([]float64, 4096)
	v[0] = 1.0
	return v, nil
}

func TestPerformanceBenchmark_WorkerScalingAndThroughput(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()
	generateRealisticRepository(t, rootDir)

	workerCounts := []int{1, 2, 4, 8}
	type workerResult struct {
		workers        int
		duration       time.Duration
		filesPerSec    float64
		chunksPerSec   float64
		embeddingsSec  float64
		chunksEmbedded int
	}

	var results []workerResult
	embedder := &simulatedNetworkLatencyEmbedder{latency: 2 * time.Millisecond}

	for _, w := range workerCounts {
		storeDir := t.TempDir()
		store, _ := indexer.NewFilesystemIndexStore(storeDir)

		config := indexer.DefaultIndexConfig()
		config.ParseWorkers = w
		config.EmbedWorkers = w

		idx := &indexer.IncrementalIndexer{
			Config:   config,
			Embedder: embedder,
			Store:    store,
		}

		start := time.Now()
		indexed, err := idx.Index(ctx, rootDir)
		if err != nil {
			t.Fatalf("worker %d indexing failed: %v", w, err)
		}
		duration := time.Since(start)

		files := float64(indexed.Stats.FilesScanned)
		chunks := float64(indexed.Stats.ChunksEmbedded)
		durSec := duration.Seconds()

		res := workerResult{
			workers:        w,
			duration:       duration,
			filesPerSec:    files / durSec,
			chunksPerSec:   chunks / durSec,
			embeddingsSec:  chunks / durSec,
			chunksEmbedded: indexed.Stats.ChunksEmbedded,
		}
		results = append(results, res)
	}

	t.Log("\n==================== WORKER CONCURRENCY & THROUGHPUT SCALING ====================")
	t.Logf("%-10s | %-12s | %-14s | %-16s | %-10s", "Workers", "Duration", "Files/sec", "Chunks/sec", "Speedup")
	t.Log("---------------------------------------------------------------------------------")
	baseDuration := results[0].duration.Seconds()
	for _, r := range results {
		speedup := baseDuration / r.duration.Seconds()
		t.Logf("%-10d | %-12v | %-14.1f | %-16.1f | %-10.2fx",
			r.workers, r.duration.Round(time.Millisecond), r.filesPerSec, r.chunksPerSec, speedup)
	}
	t.Log("=================================================================================\n")

	// Ensure 8 workers is significantly faster than sequential
	seqTime := results[0].duration
	parTime := results[len(results)-1].duration
	if parTime >= seqTime {
		t.Errorf("expected 8 workers (%v) to be faster than 1 worker (%v)", parTime, seqTime)
	}
}

func TestPerformanceBenchmark_QueryLatencyP50P95P99(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()
	generateRealisticRepository(t, rootDir)

	storeDir := t.TempDir()
	store, _ := indexer.NewFilesystemIndexStore(storeDir)

	embedder := &testEmbedderWithTracking{}
	idx := &indexer.IncrementalIndexer{
		Config:   indexer.DefaultIndexConfig(),
		Embedder: embedder,
		Store:    store,
	}

	indexed, err := idx.Index(ctx, rootDir)
	if err != nil {
		t.Fatalf("indexing failed: %v", err)
	}

	repo, err := retrieval.NewIndexedRepository(indexed, embedder)
	if err != nil {
		t.Fatalf("NewIndexedRepository failed: %v", err)
	}

	tools := agent.NewCodePilotTools(repo)

	queries := []string{
		"checkout", "payment gateway", "handler execute", "auth validate",
		"inventory order", "storage cache", "helper notification", "process order",
	}

	iterations := 100
	latencies := make([]time.Duration, iterations)

	for i := 0; i < iterations; i++ {
		q := queries[i%len(queries)]
		qStart := time.Now()
		_, err := tools.SearchCode(q, 5)
		if err != nil {
			t.Fatalf("search error: %v", err)
		}
		latencies[i] = time.Since(qStart)
	}

	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	p50 := latencies[int(math.Round(float64(iterations)*0.50))-1]
	p95 := latencies[int(math.Round(float64(iterations)*0.95))-1]
	p99 := latencies[int(math.Round(float64(iterations)*0.99))-1]

	t.Log("\n==================== RETRIEVAL LATENCY PERCENTILES ====================")
	t.Logf("Queries Run: %d (across %d chunks)", iterations, len(indexed.Chunks))
	t.Logf("  p50 (Median): %v", p50)
	t.Logf("  p95:          %v", p95)
	t.Logf("  p99:          %v", p99)
	t.Log("=======================================================================\n")

	if p99 > 150*time.Millisecond {
		t.Errorf("p99 latency %v exceeded threshold of 150ms", p99)
	}
}

func TestPerformanceBenchmark_MemoryAndDiskFootprint(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()
	generateRealisticRepository(t, rootDir)

	storeDir := t.TempDir()
	store, _ := indexer.NewFilesystemIndexStore(storeDir)

	var memBefore runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memBefore)

	embedder := &testEmbedderWithTracking{}
	idx := &indexer.IncrementalIndexer{
		Config:   indexer.DefaultIndexConfig(),
		Embedder: embedder,
		Store:    store,
	}

	indexed, err := idx.Index(ctx, rootDir)
	if err != nil {
		t.Fatalf("indexing failed: %v", err)
	}

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	repoID, _ := indexer.CanonicalRepositoryID(rootDir)
	indexPath := filepath.Join(storeDir, repoID, "index.json")
	fileInfo, err := os.Stat(indexPath)
	if err != nil {
		t.Fatalf("stat index.json failed: %v", err)
	}

	diskBytes := fileInfo.Size()
	chunkCount := len(indexed.Chunks)
	bytesPerChunk := float64(diskBytes) / float64(chunkCount)
	allocMB := float64(memAfter.TotalAlloc-memBefore.TotalAlloc) / (1024 * 1024)

	t.Log("\n==================== RESOURCE & FOOTPRINT METRICS ====================")
	t.Logf("Indexed Chunks:       %d", chunkCount)
	t.Logf("On-Disk Index Size:   %.2f KB (%d bytes)", float64(diskBytes)/1024, diskBytes)
	t.Logf("Average Size / Chunk: %.1f bytes", bytesPerChunk)
	t.Logf("Total Heap Allocated: %.2f MB", allocMB)
	t.Log("======================================================================\n")

	if diskBytes == 0 {
		t.Error("index.json is empty")
	}
}
