package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type scriptedHTTPClient struct {
	responses []string
	callCount int
}

func (s *scriptedHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if s.callCount >= len(s.responses) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(bytes.NewBufferString(`{
				"message": {
					"role": "assistant",
					"content": "Final Answer: Done."
				}
			}`)),
		}, nil
	}
	respText := s.responses[s.callCount]
	s.callCount++
	jsonBody := fmt.Sprintf(`{
		"message": {
			"role": "assistant",
			"content": %q
		}
	}`, respText)

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(jsonBody)),
	}, nil
}

func TestCodePilotAgent_MultiStepExecution(t *testing.T) {
	_, tools := setupTestRepositoryForTools(t)

	// Scripted LLM dialogue:
	// Step 1: Look up symbol AuthenticateUser
	// Step 2: Read file auth.go
	// Step 3: Final Answer
	step1 := "Thought: I need to locate where AuthenticateUser is defined.\nAction: find_symbol({\"name\": \"AuthenticateUser\"})"
	step2 := "Thought: Let me inspect lines 1 to 10 of auth.go.\nAction: read_file({\"path\": \"auth.go\", \"start_line\": 1, \"end_line\": 10})"
	step3 := "Thought: I now have the full implementation details.\nFinal Answer:\nAuthenticateUser is defined in auth.go on lines 7-9 and delegates token validation to Database.ValidateToken."

	client := &scriptedHTTPClient{
		responses: []string{step1, step2, step3},
	}
	llm := &LLMClient{
		BaseURL:    "http://test",
		Model:      "test-model",
		HTTPClient: client,
	}

	agent := NewCodePilotAgent(tools, llm)
	res, err := agent.Run(context.Background(), "Explain how authentication is implemented")
	if err != nil {
		t.Fatalf("agent.Run() error = %v", err)
	}

	if !res.Completed {
		t.Errorf("expected Completed = true, got false")
	}
	if len(res.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(res.Steps))
	}
	if res.Steps[0].ToolName != "find_symbol" {
		t.Errorf("step 1 tool = %s, want find_symbol", res.Steps[0].ToolName)
	}
	if res.Steps[1].ToolName != "read_file" {
		t.Errorf("step 2 tool = %s, want read_file", res.Steps[1].ToolName)
	}
	if !strings.Contains(res.Answer, "delegates token validation") {
		t.Errorf("unexpected answer: %s", res.Answer)
	}
}

func TestCodePilotAgent_LoopDetection(t *testing.T) {
	_, tools := setupTestRepositoryForTools(t)

	// Scripted LLM repeats identical action twice, then receives intervention warning and completes
	repeatedStep := "Thought: Repeating action.\nAction: list_files({})"
	finalStep := "Thought: I will summarize now.\nFinal Answer:\nFiles are auth.go and db.go."

	client := &scriptedHTTPClient{
		responses: []string{repeatedStep, repeatedStep, repeatedStep, finalStep},
	}
	llm := &LLMClient{
		BaseURL:    "http://test",
		Model:      "test-model",
		HTTPClient: client,
	}

	agent := NewCodePilotAgent(tools, llm)
	res, err := agent.Run(context.Background(), "List all files")
	if err != nil {
		t.Fatalf("agent.Run() error = %v", err)
	}

	if !res.Completed {
		t.Errorf("expected agent to recover and complete, got false")
	}
	// Verify intervention observation was sent
	hasIntervention := false
	for _, s := range res.Steps {
		if strings.Contains(s.Observation, "Repeated action detected") {
			hasIntervention = true
			break
		}
	}
	if !hasIntervention {
		t.Error("expected loop intervention in steps")
	}
}
