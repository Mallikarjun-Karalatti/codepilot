package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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

func main() {
	requestBody := ChatRequest{
		Model: "qwen3:8b",
		Messages: []Message{
			{Role: "user",
				Content: "Explain what an API is in 3 sentences.",
			},
		},
		Stream: false,
	}

	data, err := json.Marshal(requestBody)
	if err != nil {
		panic(err)
	}

	request, err := http.NewRequest(
		http.MethodPost,
		"http://localhost:11434/api/chat",
		bytes.NewBuffer(data),
	)
	if err != nil {
		panic(err)
	}

	request.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()

	fmt.Println("Status:", response.Status)

	var chatResponse ChatResponse

	err = json.NewDecoder(response.Body).Decode(&chatResponse)
	if err != nil {
		panic(err)
	}

	fmt.Println("Answer:", chatResponse.Message.Content)
}
