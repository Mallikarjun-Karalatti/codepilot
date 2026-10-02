package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRepositoryScanner(t *testing.T) {
	ctx := context.Background()

	t.Run("scans valid go files and applies default exclusions", func(t *testing.T) {
		tempDir := t.TempDir()

		files := map[string]string{
			"main.go":                    "package main",
			"main_test.go":               "package main",
			"internal/auth/auth.go":      "package auth",
			"internal/auth/auth_test.go": "package auth",
			"vendor/lib/lib.go":          "package lib",
			".git/config":                "[core]",
			"README.md":                  "# Readme",
		}

		for path, content := range files {
			fullPath := filepath.Join(tempDir, path)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
				t.Fatalf("MkdirAll failed: %v", err)
			}
			if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
				t.Fatalf("WriteFile failed: %v", err)
			}
		}

		scanner := &RepositoryScanner{Options: DefaultScannerOptions()}
		snapshots, err := scanner.Scan(ctx, tempDir)
		if err != nil {
			t.Fatalf("Scan() unexpected error = %v", err)
		}

		var gotPaths []string
		for _, s := range snapshots {
			gotPaths = append(gotPaths, s.Path)
		}

		wantPaths := []string{
			"internal/auth/auth.go",
			"main.go",
		}

		if !reflect.DeepEqual(gotPaths, wantPaths) {
			t.Fatalf("got scanned paths %v, want %v", gotPaths, wantPaths)
		}
	})

	t.Run("includes test files when configured", func(t *testing.T) {
		tempDir := t.TempDir()
		for _, file := range []string{"app.go", "app_test.go"} {
			if err := os.WriteFile(filepath.Join(tempDir, file), []byte("package app"), 0o600); err != nil {
				t.Fatalf("WriteFile failed: %v", err)
			}
		}

		scanner := &RepositoryScanner{
			Options: ScannerOptions{
				IncludeTests: true,
			},
		}

		snapshots, err := scanner.Scan(ctx, tempDir)
		if err != nil {
			t.Fatalf("Scan() error = %v", err)
		}

		var gotPaths []string
		for _, s := range snapshots {
			gotPaths = append(gotPaths, s.Path)
		}

		wantPaths := []string{
			"app.go",
			"app_test.go",
		}

		if !reflect.DeepEqual(gotPaths, wantPaths) {
			t.Fatalf("got %v, want %v", gotPaths, wantPaths)
		}
	})

	t.Run("returns error on non-existent directory", func(t *testing.T) {
		scanner := &RepositoryScanner{Options: DefaultScannerOptions()}
		_, err := scanner.Scan(ctx, filepath.Join(t.TempDir(), "nonexistent"))
		if err == nil {
			t.Fatal("Scan() want error for missing directory, got nil")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		tempDir := t.TempDir()
		_ = os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0o600)

		cancelCtx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately

		scanner := &RepositoryScanner{Options: DefaultScannerOptions()}
		_, err := scanner.Scan(cancelCtx, tempDir)
		if err == nil {
			t.Fatal("Scan() want error on canceled context, got nil")
		}
	})
}
