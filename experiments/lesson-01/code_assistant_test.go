package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type assistantTestEmbedder struct {
	embedding []float64
	err       error
}

func (embedder assistantTestEmbedder) Embed(string) ([]float64, error) {
	if embedder.err != nil {
		return nil, embedder.err
	}
	return embedder.embedding, nil
}

type assistantTestHTTPClient struct {
	response *http.Response
	err      error
	request  *http.Request
	body     string
	calls    int
}

func (client *assistantTestHTTPClient) Do(request *http.Request) (*http.Response, error) {
	client.request = request
	client.calls++
	if request.Body != nil {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		client.body = string(body)
	}
	if client.err != nil {
		return nil, client.err
	}
	return client.response, nil
}

type assistantTestTokenCounter struct {
	count int
}

func (counter assistantTestTokenCounter) CountTokens(string) (int, error) {
	return counter.count, nil
}

func newAssistantTestAssistant(chunks []CodeChunk, llmHTTP HTTPClient, embedErr error) *CodeAssistant {
	documents := make([]CodeDocument, len(chunks))
	for i, chunk := range chunks {
		documents[i] = CodeDocument{Chunk: chunk, Embedding: []float64{1, 0}}
	}

	return &CodeAssistant{
		SearchEngine: &CodeSearchEngine{
			Embedder:  assistantTestEmbedder{embedding: []float64{1, 0}, err: embedErr},
			Documents: documents,
		},
		ContextBuilder: &ContextBuilder{
			Documents: chunks,
			Tokenizer: assistantTestTokenCounter{count: 1},
			MaxTokens: 100,
		},
		Formatter: &ContextFormatter{},
		LLM:       &LLMClient{BaseURL: "http://test", HTTPClient: llmHTTP},
	}
}

func successfulAssistantHTTPClient() *assistantTestHTTPClient {
	return &assistantTestHTTPClient{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"message":{"content":"The code is in AuthenticateUser."}}`)),
		},
	}
}

func TestCodeAssistantAsk(t *testing.T) {
	chunk := CodeChunk{
		ID:         1,
		Text:       "func AuthenticateUser(token string) bool { return token != \"\" }",
		SourceFile: "auth.go",
		StartLine:  3,
		EndLine:    3,
		Kind:       ChunkKindFunction,
		Name:       "AuthenticateUser",
	}
	question := "Where is authentication handled?"

	t.Run("nil assistant returns error", func(t *testing.T) {
		var assistant *CodeAssistant
		if _, err := assistant.Ask(question); err == nil {
			t.Fatal("Ask() error = nil, want nil assistant error")
		}
	})

	t.Run("empty question returns error", func(t *testing.T) {
		assistant := newAssistantTestAssistant([]CodeChunk{chunk}, successfulAssistantHTTPClient(), nil)
		if _, err := assistant.Ask("  \n\t"); err == nil {
			t.Fatal("Ask() error = nil, want empty question error")
		}
	})

	t.Run("missing SearchEngine returns error", func(t *testing.T) {
		assistant := &CodeAssistant{}
		if _, err := assistant.Ask(question); err == nil || !strings.Contains(err.Error(), "search engine") {
			t.Errorf("Ask() error = %v, want missing SearchEngine error", err)
		}
	})

	t.Run("missing ContextBuilder returns error", func(t *testing.T) {
		assistant := &CodeAssistant{SearchEngine: &CodeSearchEngine{}}
		if _, err := assistant.Ask(question); err == nil || !strings.Contains(err.Error(), "context builder") {
			t.Errorf("Ask() error = %v, want missing ContextBuilder error", err)
		}
	})

	t.Run("missing Formatter returns error", func(t *testing.T) {
		assistant := &CodeAssistant{
			SearchEngine:   &CodeSearchEngine{},
			ContextBuilder: &ContextBuilder{},
		}
		if _, err := assistant.Ask(question); err == nil || !strings.Contains(err.Error(), "formatter") {
			t.Errorf("Ask() error = %v, want missing Formatter error", err)
		}
	})

	t.Run("missing LLM returns error", func(t *testing.T) {
		assistant := &CodeAssistant{
			SearchEngine:   &CodeSearchEngine{},
			ContextBuilder: &ContextBuilder{},
			Formatter:      &ContextFormatter{},
		}
		if _, err := assistant.Ask(question); err == nil || !strings.Contains(err.Error(), "LLM client") {
			t.Errorf("Ask() error = %v, want missing LLM error", err)
		}
	})

	t.Run("search failure preserves cause", func(t *testing.T) {
		searchErr := errors.New("embedding service unavailable")
		assistant := newAssistantTestAssistant([]CodeChunk{chunk}, successfulAssistantHTTPClient(), searchErr)
		if _, err := assistant.Ask(question); !errors.Is(err, searchErr) {
			t.Errorf("Ask() error = %v, want wrapped search error %v", err, searchErr)
		}
	})

	t.Run("zero search results returns without calling LLM", func(t *testing.T) {
		llmHTTP := successfulAssistantHTTPClient()
		assistant := newAssistantTestAssistant(nil, llmHTTP, nil)
		answer, err := assistant.Ask(question)
		if err != nil {
			t.Fatalf("Ask() error = %v", err)
		}
		if answer != "I couldn't find relevant code for that question." {
			t.Errorf("Ask() = %q, want no-code response", answer)
		}
		if llmHTTP.calls != 0 {
			t.Errorf("LLM calls = %d, want 0", llmHTTP.calls)
		}
	})

	t.Run("context failure preserves cause", func(t *testing.T) {
		method := chunk
		method.Kind = ChunkKindMethod
		method.ParentID = 404
		assistant := newAssistantTestAssistant([]CodeChunk{method}, successfulAssistantHTTPClient(), nil)
		if _, err := assistant.Ask(question); err == nil || !strings.Contains(err.Error(), "parent chunk 404") {
			t.Errorf("Ask() error = %v, want missing parent context error", err)
		}
	})

	t.Run("empty token-budgeted context returns no-code response", func(t *testing.T) {
		llmHTTP := successfulAssistantHTTPClient()
		assistant := newAssistantTestAssistant([]CodeChunk{chunk}, llmHTTP, nil)
		assistant.ContextBuilder.MaxTokens = 1
		assistant.ContextBuilder.Tokenizer = assistantTestTokenCounter{count: 2}
		answer, err := assistant.Ask(question)
		if err != nil {
			t.Fatalf("Ask() error = %v", err)
		}
		if answer != "I couldn't find relevant code for that question." {
			t.Errorf("Ask() = %q, want no-code response", answer)
		}
		if llmHTTP.calls != 0 {
			t.Errorf("LLM calls = %d, want 0", llmHTTP.calls)
		}
	})

	t.Run("formatted context is included in LLM request", func(t *testing.T) {
		llmHTTP := successfulAssistantHTTPClient()
		assistant := newAssistantTestAssistant([]CodeChunk{chunk}, llmHTTP, nil)
		if _, err := assistant.Ask(question); err != nil {
			t.Fatalf("Ask() error = %v", err)
		}
		var request ChatRequest
		if err := json.Unmarshal([]byte(llmHTTP.body), &request); err != nil {
			t.Fatalf("unmarshal LLM request: %v", err)
		}
		if len(request.Messages) != 1 {
			t.Fatalf("LLM request has %d messages, want 1", len(request.Messages))
		}
		content := request.Messages[0].Content
		for _, expected := range []string{
			"[FUNCTION: AuthenticateUser]",
			"Source: auth.go:3-3",
			chunk.Text,
			question,
		} {
			if !strings.Contains(content, expected) {
				t.Errorf("LLM request content %q does not contain %q", content, expected)
			}
		}
	})

	t.Run("LLM failure preserves cause", func(t *testing.T) {
		llmErr := errors.New("LLM server unavailable")
		llmHTTP := &assistantTestHTTPClient{err: llmErr}
		assistant := newAssistantTestAssistant([]CodeChunk{chunk}, llmHTTP, nil)
		if _, err := assistant.Ask(question); !errors.Is(err, llmErr) {
			t.Errorf("Ask() error = %v, want wrapped LLM error %v", err, llmErr)
		}
	})

	t.Run("successful answer is returned", func(t *testing.T) {
		assistant := newAssistantTestAssistant([]CodeChunk{chunk}, successfulAssistantHTTPClient(), nil)
		answer, err := assistant.Ask(question)
		if err != nil {
			t.Fatalf("Ask() error = %v", err)
		}
		if answer != "The code is in AuthenticateUser." {
			t.Errorf("Ask() = %q, want expected answer", answer)
		}
	})
}

func TestBuildPrompt(t *testing.T) {
	got := buildPrompt("Where is auth handled?", "[FUNCTION: AuthenticateUser]")
	want := "Answer the question using the code context below. If the context does not contain the answer, say so.\n\nCode context:\n[FUNCTION: AuthenticateUser]\n\nQuestion: Where is auth handled?"
	if got != want {
		t.Errorf("buildPrompt() = %q, want %q", got, want)
	}
}
