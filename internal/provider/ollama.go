package provider

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

type OllamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

func GenerateOllama(model string, prompt string) (string, error) {
	reqBody := OllamaRequest{
		Model:  model,
		Prompt: prompt,
		Stream: false,
	}

	body, _ := json.Marshal(reqBody)

	resp, err := http.Post(
		"http://localhost:11434/api/generate",
		"application/json",
		bytes.NewBuffer(body),
	)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	responseBody, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	json.Unmarshal(responseBody, &result)

	if val, ok := result["response"].(string); ok {
		return val, nil
	}

	return "", nil
}