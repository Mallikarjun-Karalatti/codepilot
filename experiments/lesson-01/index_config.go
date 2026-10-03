package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	indexSchemaVersion = 1
	chunkerVersion     = "go-ast-v1"
)

// IndexConfig is the indexing configuration encoded by IndexConfigFingerprint.
type IndexConfig struct {
	SchemaVersion       int
	ChunkerVersion      string
	EmbeddingModel      string
	EmbeddingDimension  int
	IncludeTests        bool
	ExcludedDirectories []string
	EmbedWorkers        int // Concurrency level for embedding (defaults to 4)
	ParseWorkers        int // Concurrency level for AST parsing (defaults to runtime.NumCPU())
}

func DefaultIndexConfig() IndexConfig {
	return IndexConfig{
		SchemaVersion:       indexSchemaVersion,
		ChunkerVersion:      chunkerVersion,
		EmbeddingModel:      "qwen3-embedding",
		EmbeddingDimension:  4096,
		IncludeTests:        false,
		ExcludedDirectories: []string{".git", "vendor"},
		EmbedWorkers:        4,
		ParseWorkers:        runtime.NumCPU(),
	}
}

// EffectiveEmbedWorkers returns a validated, positive concurrency limit for embeddings.
func (c IndexConfig) EffectiveEmbedWorkers() int {
	if c.EmbedWorkers <= 0 {
		return 4
	}
	return c.EmbedWorkers
}

// EffectiveParseWorkers returns a validated, positive concurrency limit for AST parsing.
func (c IndexConfig) EffectiveParseWorkers() int {
	if c.ParseWorkers <= 0 {
		workers := runtime.NumCPU()
		if workers <= 0 {
			return 1
		}
		return workers
	}
	return c.ParseWorkers
}

// Fingerprint generates a deterministic hash representing index compatibility.
// Operational concurrency fields (EmbedWorkers, ParseWorkers) are intentionally
// excluded so changing worker counts never invalidates existing indices.
func (c IndexConfig) Fingerprint() (string, error) {
	excluded := c.normalizedExcluded()
	chunker := c.ChunkerVersion
	if chunker == "" {
		chunker = chunkerVersion
	}
	schema := c.SchemaVersion
	if schema == 0 {
		schema = indexSchemaVersion
	}
	payload, err := json.Marshal(struct {
		SchemaVersion       int      `json:"schema_version"`
		ChunkerVersion      string   `json:"chunker_version"`
		EmbeddingModel      string   `json:"embedding_model"`
		EmbeddingDimension  int      `json:"embedding_dimension"`
		IncludeTests        bool     `json:"include_tests"`
		ExcludedDirectories []string `json:"excluded_directories"`
	}{
		SchemaVersion:       schema,
		ChunkerVersion:      chunker,
		EmbeddingModel:      c.EmbeddingModel,
		EmbeddingDimension:  c.EmbeddingDimension,
		IncludeTests:        c.IncludeTests,
		ExcludedDirectories: excluded,
	})
	if err != nil {
		return "", fmt.Errorf("marshal index config fingerprint: %w", err)
	}

	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:]), nil
}

func (c IndexConfig) scannerOptions() ScannerOptions {
	return ScannerOptions{
		ExcludedDirectories: c.normalizedExcluded(),
		IncludeTests:        c.IncludeTests,
	}
}

// CanonicalRepositoryID hashes the resolved repository root. Moving a checkout
// yields a new identity and a full index, which is acceptable for V1.
func CanonicalRepositoryID(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("absolute repository path: %w", err)
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	sum := sha256.Sum256([]byte(filepath.ToSlash(abs)))
	return hex.EncodeToString(sum[:]), nil
}

func DefaultCodePilotCacheDir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("user cache dir: %w", err)
	}
	return filepath.Join(cache, "codepilot"), nil
}

func (c IndexConfig) normalizedExcluded() []string {
	seen := make(map[string]struct{}, len(c.ExcludedDirectories)+1)
	for _, dir := range c.ExcludedDirectories {
		clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(dir)))
		clean = strings.TrimPrefix(clean, "./")
		clean = strings.Trim(clean, "/")
		if clean != "" {
			seen[clean] = struct{}{}
		}
	}
	seen[".git"] = struct{}{}

	result := make([]string, 0, len(seen))
	for dir := range seen {
		result = append(result, dir)
	}
	sort.Strings(result)
	return result
}
