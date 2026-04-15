package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type OllamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

func GenerateOllama(model string, prompt string) (string, error) {
	log.Printf("🌐 [Ollama] Preparing request | Model: %s | Prompt length: %d chars", model, len(prompt))

	reqBody := OllamaRequest{
		Model:  model,
		Prompt: prompt,
		Stream: false,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("❌ [Ollama] Failed to marshal request: %v", err)
		return "", err
	}
	log.Printf("🌐 [Ollama] Sending HTTP POST request to: http://localhost:11434/api/generate")

	start := time.Now()
	resp, err := http.Post(
		"http://localhost:11434/api/generate",
		"application/json",
		bytes.NewBuffer(body),
	)
	if err != nil {
		log.Printf("❌ [Ollama] Request failed: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	log.Printf("🌐 [Ollama] Response received | Status: %d | Took: %v", resp.StatusCode, time.Since(start))

	if resp.StatusCode != 200 {
		log.Printf("❌ [Ollama] Non-200 status code: %d", resp.StatusCode)
		return "", fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("❌ [Ollama] Failed to read response body: %v", err)
		return "", err
	}
	log.Printf("🌐 [Ollama] Response body size: %d bytes", len(responseBody))

	var result map[string]interface{}
	err = json.Unmarshal(responseBody, &result)
	if err != nil {
		log.Printf("❌ [Ollama] Failed to unmarshal response: %v", err)
		return "", err
	}

	if val, ok := result["response"].(string); ok {
		if val == "" {
			log.Printf("⚠️ [Ollama] Response field is empty")
			return "", fmt.Errorf("empty response from ollama")
		}
		log.Printf("✅ [Ollama] Successfully extracted response | Length: %d chars", len(val))
		return val, nil
	}

	log.Printf("❌ [Ollama] Failed to extract response field from result. Got: %v", result)
	return "", fmt.Errorf("no response field in ollama output")
}
