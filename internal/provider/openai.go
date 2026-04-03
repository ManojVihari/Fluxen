package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type OpenAIRequest struct {
	Model    string        `json:"model"`
	Messages []OAIMsg      `json:"messages"`
}

type OAIMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func GenerateOpenAI(apiKey string, model string, prompt string) (string, error) {

	reqBody := OpenAIRequest{
		Model: model,
		Messages: []OAIMsg{
			{Role: "user", Content: prompt},
		},
	}

	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequest(
		"POST",
		"https://api.openai.com/v1/chat/completions",
		bytes.NewBuffer(body),
	)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	responseBody, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	json.Unmarshal(responseBody, &result)

	// Extract assistant response
	choices, ok := result["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("invalid OpenAI response")
	}

	first := choices[0].(map[string]interface{})
	message := first["message"].(map[string]interface{})

	if content, ok := message["content"].(string); ok {
		return content, nil
	}

	return "", nil
}