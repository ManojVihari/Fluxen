package gateway

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ManojVihari/Fluxen/internal/analyzer"
	"github.com/ManojVihari/Fluxen/internal/auth"
	"github.com/ManojVihari/Fluxen/internal/repository"
	"github.com/ManojVihari/Fluxen/internal/models"
	"github.com/ManojVihari/Fluxen/internal/provider"
	"github.com/google/uuid"
)

func ChatHandler(w http.ResponseWriter, r *http.Request) {

	// -------- AUTH --------
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
		return
	}

	apiKeyRaw := strings.TrimPrefix(authHeader, "Bearer ")
	if apiKeyRaw == "" {
		http.Error(w, "Invalid Authorization format", http.StatusUnauthorized)
		return
	}

	// Hash API key
	keyHash := auth.HashAPIKey(apiKeyRaw)

	// Fetch API key from DB
	apiKey, err := repository.GetAPIKey(keyHash)
	if err != nil {
		log.Println("Auth error:", err)
		http.Error(w, "Invalid API Key", http.StatusUnauthorized)
		return
	}

	// -------- BUDGET CHECK --------
	currentUsage, err := repository.GetMonthlyUsage(apiKey.ID)
	if err != nil {
		http.Error(w, "Failed to fetch usage", http.StatusInternalServerError)
		return
	}

	if currentUsage >= apiKey.Budget {
		http.Error(w, "Monthly budget exceeded", http.StatusForbidden)
		return
	}

	// -------- PARSE REQUEST --------
	var chatReq models.ChatRequest

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request", http.StatusInternalServerError)
		return
	}

	err = json.Unmarshal(body, &chatReq)
	if err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// -------- BUILD PROMPT --------
	prompt := ""
	for _, msg := range chatReq.Messages {
		prompt += msg.Role + ": " + msg.Content + "\n"
	}

	// -------- ANALYZE --------
	promptTokens := analyzer.EstimateTokens(prompt)

	log.Printf("Model: %s | Prompt Tokens: %d",
		chatReq.Model,
		promptTokens,
	)

	// -------- CALL PROVIDER --------

	providerName := apiKey.Provider

	assistantText, err := provider.Generate(
		providerName,
		apiKey.ProviderAPIKey,
		chatReq.Model,
		prompt,
	)
	if err != nil {
		http.Error(w, "Provider error", http.StatusInternalServerError)
		return
	}

	// -------- RESPONSE ANALYSIS --------
	completionTokens := analyzer.EstimateTokens(assistantText)
	totalTokens := promptTokens + completionTokens
	totalCost := analyzer.EstimateCost(totalTokens)

	requestID := uuid.New().String()

	// -------- RESPONSE --------
	response := models.OpenAIResponse{
		ID:      requestID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   chatReq.Model,
		Choices: []models.Choice{
			{
				Index: 0,
				Message: models.Message{
					Role:    "assistant",
					Content: assistantText,
				},
				FinishReason: "stop",
			},
		},
		Usage: models.Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
		},
	}

	// Send response immediately (low latency)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	// -------- ASYNC METRICS (NON-BLOCKING) --------
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Println("metrics panic:", r)
			}
		}()

		repository.InsertUsage(
			apiKey.ID,
			requestID,
			chatReq.Model,
			promptTokens,
			completionTokens,
			totalTokens,
			totalCost,
		)
	}()
}
