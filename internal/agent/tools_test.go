package agent

import (
	"strings"
	"testing"
)

func TestCodePilotTools_ListAndRead(t *testing.T) {
	_, tools := setupTestRepositoryForTools(t)

	// 1. ListFiles
	files, err := tools.ListFiles("")
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %v", files)
	}

	// 2. ReadFile
	content, err := tools.ReadFile("auth.go", 1, 5)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(content.Content, "AuthService") {
		t.Errorf("expected AuthService in read content, got:\n%s", content.Content)
	}

	// 3. ReadFile Path Traversal Security
	_, err = tools.ReadFile("../secret.txt", 1, 10)
	if err == nil {
		t.Fatal("expected error on path traversal, got nil")
	}
}

func TestCodePilotTools_SymbolsAndRelationships(t *testing.T) {
	_, tools := setupTestRepositoryForTools(t)

	// 1. FindSymbol
	symbols, err := tools.FindSymbol("AuthenticateUser")
	if err != nil {
		t.Fatalf("FindSymbol() error = %v", err)
	}
	if len(symbols) == 0 || symbols[0].Name != "AuthenticateUser" {
		t.Fatalf("expected to find AuthenticateUser, got %v", symbols)
	}

	// 2. FindCallees
	callees, err := tools.FindCallees("AuthenticateUser")
	if err != nil {
		t.Fatalf("FindCallees() error = %v", err)
	}
	if len(callees) == 0 {
		t.Fatalf("expected AuthenticateUser to call ValidateToken, got none")
	}
	if callees[0].Name != "ValidateToken" {
		t.Errorf("callee = %s, want ValidateToken", callees[0].Name)
	}

	// 3. FindCallers
	callers, err := tools.FindCallers("ValidateToken")
	if err != nil {
		t.Fatalf("FindCallers() error = %v", err)
	}
	if len(callers) == 0 {
		t.Fatalf("expected ValidateToken to be called by AuthenticateUser, got none")
	}
	if callers[0].Name != "AuthenticateUser" {
		t.Errorf("caller = %s, want AuthenticateUser", callers[0].Name)
	}

	// 4. File Outline
	outline, err := tools.GetFileOutline("auth.go")
	if err != nil {
		t.Fatalf("GetFileOutline() error = %v", err)
	}
	if len(outline) != 2 { // AuthService struct + AuthenticateUser method
		t.Fatalf("expected 2 symbols in outline, got %d", len(outline))
	}
}

func TestCodePilotTools_ExecuteTool(t *testing.T) {
	_, tools := setupTestRepositoryForTools(t)

	res, err := tools.ExecuteTool("list_files", map[string]any{})
	if err != nil {
		t.Fatalf("ExecuteTool(list_files) error = %v", err)
	}
	if !strings.Contains(res, "auth.go") {
		t.Errorf("result missing auth.go: %s", res)
	}

	// Unknown tool error
	_, err = tools.ExecuteTool("unknown_tool", map[string]any{})
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}
