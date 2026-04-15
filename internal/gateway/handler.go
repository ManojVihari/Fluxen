package gateway

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ManojVihari/Fluxen/internal/analyzer"
	"github.com/ManojVihari/Fluxen/internal/auth"
	"github.com/ManojVihari/Fluxen/internal/cache"
	"github.com/ManojVihari/Fluxen/internal/models"
	"github.com/ManojVihari/Fluxen/internal/policy"
	"github.com/ManojVihari/Fluxen/internal/provider"
	"github.com/ManojVihari/Fluxen/internal/repository"
	"github.com/google/uuid"
)

func ChatHandler(w http.ResponseWriter, r *http.Request) {
	requestID := uuid.New().String()
	log.Printf("[%s] 🔍 REQUEST STARTED", requestID)

	// -------- AUTH --------
	log.Printf("[%s] 🔐 Currently authenticating...", requestID)
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		log.Printf("[%s] ❌ Missing Authorization header", requestID)
		http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
		return
	}

	apiKeyRaw := strings.TrimPrefix(authHeader, "Bearer ")
	if apiKeyRaw == "" {
		log.Printf("[%s] ❌ Invalid Authorization format", requestID)
		http.Error(w, "Invalid Authorization format", http.StatusUnauthorized)
		return
	}

	log.Printf("[%s] 🔑 Hashing API key...", requestID)
	// Hash API key
	keyHash := auth.HashAPIKey(apiKeyRaw)

	log.Printf("[%s] 📋 Checking API key in database...", requestID)
	// Fetch API key from DB
	apiKey, err := repository.GetAPIKey(keyHash)
	if err != nil {
		log.Printf("[%s] ❌ API key validation failed: %v", requestID, err)
		http.Error(w, "Invalid API Key", http.StatusUnauthorized)
		return
	}
	log.Printf("[%s] ✅ API key validated | Role: %s | Provider: %s | ID: %d", requestID, apiKey.RoleName, apiKey.Provider, apiKey.ID)

	// -------- BUDGET CHECK --------
	log.Printf("[%s] 💰 Checking budget...", requestID)
	currentUsage, err := repository.GetMonthlyUsage(apiKey.ID)
	if err != nil {
		log.Printf("[%s] ❌ Failed to fetch usage: %v", requestID, err)
		http.Error(w, "Failed to fetch usage", http.StatusInternalServerError)
		return
	}

	if currentUsage >= apiKey.Budget {
		log.Printf("[%s] ❌ Monthly budget exceeded | Current: $%.2f | Budget: $%.2f", requestID, currentUsage, apiKey.Budget)
		http.Error(w, "Monthly budget exceeded", http.StatusForbidden)
		return
	}
	log.Printf("[%s] ✅ Budget check passed | Current: $%.2f / $%.2f", requestID, currentUsage, apiKey.Budget)

	// -------- LOAD ROLE POLICY --------
	log.Printf("[%s] 👥 Loading role policy for role: %s", requestID, apiKey.RoleName)
	rolePolicy, err := repository.GetRolePolicy(apiKey.RoleName)
	if err != nil {
		log.Printf("[%s] ❌ Failed to load role policy: %v", requestID, err)
		http.Error(w, "Failed to load role policy", http.StatusInternalServerError)
		return
	}
	log.Printf("[%s] ✅ Role policy loaded", requestID)

	log.Printf("[%s] 🔍 Checking chat permission...", requestID)
	if err := policy.CheckChatPermission(*rolePolicy); err != nil {
		log.Printf("[%s] ❌ Chat permission denied: %v", requestID, err)
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	log.Printf("[%s] ✅ Chat permission granted", requestID)

	// -------- RATE LIMIT --------
	log.Printf("[%s] 🚦 Checking rate limit | Limit: %d/min", requestID, rolePolicy.RateLimit)
	if err := policy.CheckRateLimit(r.Context(), apiKey.ID, rolePolicy.RateLimit); err != nil {
		log.Printf("[%s] ❌ Rate limit exceeded: %v", requestID, err)
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	}
	log.Printf("[%s] ✅ Rate limit check passed", requestID)

	// -------- PARSE REQUEST --------
	log.Printf("[%s] 📝 Parsing request...", requestID)
	var chatReq models.ChatRequest

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("[%s] ❌ Failed to read request body: %v", requestID, err)
		http.Error(w, "Failed to read request", http.StatusInternalServerError)
		return
	}

	err = json.Unmarshal(body, &chatReq)
	if err != nil {
		log.Printf("[%s] ❌ Invalid JSON in request: %v", requestID, err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	log.Printf("[%s] ✅ Request parsed | Messages: %d", requestID, len(chatReq.Messages))

	// -------- BUILD PROMPT --------
	prompt := ""
	for _, msg := range chatReq.Messages {
		prompt += msg.Role + ": " + msg.Content + "\n"
	}

	// -------- SEMANTIC CACHE LOOKUP --------
	log.Printf("[%s] 🔎 Checking semantic cache for model: %s", requestID, chatReq.Model)
	if cached, hit := cache.Lookup(r.Context(), chatReq.Model, chatReq.Messages); hit {
		cached.ID = uuid.New().String()

		// Estimate how much this cache hit saved
		savedTokens := cached.Usage.TotalTokens
		savedCost := analyzer.EstimateCost(chatReq.Model, cached.Usage.PromptTokens, cached.Usage.CompletionTokens)
		cache.RecordHit(r.Context(), savedCost, savedTokens)

		log.Printf("[%s] ✅ CACHE HIT! Saved %.2f tokens and %.4f cost | Model: %s", requestID, float64(savedTokens), savedCost, chatReq.Model)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		json.NewEncoder(w).Encode(cached)
		return
	}
	log.Printf("[%s] 📊 Cache miss - will call provider", requestID)

	// -------- MODEL ACCESS CHECK --------
	log.Printf("[%s] 🤖 Checking model access for: %s", requestID, chatReq.Model)
	if err := policy.CheckModelAccess(*rolePolicy, chatReq.Model); err != nil {
		log.Printf("[%s] ❌ Model access denied: %v", requestID, err)
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	log.Printf("[%s] ✅ Model access granted", requestID)

	// -------- ANALYZE --------
	log.Printf("[%s] 📊 Analyzing token count...", requestID)
	prompttokens := analyzer.EstimateTokens(prompt)
	log.Printf("[%s] 📈 Estimated prompt tokens: %d", requestID, prompttokens)

	// -------- TOKEN LIMIT CHECK --------
	log.Printf("[%s] 🔢 Checking token limit | Limit: %d | Current: %d", requestID, rolePolicy.MaxTokensPerReq, prompttokens)
	if err := policy.CheckTokenLimit(*rolePolicy, prompttokens); err != nil {
		log.Printf("[%s] ❌ Token limit exceeded: %v", requestID, err)
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	log.Printf("[%s] ✅ Token limit check passed", requestID)

	log.Printf("[%s] 📋 Summary | Model: %s | Role: %s | Prompt Tokens: %d",
		requestID,
		chatReq.Model,
		apiKey.RoleName,
		prompttokens,
	)

	// -------- CALL PROVIDER --------
	log.Printf("[%s] 🌐 Calling provider: %s", requestID, apiKey.Provider)
	providerName := apiKey.Provider

	assistantText, err := provider.Generate(
		providerName,
		apiKey.ProviderAPIKey,
		chatReq.Model,
		prompt,
	)
	if err != nil || assistantText == "" {
		if err != nil {
			log.Printf("[%s] ❌ Provider error: %v", requestID, err)
		} else {
			log.Printf("[%s] ❌ Provider returned empty response", requestID)
		}
		http.Error(w, "Provider error or empty response", http.StatusInternalServerError)
		return
	}
	log.Printf("[%s] ✅ Response received from provider", requestID)

	// -------- RESPONSE ANALYSIS --------
	log.Printf("[%s] 📊 Analyzing response...", requestID)
	completionTokens := analyzer.EstimateTokens(assistantText)
	totalTokens := prompttokens + completionTokens
	totalCost := analyzer.EstimateCost(chatReq.Model, prompttokens, completionTokens)
	log.Printf("[%s] 📈 Response analysis | Completion Tokens: %d | Total Tokens: %d | Cost: $%.4f", requestID, completionTokens, totalTokens, totalCost)

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
			PromptTokens:     prompttokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
		},
	}

	// Record cache miss
	cache.RecordMiss(r.Context())

	// Send response immediately (low latency)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "MISS")
	json.NewEncoder(w).Encode(response)

	// -------- ASYNC METRICS + CACHE STORE (NON-BLOCKING) --------
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[%s] ❌ Metrics panic: %v", requestID, r)
			}
		}()

		log.Printf("[%s] 💾 Storing response in cache...", requestID)
		cache.Store(context.Background(), chatReq.Model, chatReq.Messages, response)
		log.Printf("[%s] ✅ Response cached", requestID)

		log.Printf("[%s] 📝 Recording usage metrics...", requestID)
		repository.InsertUsage(
			apiKey.ID,
			requestID,
			chatReq.Model,
			prompttokens,
			completionTokens,
			totalTokens,
			totalCost,
		)
		log.Printf("[%s] ✅ Usage recorded | API Key ID: %d | Cost: $%.4f", requestID, apiKey.ID, totalCost)
		log.Printf("[%s] ✅ REQUEST COMPLETED", requestID)
	}()

	log.Printf("[%s] ✅ Response sent to client | Cache: MISS", requestID)
}
