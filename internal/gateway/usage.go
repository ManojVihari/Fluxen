package gateway

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ManojVihari/Fluxen/internal/auth"
	"github.com/ManojVihari/Fluxen/internal/policy"
	"github.com/ManojVihari/Fluxen/internal/repository"
)

func UsageHandler(w http.ResponseWriter, r *http.Request) {

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

	// Hash key
	keyHash := auth.HashAPIKey(apiKeyRaw)

	apiKey, err := repository.GetAPIKey(keyHash)
	if err != nil {
		http.Error(w, "Invalid API Key", http.StatusUnauthorized)
		return
	}

	// -------- ROLE CHECK --------
	rolePolicy, err := repository.GetRolePolicy(apiKey.RoleName)
	if err != nil {
		http.Error(w, "Failed to load role policy", http.StatusInternalServerError)
		return
	}

	if err := policy.CheckUsagePermission(*rolePolicy); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	// -------- FETCH USAGE --------
	summary, err := repository.GetUsageSummary(apiKey.ID)
	if err != nil {
		http.Error(w, "Failed to fetch usage", http.StatusInternalServerError)
		return
	}

	// -------- RESPONSE --------
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}