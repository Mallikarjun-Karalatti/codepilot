package main

import (
	"context"
	"strings"
	"testing"
)

func TestCLIDispatch(t *testing.T) {
	ctx := context.Background()

	// 1. Help flag
	if err := runCLI(ctx, []string{"--help"}); err != nil {
		t.Fatalf("expected no error for --help, got: %v", err)
	}

	// 2. Unknown command
	err := runCLI(ctx, []string{"nonexistent_cmd"})
	if err == nil {
		t.Fatal("expected error for nonexistent_cmd, got nil")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("unexpected error: %v", err)
	}

	// 3. Missing arguments
	err = runCLI(ctx, []string{"index"})
	if err == nil {
		t.Fatal("expected error for index with no repo, got nil")
	}
	err = runCLI(ctx, []string{"ask", "repo"})
	if err == nil {
		t.Fatal("expected error for ask with no question, got nil")
	}
	err = runCLI(ctx, []string{"agent", "repo"})
	if err == nil {
		t.Fatal("expected error for agent with no goal, got nil")
	}
	err = runCLI(ctx, []string{"tool", "repo"})
	if err == nil {
		t.Fatal("expected error for tool with no tool name, got nil")
	}
}
