package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPServerLifecycle(t *testing.T) {
	_, tools := setupTestRepositoryForTools(t)
	server := NewMCPServer(tools)

	// Simulated incoming JSON-RPC stream
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list_files","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"unknown_method"}`,
	}

	input := strings.Join(requests, "\n") + "\n"
	var output bytes.Buffer

	err := server.Serve(context.Background(), strings.NewReader(input), &output)
	if err != nil {
		t.Fatalf("server.Serve() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 5 { // notifications/initialized produces no response
		t.Fatalf("expected 5 responses, got %d:\n%s", len(lines), output.String())
	}

	// 1. Validate initialize response
	var initResp jsonRPCResponse
	if err := json.Unmarshal([]byte(lines[0]), &initResp); err != nil {
		t.Fatalf("unmarshal init response: %v", err)
	}
	if initResp.ID.(float64) != 1 {
		t.Errorf("init id = %v, want 1", initResp.ID)
	}
	resMap := initResp.Result.(map[string]any)
	serverInfo := resMap["serverInfo"].(map[string]any)
	if serverInfo["name"] != "codepilot-mcp" {
		t.Errorf("server name = %v, want 'codepilot-mcp'", serverInfo["name"])
	}

	// 2. Validate ping response
	var pingResp jsonRPCResponse
	if err := json.Unmarshal([]byte(lines[1]), &pingResp); err != nil {
		t.Fatalf("unmarshal ping response: %v", err)
	}
	if pingResp.ID.(float64) != 2 {
		t.Errorf("ping id = %v, want 2", pingResp.ID)
	}

	// 3. Validate tools/list response
	var toolsResp jsonRPCResponse
	if err := json.Unmarshal([]byte(lines[2]), &toolsResp); err != nil {
		t.Fatalf("unmarshal tools/list response: %v", err)
	}
	toolsMap := toolsResp.Result.(map[string]any)
	toolList := toolsMap["tools"].([]any)
	if len(toolList) < 6 {
		t.Errorf("expected at least 6 tools, got %d", len(toolList))
	}

	// 4. Validate tools/call response
	var callResp jsonRPCResponse
	if err := json.Unmarshal([]byte(lines[3]), &callResp); err != nil {
		t.Fatalf("unmarshal tools/call response: %v", err)
	}
	callMap := callResp.Result.(map[string]any)
	contentList := callMap["content"].([]any)
	firstContent := contentList[0].(map[string]any)
	textVal := firstContent["text"].(string)
	if !strings.Contains(textVal, "auth.go") {
		t.Errorf("tools/call text missing auth.go: %s", textVal)
	}

	// 5. Validate method not found error
	var errResp jsonRPCResponse
	if err := json.Unmarshal([]byte(lines[4]), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp.Error == nil || errResp.Error.Code != mcpErrMethodNotFound {
		t.Errorf("expected method not found error (-32601), got %+v", errResp.Error)
	}
}
