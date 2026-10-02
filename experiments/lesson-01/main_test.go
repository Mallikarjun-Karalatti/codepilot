package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type FakeHTTPClient struct {
	Response *http.Response
	Err      error
}

type FakeEmbedder struct {
	Embeddings map[string][]float64
}

func (f FakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return f.Response, f.Err
}

func (f FakeEmbedder) Embed(text string) ([]float64, error) {
	if embedding, ok := f.Embeddings[text]; ok {
		return embedding, nil
	}
	return nil, fmt.Errorf("embedding not found for text: %s", text)
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

			messages := []Message{
				{Role: "user", Content: "What is the capital of France?"},
			}
			got, err := client.Chat(messages)
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
			if requestBody.Options["temperature"] != float64(0) || requestBody.Options["num_predict"] != float64(64) {
				t.Errorf("request options = %v, want temperature=0 and num_predict=64", requestBody.Options)
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
	client.Options = map[string]any{"temperature": 0.0, "num_predict": 64}
	messages := []Message{
		{Role: "user", Content: "What is the capital of France?"},
	}
	answer, err := client.Chat(messages)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if answer != "Paris" {
		t.Fatalf("expected Paris, got %s", answer)
	}
}

func TestSearch(t *testing.T) {
	// Create a fake embedder with predefined embeddings
	fake := FakeEmbedder{
		Embeddings: map[string][]float64{
			"password": {1, 0},
			"weather":  {0, 1},
		},
	}

	engine := &SearchEngine{
		Embedder: fake,
	}

	engine.Documents = []Document{
		{
			ID:        1,
			Text:      "password",
			Embedding: []float64{1, 0},
		},
		{
			ID:        2,
			Text:      "weather",
			Embedding: []float64{0, 1},
		},
	}

	tests := []struct {
		name          string
		query         string
		limit         int
		documents     []Document
		wantErr       bool
		wantCount     int
		wantFirstText string
		wantScore     float64
	}{
		{
			name:          "correct ranking",
			query:         "password",
			limit:         2,
			documents:     engine.Documents,
			wantCount:     2,
			wantFirstText: "password",
			wantScore:     1.0,
		},
		{
			name:          "respects limit",
			query:         "weather",
			limit:         1,
			documents:     engine.Documents,
			wantCount:     1,
			wantFirstText: "weather",
			wantScore:     1.0,
		}, {
			name:      "embedding failure",
			query:     "unknown",
			limit:     2,
			documents: engine.Documents,
			wantErr:   true,
		},
		{
			name:      "empty documents",
			query:     "password",
			limit:     2,
			documents: nil,
			wantCount: 0,
		},
		{
			name:      "invalid limit",
			query:     "password",
			limit:     0,
			documents: engine.Documents,
			wantErr:   true,
		},
		{
			name:  "embedding dimension mismatch",
			query: "password",
			limit: 2,
			documents: []Document{
				{
					ID:        1,
					Text:      "bad document",
					Embedding: []float64{1, 0, 0},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := &SearchEngine{
				Embedder:  fake,
				Documents: tt.documents,
			}

			results, err := engine.Search(tt.query, tt.limit)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Search() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			if len(results) != tt.wantCount {
				t.Fatalf(
					"Search() got %d results, want %d",
					len(results),
					tt.wantCount,
				)
			}

			if len(results) > 0 {
				if results[0].Document.Text != tt.wantFirstText {
					t.Errorf(
						"first document = %q, want %q",
						results[0].Document.Text,
						tt.wantFirstText,
					)
				}

				if math.Abs(results[0].Score-tt.wantScore) > 1e-9 {
					t.Errorf(
						"first score = %v, want %v",
						results[0].Score,
						tt.wantScore,
					)
				}
			}
		})
	}
}
