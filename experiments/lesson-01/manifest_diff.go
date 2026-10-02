package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// RepositoryManifest represents the persisted metadata state of an indexed repository.
type RepositoryManifest struct {
	SchemaVersion          int                     `json:"schema_version"`
	RepositoryID           string                  `json:"repository_id"`
	IndexConfigFingerprint string                  `json:"index_config_fingerprint"`
	GeneratedAt            time.Time               `json:"generated_at"`
	ExcludedDirectories    []string                `json:"excluded_directories,omitempty"`
	IncludeTests           bool                    `json:"include_tests"`
	Files                  map[string]ManifestFile `json:"files"`
}

// ManifestFile stores metadata and chunk references for a tracked source file.
type ManifestFile struct {
	SHA256   string    `json:"sha256"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
	ChunkIDs []int     `json:"chunk_ids,omitempty"`
}

// FileSnapshot represents point-in-time filesystem metadata for a file.
type FileSnapshot struct {
	Path    string // Normalized relative path (forward slashes)
	Size    int64
	ModTime time.Time
}

// ChangeSet classifies eligible files against the previous manifest.
type ChangeSet struct {
	Added       []string
	Modified    []string
	Deleted     []string
	Unchanged   []string
	FullReindex bool
}

// FileHasher defines the contract for computing content digests.
type FileHasher interface {
	HashFile(ctx context.Context, fullPath string) (string, error)
}

// SHA256FileHasher computes hex-encoded SHA-256 digests from disk.
type SHA256FileHasher struct{}

func (h *SHA256FileHasher) HashFile(ctx context.Context, fullPath string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	file, err := os.Open(fullPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	buf := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := file.Read(buf)
		if n > 0 {
			if _, err := hasher.Write(buf[:n]); err != nil {
				return "", fmt.Errorf("hash %q: %w", fullPath, err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", fmt.Errorf("read %q for hashing: %w", fullPath, readErr)
		}
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// ChangeDetector determines file status changes against a previous manifest.
type ChangeDetector struct {
	Hasher  FileHasher
	RootDir string
}

// Detect computes the ChangeSet comparing current snapshots against the previous manifest.
func (d *ChangeDetector) Detect(
	ctx context.Context,
	prevManifest *RepositoryManifest,
	currentRepositoryID string,
	currentConfigFingerprint string,
	snapshots []FileSnapshot,
) (*ChangeSet, error) {
	if d == nil {
		return nil, fmt.Errorf("change detector must not be nil")
	}
	if d.Hasher == nil {
		return nil, fmt.Errorf("file hasher must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	changeSet := emptyChangeSet()

	if prevManifest != nil && prevManifest.RepositoryID != "" && currentRepositoryID != "" &&
		prevManifest.RepositoryID != currentRepositoryID {
		return nil, fmt.Errorf(
			"repository id mismatch: manifest %q vs current %q",
			prevManifest.RepositoryID,
			currentRepositoryID,
		)
	}

	// No previous index: every eligible file is added.
	if prevManifest == nil || len(prevManifest.Files) == 0 {
		for _, snap := range snapshots {
			changeSet.Added = append(changeSet.Added, snap.Path)
		}
		sort.Strings(changeSet.Added)
		return changeSet, nil
	}

	seenOnDisk := make(map[string]FileSnapshot, len(snapshots))
	for _, snap := range snapshots {
		seenOnDisk[snap.Path] = snap
	}

	// Config fingerprint mismatch: indexed representations are invalid.
	// Surviving paths are Modified (must be reprocessed), not Added+Deleted.
	if prevManifest.IndexConfigFingerprint != currentConfigFingerprint {
		changeSet.FullReindex = true
		for _, snap := range snapshots {
			if _, existed := prevManifest.Files[snap.Path]; existed {
				changeSet.Modified = append(changeSet.Modified, snap.Path)
			} else {
				changeSet.Added = append(changeSet.Added, snap.Path)
			}
		}
		for prevPath := range prevManifest.Files {
			if _, exists := seenOnDisk[prevPath]; !exists {
				changeSet.Deleted = append(changeSet.Deleted, prevPath)
			}
		}
		sortChangeSet(changeSet)
		return changeSet, nil
	}

	for _, snap := range snapshots {
		manifestFile, exists := prevManifest.Files[snap.Path]
		if !exists {
			changeSet.Added = append(changeSet.Added, snap.Path)
			continue
		}

		if snap.Size == manifestFile.Size && snap.ModTime.Equal(manifestFile.ModTime) {
			changeSet.Unchanged = append(changeSet.Unchanged, snap.Path)
			continue
		}

		currentHash, err := d.Hasher.HashFile(ctx, d.fullPath(snap.Path))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || os.IsNotExist(err) {
				changeSet.Deleted = append(changeSet.Deleted, snap.Path)
				continue
			}
			return nil, fmt.Errorf("hash %q: %w", snap.Path, err)
		}

		if currentHash == manifestFile.SHA256 {
			changeSet.Unchanged = append(changeSet.Unchanged, snap.Path)
		} else {
			changeSet.Modified = append(changeSet.Modified, snap.Path)
		}
	}

	for prevPath := range prevManifest.Files {
		if _, exists := seenOnDisk[prevPath]; !exists {
			changeSet.Deleted = append(changeSet.Deleted, prevPath)
		}
	}

	sortChangeSet(changeSet)
	return changeSet, nil
}

func (d *ChangeDetector) fullPath(relative string) string {
	if d.RootDir == "" {
		return relative
	}
	return filepath.Join(d.RootDir, filepath.FromSlash(relative))
}

func emptyChangeSet() *ChangeSet {
	return &ChangeSet{
		Added:     make([]string, 0),
		Modified:  make([]string, 0),
		Deleted:   make([]string, 0),
		Unchanged: make([]string, 0),
	}
}

func sortChangeSet(changeSet *ChangeSet) {
	sort.Strings(changeSet.Added)
	sort.Strings(changeSet.Modified)
	sort.Strings(changeSet.Deleted)
	sort.Strings(changeSet.Unchanged)
}
