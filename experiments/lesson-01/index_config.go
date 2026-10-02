package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const (
	indexSchemaVersion = 1
	chunkerVersion     = "go-ast-v1"
)

// IndexConfig is the indexing configuration encoded by IndexConfigFingerprint.
type IndexConfig struct {
	SchemaVersion      int
	ChunkerVersion     string
	EmbeddingModel     string
	EmbeddingDimension int
	IncludeTests       bool
	ExcludedDirectories []string
}

func DefaultIndexConfig() IndexConfig {
	return IndexConfig{
		SchemaVersion:       indexSchemaVersion,
		ChunkerVersion:      chunkerVersion,
		EmbeddingModel:      "qwen3-embedding",
		EmbeddingDimension:  4096,
		IncludeTests:        false,
		ExcludedDirectories: []string{".git", "vendor"},
	}
}

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
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func (c IndexConfig) normalizedExcluded() []string {
	seen := map[string]struct{}{".git": {}}
	for _, dir := range c.ExcludedDirectories {
		if dir == "" {
			continue
		}
		seen[dir] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for dir := range seen {
		out = append(out, dir)
	}
	sort.Strings(out)
	return out
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
