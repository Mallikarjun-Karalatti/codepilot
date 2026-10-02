package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
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

// ChangeSet classifies files based on comparisons between the filesystem and manifest.
type ChangeSet struct {
	Added     []string
	Modified  []string
	Deleted   []string
	Unchanged []string
}

// FileHasher defines the contract for computing content digests.
type FileHasher interface {
	HashFile(ctx context.Context, fullPath string) (string, error)
}

// SHA256FileHasher computes hex-encoded SHA-256 digests from disk.
type SHA256FileHasher struct{}

func (h *SHA256FileHasher) HashFile(ctx context.Context, fullPath string) (string, error) {
	file, err := os.Open(fullPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("read %q for hashing: %w", fullPath, err)
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
	currentConfigFingerprint string,
	snapshots []FileSnapshot,
) (*ChangeSet, error) {
	if d == nil {
		return nil, fmt.Errorf("change detector must not be nil")
	}
	if d.Hasher == nil {
		return nil, fmt.Errorf("file hasher must not be nil")
	}

	changeSet := &ChangeSet{
		Added:     make([]string, 0),
		Modified:  make([]string, 0),
		Deleted:   make([]string, 0),
		Unchanged: make([]string, 0),
	}

	// Case 1: No previous manifest -> Clean-slate initial index
	if prevManifest == nil || len(prevManifest.Files) == 0 {
		for _, snap := range snapshots {
			changeSet.Added = append(changeSet.Added, snap.Path)
		}
		sort.Strings(changeSet.Added)
		return changeSet, nil
	}

	// Case 2: Config fingerprint mismatch -> Full re-index triggered
	if prevManifest.IndexConfigFingerprint != currentConfigFingerprint {
		for _, snap := range snapshots {
			changeSet.Added = append(changeSet.Added, snap.Path)
		}
		for prevPath := range prevManifest.Files {
			changeSet.Deleted = append(changeSet.Deleted, prevPath)
		}
		sort.Strings(changeSet.Added)
		sort.Strings(changeSet.Deleted)
		return changeSet, nil
	}

	// Case 3: Incremental comparison
	seenOnDisk := make(map[string]struct{}, len(snapshots))

	for _, snap := range snapshots {
		seenOnDisk[snap.Path] = struct{}{}
		manifestFile, exists := prevManifest.Files[snap.Path]

		if !exists {
			changeSet.Added = append(changeSet.Added, snap.Path)
			continue
		}

		// Fast Path: Size and ModTime match exactly -> file was untouched
		if snap.Size == manifestFile.Size && snap.ModTime.Equal(manifestFile.ModTime) {
			changeSet.Unchanged = append(changeSet.Unchanged, snap.Path)
			continue
		}

		// Fallback: Verify cryptographic hash
		fullPath := snap.Path
		if d.RootDir != "" {
			fullPath = fmt.Sprintf("%s/%s", d.RootDir, snap.Path)
		}

		currentHash, err := d.Hasher.HashFile(ctx, fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				// File vanished during traversal
				changeSet.Deleted = append(changeSet.Deleted, snap.Path)
				continue
			}
			return nil, fmt.Errorf("hash %q: %w", snap.Path, err)
		}

		if currentHash == manifestFile.SHA256 {
			// ModTime changed, but content remained identical
			changeSet.Unchanged = append(changeSet.Unchanged, snap.Path)
		} else {
			changeSet.Modified = append(changeSet.Modified, snap.Path)
		}
	}

	// Find deleted files (existed in previous manifest, missing from disk scan)
	for prevPath := range prevManifest.Files {
		if _, exists := seenOnDisk[prevPath]; !exists {
			changeSet.Deleted = append(changeSet.Deleted, prevPath)
		}
	}

	// Deterministic sorting
	sort.Strings(changeSet.Added)
	sort.Strings(changeSet.Modified)
	sort.Strings(changeSet.Deleted)
	sort.Strings(changeSet.Unchanged)

	return changeSet, nil
}
