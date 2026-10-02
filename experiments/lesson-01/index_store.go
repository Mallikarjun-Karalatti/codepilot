package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// PersistedIndex is the on-disk index: manifest metadata plus embeddings.
// Relationship and lexical state are rebuilt on load so IDs stay consistent
// with the stored chunks without duplicating graph internals in the manifest.
type PersistedIndex struct {
	Manifest  RepositoryManifest `json:"manifest"`
	Documents []CodeDocument     `json:"documents"`
}

// IndexStore loads and atomically commits a repository index.
type IndexStore interface {
	Load(ctx context.Context, repositoryID string) (*PersistedIndex, error)
	Commit(ctx context.Context, repositoryID string, state *PersistedIndex) error
}

// FilesystemIndexStore keeps one index.json per repository under BaseDir.
type FilesystemIndexStore struct {
	BaseDir string
}

func NewFilesystemIndexStore(baseDir string) (*FilesystemIndexStore, error) {
	if baseDir == "" {
		dir, err := DefaultCodePilotCacheDir()
		if err != nil {
			return nil, err
		}
		baseDir = dir
	}
	return &FilesystemIndexStore{BaseDir: baseDir}, nil
}

func (s *FilesystemIndexStore) repoDir(repositoryID string) string {
	return filepath.Join(s.BaseDir, repositoryID)
}

func (s *FilesystemIndexStore) Load(ctx context.Context, repositoryID string) (*PersistedIndex, error) {
	if s == nil {
		return nil, fmt.Errorf("index store must not be nil")
	}
	if repositoryID == "" {
		return nil, fmt.Errorf("repository id must not be empty")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := filepath.Join(s.repoDir(repositoryID), "index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read index %q: %w", path, err)
	}
	var state PersistedIndex
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode index %q: %w", path, err)
	}
	return &state, nil
}

func (s *FilesystemIndexStore) Commit(ctx context.Context, repositoryID string, state *PersistedIndex) error {
	if s == nil {
		return fmt.Errorf("index store must not be nil")
	}
	if repositoryID == "" {
		return fmt.Errorf("repository id must not be empty")
	}
	if state == nil {
		return fmt.Errorf("persisted index must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	dir := s.repoDir(repositoryID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create index dir %q: %w", dir, err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode index: %w", err)
	}

	tmp := filepath.Join(dir, "index.json.tmp")
	final := filepath.Join(dir, "index.json")
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create temp index %q: %w", tmp, err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("write temp index %q: %w", tmp, err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync temp index %q: %w", tmp, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temp index %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("commit index %q: %w", final, err)
	}
	return nil
}
