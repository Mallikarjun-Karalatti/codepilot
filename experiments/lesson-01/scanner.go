package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ScannerOptions configures filesystem traversal policies.
type ScannerOptions struct {
	ExcludedDirectories []string
	IncludeTests        bool
}

// DefaultScannerOptions returns default exclusion rules for CodePilot.
func DefaultScannerOptions() ScannerOptions {
	return ScannerOptions{
		ExcludedDirectories: []string{".git", "vendor"},
		IncludeTests:        false,
	}
}

// RepositoryScanner walks a codebase and produces normalized FileSnapshot records.
type RepositoryScanner struct {
	Options ScannerOptions
}

// Scan walks rootDir and generates a deterministically sorted slice of FileSnapshot entries.
func (s *RepositoryScanner) Scan(ctx context.Context, rootDir string) ([]FileSnapshot, error) {
	if s == nil {
		return nil, fmt.Errorf("repository scanner must not be nil")
	}

	cleanRoot := filepath.Clean(rootDir)
	info, err := os.Stat(cleanRoot)
	if err != nil {
		return nil, fmt.Errorf("stat repository root %q: %w", rootDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repository root %q is not a directory", rootDir)
	}

	excludedDirs := make(map[string]struct{}, len(s.Options.ExcludedDirectories))
	for _, dir := range s.Options.ExcludedDirectories {
		trimmed := strings.Trim(dir, "/\\")
		if trimmed != "" {
			excludedDirs[trimmed] = struct{}{}
		}
	}
	// .git is unconditionally excluded regardless of user options
	excludedDirs[".git"] = struct{}{}

	var snapshots []FileSnapshot

	err = filepath.WalkDir(cleanRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if d.IsDir() {
			if path == cleanRoot {
				return nil
			}
			name := d.Name()
			if _, excluded := excludedDirs[name]; excluded {
				return filepath.SkipDir
			}
			return nil
		}

		// Only Go source files
		if filepath.Ext(path) != ".go" {
			return nil
		}

		// Skip tests if not explicitly included
		if !s.Options.IncludeTests && strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}

		relPath, err := filepath.Rel(cleanRoot, path)
		if err != nil {
			return fmt.Errorf("rel path for %q: %w", path, err)
		}
		normalizedPath := filepath.ToSlash(relPath)

		fileInfo, err := d.Info()
		if err != nil {
			if os.IsNotExist(err) {
				// File deleted during traversal; skip gracefully
				return nil
			}
			return fmt.Errorf("file info for %q: %w", path, err)
		}

		snapshots = append(snapshots, FileSnapshot{
			Path:    normalizedPath,
			Size:    fileInfo.Size(),
			ModTime: fileInfo.ModTime(),
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walk repository %q: %w", rootDir, err)
	}

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Path < snapshots[j].Path
	})

	return snapshots, nil
}
