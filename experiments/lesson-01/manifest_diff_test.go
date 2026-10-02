package main

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

type mockHasher struct {
	hashes map[string]string
	errs   map[string]error
}

func (m *mockHasher) HashFile(ctx context.Context, path string) (string, error) {
	if err, ok := m.errs[path]; ok {
		return "", err
	}
	if hash, ok := m.hashes[path]; ok {
		return hash, nil
	}
	return "", errors.New("hash not found in mock")
}

func TestChangeDetector(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	earlier := now.Add(-1 * time.Hour)

	const fingerprint = "config-v1"

	t.Run("nil manifest treats all files as added", func(t *testing.T) {
		detector := &ChangeDetector{Hasher: &mockHasher{}}
		snapshots := []FileSnapshot{
			{Path: "b.go", Size: 100, ModTime: now},
			{Path: "a.go", Size: 200, ModTime: now},
		}

		got, err := detector.Detect(ctx, nil, "repo-1", fingerprint, snapshots)
		if err != nil {
			t.Fatalf("Detect() unexpected error = %v", err)
		}

		want := &ChangeSet{
			Added:     []string{"a.go", "b.go"}, // sorted
			Modified:  []string{},
			Deleted:   []string{},
			Unchanged: []string{},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("fingerprint mismatch forces full re-index", func(t *testing.T) {
		detector := &ChangeDetector{Hasher: &mockHasher{}}
		prevManifest := &RepositoryManifest{
			RepositoryID:           "repo-1",
			IndexConfigFingerprint: "old-fingerprint",
			Files: map[string]ManifestFile{
				"old.go": {SHA256: "hash-old", Size: 50, ModTime: earlier},
			},
		}
		snapshots := []FileSnapshot{
			{Path: "new.go", Size: 100, ModTime: now},
		}

		got, err := detector.Detect(ctx, prevManifest, "repo-1", "new-fingerprint", snapshots)
		if err != nil {
			t.Fatalf("Detect() unexpected error = %v", err)
		}

		want := &ChangeSet{
			Added:       []string{"new.go"},
			Modified:    []string{},
			Deleted:     []string{"old.go"},
			Unchanged:   []string{},
			FullReindex: true,
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("fast path skips hashing when size and modtime match", func(t *testing.T) {
		// Mock has NO hashes registered; calling HashFile would fail
		detector := &ChangeDetector{Hasher: &mockHasher{}}
		prevManifest := &RepositoryManifest{
			RepositoryID:           "repo-1",
			IndexConfigFingerprint: fingerprint,
			Files: map[string]ManifestFile{
				"stable.go": {SHA256: "hash123", Size: 100, ModTime: earlier},
			},
		}
		snapshots := []FileSnapshot{
			{Path: "stable.go", Size: 100, ModTime: earlier},
		}

		got, err := detector.Detect(ctx, prevManifest, "repo-1", fingerprint, snapshots)
		if err != nil {
			t.Fatalf("Detect() should have succeeded via fast-path without hashing: %v", err)
		}

		if len(got.Unchanged) != 1 || got.Unchanged[0] != "stable.go" {
			t.Errorf("got Unchanged = %v, want [stable.go]", got.Unchanged)
		}
	})

	t.Run("modtime changed but content identical marks file unchanged", func(t *testing.T) {
		hasher := &mockHasher{
			hashes: map[string]string{"touched.go": "same-hash"},
		}
		detector := &ChangeDetector{Hasher: hasher}
		prevManifest := &RepositoryManifest{
			RepositoryID:           "repo-1",
			IndexConfigFingerprint: fingerprint,
			Files: map[string]ManifestFile{
				"touched.go": {SHA256: "same-hash", Size: 100, ModTime: earlier},
			},
		}
		snapshots := []FileSnapshot{
			{Path: "touched.go", Size: 100, ModTime: now}, // ModTime advanced (e.g., git checkout)
		}

		got, err := detector.Detect(ctx, prevManifest, "repo-1", fingerprint, snapshots)
		if err != nil {
			t.Fatalf("Detect() unexpected error = %v", err)
		}

		if len(got.Unchanged) != 1 || got.Unchanged[0] != "touched.go" {
			t.Errorf("got Unchanged = %v, want [touched.go]", got.Unchanged)
		}
		if len(got.Modified) != 0 {
			t.Errorf("got Modified = %v, want empty", got.Modified)
		}
	})

	t.Run("size identical but content changed detects modification", func(t *testing.T) {
		hasher := &mockHasher{
			hashes: map[string]string{"edit.go": "new-hash"},
		}
		detector := &ChangeDetector{Hasher: hasher}
		prevManifest := &RepositoryManifest{
			RepositoryID:           "repo-1",
			IndexConfigFingerprint: fingerprint,
			Files: map[string]ManifestFile{
				"edit.go": {SHA256: "old-hash", Size: 100, ModTime: earlier},
			},
		}
		snapshots := []FileSnapshot{
			{Path: "edit.go", Size: 100, ModTime: now}, // Same size, different timestamp & content
		}

		got, err := detector.Detect(ctx, prevManifest, "repo-1", fingerprint, snapshots)
		if err != nil {
			t.Fatalf("Detect() unexpected error = %v", err)
		}

		if len(got.Modified) != 1 || got.Modified[0] != "edit.go" {
			t.Errorf("got Modified = %v, want [edit.go]", got.Modified)
		}
	})

	t.Run("detects deletions", func(t *testing.T) {
		detector := &ChangeDetector{Hasher: &mockHasher{}}
		prevManifest := &RepositoryManifest{
			RepositoryID:           "repo-1",
			IndexConfigFingerprint: fingerprint,
			Files: map[string]ManifestFile{
				"deleted.go": {SHA256: "hash1", Size: 50, ModTime: earlier},
				"kept.go":    {SHA256: "hash2", Size: 50, ModTime: earlier},
			},
		}
		snapshots := []FileSnapshot{
			{Path: "kept.go", Size: 50, ModTime: earlier},
		}

		got, err := detector.Detect(ctx, prevManifest, "repo-1", fingerprint, snapshots)
		if err != nil {
			t.Fatalf("Detect() unexpected error = %v", err)
		}

		if len(got.Deleted) != 1 || got.Deleted[0] != "deleted.go" {
			t.Errorf("got Deleted = %v, want [deleted.go]", got.Deleted)
		}
	})

	t.Run("file deleted mid-scan handles not-exist error gracefully", func(t *testing.T) {
		hasher := &mockHasher{
			errs: map[string]error{"vanished.go": os.ErrNotExist},
		}
		detector := &ChangeDetector{Hasher: hasher}
		prevManifest := &RepositoryManifest{
			RepositoryID:           "repo-1",
			IndexConfigFingerprint: fingerprint,
			Files: map[string]ManifestFile{
				"vanished.go": {SHA256: "hash1", Size: 50, ModTime: earlier},
			},
		}
		snapshots := []FileSnapshot{
			{Path: "vanished.go", Size: 50, ModTime: now}, // Trigger hash fallback, which finds it missing
		}

		got, err := detector.Detect(ctx, prevManifest, "repo-1", fingerprint, snapshots)
		if err != nil {
			t.Fatalf("Detect() unexpected error on vanished file = %v", err)
		}

		if len(got.Deleted) != 1 || got.Deleted[0] != "vanished.go" {
			t.Errorf("got Deleted = %v, want [vanished.go]", got.Deleted)
		}
	})

	t.Run("fingerprint mismatch classifies surviving files as modified", func(t *testing.T) {
		detector := &ChangeDetector{Hasher: &mockHasher{}}
		prevManifest := &RepositoryManifest{
			RepositoryID:           "repo-1",
			IndexConfigFingerprint: "old-fingerprint",
			Files: map[string]ManifestFile{
				"keep.go": {SHA256: "hash-keep", Size: 10, ModTime: earlier},
				"gone.go": {SHA256: "hash-gone", Size: 10, ModTime: earlier},
			},
		}
		snapshots := []FileSnapshot{
			{Path: "keep.go", Size: 10, ModTime: earlier},
			{Path: "new.go", Size: 20, ModTime: now},
		}

		got, err := detector.Detect(ctx, prevManifest, "repo-1", "new-fingerprint", snapshots)
		if err != nil {
			t.Fatalf("Detect() unexpected error = %v", err)
		}
		want := &ChangeSet{
			Added:       []string{"new.go"},
			Modified:    []string{"keep.go"},
			Deleted:     []string{"gone.go"},
			Unchanged:   []string{},
			FullReindex: true,
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("repository id mismatch is rejected", func(t *testing.T) {
		detector := &ChangeDetector{Hasher: &mockHasher{}}
		prevManifest := &RepositoryManifest{
			RepositoryID:           "repo-a",
			IndexConfigFingerprint: fingerprint,
			Files: map[string]ManifestFile{
				"a.go": {SHA256: "h", Size: 1, ModTime: earlier},
			},
		}
		_, err := detector.Detect(ctx, prevManifest, "repo-b", fingerprint, nil)
		if err == nil {
			t.Fatal("Detect() error = nil, want repository id mismatch")
		}
	})

	t.Run("hash io error that is not not-exist fails detect", func(t *testing.T) {
		hasher := &mockHasher{
			errs: map[string]error{"edit.go": errors.New("permission denied")},
		}
		detector := &ChangeDetector{Hasher: hasher}
		prevManifest := &RepositoryManifest{
			RepositoryID:           "repo-1",
			IndexConfigFingerprint: fingerprint,
			Files: map[string]ManifestFile{
				"edit.go": {SHA256: "old-hash", Size: 100, ModTime: earlier},
			},
		}
		snapshots := []FileSnapshot{
			{Path: "edit.go", Size: 100, ModTime: now},
		}
		if _, err := detector.Detect(ctx, prevManifest, "repo-1", fingerprint, snapshots); err == nil {
			t.Fatal("Detect() error = nil, want hash failure")
		}
	})

	t.Run("classifies added modified deleted and unchanged together", func(t *testing.T) {
		hasher := &mockHasher{
			hashes: map[string]string{"changed.go": "new-hash"},
		}
		detector := &ChangeDetector{Hasher: hasher}
		prevManifest := &RepositoryManifest{
			RepositoryID:           "repo-1",
			IndexConfigFingerprint: fingerprint,
			Files: map[string]ManifestFile{
				"stable.go":  {SHA256: "h1", Size: 10, ModTime: earlier},
				"changed.go": {SHA256: "old-hash", Size: 10, ModTime: earlier},
				"deleted.go": {SHA256: "h3", Size: 10, ModTime: earlier},
			},
		}
		snapshots := []FileSnapshot{
			{Path: "added.go", Size: 5, ModTime: now},
			{Path: "changed.go", Size: 10, ModTime: now},
			{Path: "stable.go", Size: 10, ModTime: earlier},
		}
		got, err := detector.Detect(ctx, prevManifest, "repo-1", fingerprint, snapshots)
		if err != nil {
			t.Fatalf("Detect() unexpected error = %v", err)
		}
		want := &ChangeSet{
			Added:     []string{"added.go"},
			Modified:  []string{"changed.go"},
			Deleted:   []string{"deleted.go"},
			Unchanged: []string{"stable.go"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
}
