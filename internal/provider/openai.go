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

type OpenAIRequest struct {
	Model    string   `json:"model"`
	Messages []OAIMsg `json:"messages"`
}

type OAIMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func GenerateOpenAI(apiKey string, model string, prompt string) (string, error) {
	log.Printf("🌐 [OpenAI] Preparing request | Model: %s | Prompt length: %d chars", model, len(prompt))

	reqBody := OpenAIRequest{
		Model: model,
		Messages: []OAIMsg{
			{Role: "user", Content: prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("❌ [OpenAI] Failed to marshal request: %v", err)
		return "", err
	}

	req, err := http.NewRequest(
		"POST",
		"https://api.openai.com/v1/chat/completions",
		bytes.NewBuffer(body),
	)
	if err != nil {
		log.Printf("❌ [OpenAI] Failed to create request: %v", err)
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	log.Printf("🌐 [OpenAI] Sending HTTP POST request to: https://api.openai.com/v1/chat/completions")

	client := &http.Client{}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("❌ [OpenAI] Request failed: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	log.Printf("🌐 [OpenAI] Response received | Status: %d | Took: %v", resp.StatusCode, time.Since(start))

	if resp.StatusCode != 200 {
		log.Printf("❌ [OpenAI] Non-200 status code: %d", resp.StatusCode)
		return "", fmt.Errorf("openai returned status %d", resp.StatusCode)
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("❌ [OpenAI] Failed to read response body: %v", err)
		return "", err
	}
	log.Printf("🌐 [OpenAI] Response body size: %d bytes", len(responseBody))

	var result map[string]interface{}
	err = json.Unmarshal(responseBody, &result)
	if err != nil {
		log.Printf("❌ [OpenAI] Failed to unmarshal response: %v", err)
		return "", err
	}

	// Extract assistant response
	choices, ok := result["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		log.Printf("❌ [OpenAI] Invalid response - no choices found")
		return "", fmt.Errorf("invalid OpenAI response")
	}

	first := choices[0].(map[string]interface{})
	message := first["message"].(map[string]interface{})

	if content, ok := message["content"].(string); ok {
		if content == "" {
			log.Printf("⚠️ [OpenAI] Response content is empty")
			return "", fmt.Errorf("empty response from openai")
		}
		log.Printf("✅ [OpenAI] Successfully extracted response | Length: %d chars", len(content))
		return content, nil
	}

	log.Printf("❌ [OpenAI] Failed to extract content field from message")
	return "", fmt.Errorf("no content field in openai response")
}
