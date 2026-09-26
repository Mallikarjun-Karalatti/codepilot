package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type ChatResponse struct {
	Model   string  `json:"model"`
	Message Message `json:"message"`
	Done    bool    `json:"done"`
}

type LLMClient struct {
	BaseURL    string
	Model      string
	HTTPClient HTTPClient
}

type HTTPClient interface {
	Do(request *http.Request) (*http.Response, error)
}

func NewLLMClient(baseURL string, model string) *LLMClient {
	return &LLMClient{
		BaseURL: baseURL,
		Model:   model,
		HTTPClient: &http.Client{
			Timeout: 100 * time.Second,
		},
	}
}

func (client *LLMClient) Chat(prompt string) (string, error) {

	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	requestBody := ChatRequest{
		Model: client.Model,
		Messages: []Message{
			{Role: "user",
				Content: prompt,
			},
		},
		Stream: false,
	}

	data, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}

	request, err := http.NewRequest(
		http.MethodPost,
		client.BaseURL+"/api/chat",
		bytes.NewBuffer(data),
	)
	if err != nil {
		return "", err
	}

	request.Header.Set("Content-Type", "application/json")

	response, err := httpClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	if !(response.StatusCode >= 200 && response.StatusCode <= 299) {
		return "", fmt.Errorf(
			"request failed with status code: %d",
			response.StatusCode,
		)
	}

	var chatResponse ChatResponse

	err = json.NewDecoder(response.Body).Decode(&chatResponse)
	if err != nil {
		return "", err
	}

	return chatResponse.Message.Content, nil
}

func main() {
	client := NewLLMClient("http://localhost:11434", "qwen3:8b")

	answer, err := client.Chat("What is the capital of France?")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("Answer:", answer)
}
