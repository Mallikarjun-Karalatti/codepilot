package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type tokenCountHTTPClient struct {
	statusCode int
	response   string
	request    *http.Request
	calls      int
}

func (client *tokenCountHTTPClient) Do(request *http.Request) (*http.Response, error) {
	client.request = request
	client.calls++
	return &http.Response{
		StatusCode: client.statusCode,
		Body:       io.NopCloser(strings.NewReader(client.response)),
	}, nil
}

func TestEmbeddingClientCountTokens(t *testing.T) {
	httpClient := &tokenCountHTTPClient{
		statusCode: http.StatusOK,
		response:   `{"model":"qwen3-embedding","embeddings":[[0.1,0.2]],"prompt_eval_count":7}`,
	}
	client := &EmbeddingClient{
		BaseURL:    "http://localhost:11434",
		Model:      "qwen3-embedding",
		HTTPClient: httpClient,
	}

	got, err := client.CountTokens("func Add() {}")
	if err != nil {
		t.Fatalf("CountTokens() error = %v", err)
	}
	if got != 7 {
		t.Errorf("CountTokens() = %d, want 7", got)
	}
	if httpClient.request.Method != http.MethodPost || httpClient.request.URL.Path != "/api/embed" {
		t.Errorf("request = %s %s, want POST /api/embed", httpClient.request.Method, httpClient.request.URL.Path)
	}
	if httpClient.request.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", httpClient.request.Header.Get("Content-Type"))
	}
}

func TestEmbeddingClientCountTokensMissingCount(t *testing.T) {
	client := &EmbeddingClient{
		BaseURL: "http://localhost:11434",
		Model:   "qwen3-embedding",
		HTTPClient: &tokenCountHTTPClient{
			statusCode: http.StatusOK,
			response:   `{"embeddings":[[0.1,0.2]]}`,
		},
	}

	if _, err := client.CountTokens("some text"); err == nil {
		t.Fatal("CountTokens() error = nil, want missing token count error")
	}
}

func TestEmbeddingClientCountTokensEmptyInput(t *testing.T) {
	httpClient := &tokenCountHTTPClient{statusCode: http.StatusOK}
	client := &EmbeddingClient{HTTPClient: httpClient}

	got, err := client.CountTokens("")
	if err != nil {
		t.Fatalf("CountTokens() error = %v", err)
	}
	if got != 0 {
		t.Errorf("CountTokens() = %d, want 0", got)
	}
	if httpClient.calls != 0 {
		t.Errorf("HTTP calls = %d, want 0 for empty input", httpClient.calls)
	}
}
