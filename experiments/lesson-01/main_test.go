package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type FakeHTTPClient struct {
	Response *http.Response
	Err      error
}

func (f FakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return f.Response, f.Err
}

func TestChat(t *testing.T) {
	tests := []struct {
		name        string
		response    *http.Response
		clientError error
		want        string
		wantErr     bool
	}{
		{
			name: "success",
			response: &http.Response{
				StatusCode: 200,
				Body: io.NopCloser(strings.NewReader(`{
					"message": {
						"role": "assistant",
						"content": "Paris"
					}
				}`)),
			},
			want:    "Paris",
			wantErr: false,
		},
		{
			name: "http error",
			response: &http.Response{
				StatusCode: 500,
				Body:       io.NopCloser(strings.NewReader("Internal Server Error")),
			},
			wantErr: true,
		},
		{
			name:        "network error",
			clientError: errors.New("connection refused"),
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &LLMClient{
				BaseURL: "http://fake-server",
				Model:   "qwen3:8b",
				HTTPClient: FakeHTTPClient{
					Response: tt.response,
					Err:      tt.clientError,
				},
			}

			got, err := client.Chat("What is the capital of France?")
			if (err != nil) != tt.wantErr {
				t.Errorf("Chat() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("Chat() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChat_HTTP(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			var requestBody ChatRequest

			err := json.NewDecoder(r.Body).Decode(&requestBody)
			if err != nil {
				t.Fatalf("failed to decode request body: %v", err)
			}

			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}

			if r.URL.Path != "/api/chat" {
				t.Errorf("expected /api/chat, got %s", r.URL.Path)
			}

			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf(
					"expected application/json, got %s",
					r.Header.Get("Content-Type"),
				)
			}

			if requestBody.Model != "qwen3:8b" {
				t.Errorf("expected model qwen3:8b, got %s", requestBody.Model)
			}

			if len(requestBody.Messages) != 1 {
				t.Fatalf("expected 1 message, got %d", len(requestBody.Messages))
			}

			if requestBody.Messages[0].Role != "user" {
				t.Errorf(
					"expected role user, got %s",
					requestBody.Messages[0].Role,
				)
			}

			if requestBody.Messages[0].Content != "What is the capital of France?" {
				t.Errorf(
					"unexpected prompt: %s",
					requestBody.Messages[0].Content,
				)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, err = fmt.Fprint(w, `{
				"message": {
					"role": "assistant",
					"content": "Paris"
				}
			}`)
			if err != nil {
				t.Fatalf("failed to write response: %v", err)
			}
		}),
	)
	defer server.Close()

	client := NewLLMClient(server.URL, "qwen3:8b")
	answer, err := client.Chat("What is the capital of France?")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if answer != "Paris" {
		t.Fatalf("expected Paris, got %s", answer)
	}
}
