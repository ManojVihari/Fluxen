package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"fluxen/internal/crypto"
	"fluxen/internal/store"
)

// providerCredentialResponse mirrors store.ProviderCredential with the
// encrypted key field always omitted — Part G's security pass: "keys
// never returned by any API."
type providerCredentialResponse struct {
	ID       string  `json:"id"`
	Provider string  `json:"provider"`
	BaseURL  *string `json:"base_url"`
	Status   string  `json:"status"`

	CreatedAt string  `json:"created_at"`
	RevokedAt *string `json:"revoked_at,omitempty"`

	LastHealthCheckAt     *string `json:"last_health_check_at,omitempty"`
	LastHealthCheckStatus *string `json:"last_health_check_status,omitempty"`
	LastHealthCheckError  *string `json:"last_health_check_error,omitempty"`
}

func toProviderCredentialResponse(c store.ProviderCredential) providerCredentialResponse {
	out := providerCredentialResponse{
		ID: c.ID, Provider: c.Provider, BaseURL: c.BaseURL, Status: c.Status,
		CreatedAt:             c.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		LastHealthCheckStatus: c.LastHealthCheckStatus, LastHealthCheckError: c.LastHealthCheckError,
	}
	if c.RevokedAt != nil {
		s := c.RevokedAt.Format("2006-01-02T15:04:05Z07:00")
		out.RevokedAt = &s
	}
	if c.LastHealthCheckAt != nil {
		s := c.LastHealthCheckAt.Format("2006-01-02T15:04:05Z07:00")
		out.LastHealthCheckAt = &s
	}
	return out
}

// handleGetEncryptionStatus tells the dashboard whether Settings >
// Providers' credential storage is actually usable — i.e. whether
// FLUXEN_ENCRYPTION_KEY is set on this deployment. The onboarding flow
// and Settings > Providers both use this to decide whether to show the
// "generate a key" step instead of the credential form.
func (s *Server) handleGetEncryptionStatus(w http.ResponseWriter, r *http.Request) {
	configured := s.Credentials != nil && s.Credentials.Box != nil
	writeJSON(w, http.StatusOK, map[string]bool{"configured": configured})
}

// handleListProviderCredentials lists every credential (active and
// revoked) for the org — Settings > Providers (Part I.6).
func (s *Server) handleListProviderCredentials(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	if s.Credentials == nil {
		writeError(w, http.StatusServiceUnavailable, "provider credential storage is not configured")
		return
	}
	creds, err := s.Credentials.List(r.Context(), uc.OrgID)
	if err != nil {
		s.Logger.Error("api: failed to list provider credentials", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]providerCredentialResponse, 0, len(creds))
	for _, c := range creds {
		out = append(out, toProviderCredentialResponse(c))
	}
	writeJSON(w, http.StatusOK, out)
}

type createProviderCredentialRequest struct {
	Provider string `json:"provider"`
	APIKey   string `json:"api_key"`
	BaseURL  string `json:"base_url"`
}

var validProviders = map[string]bool{"openai": true, "gemini": true, "ollama": true}

// handleCreateProviderCredential creates (or replaces) the org's active
// credential for a provider. This is the real, encrypted-at-rest
// replacement for the OPENAI_API_KEY/GEMINI_API_KEY/OLLAMA_BASE_URL env
// vars earlier phases used (Part E.1).
func (s *Server) handleCreateProviderCredential(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	if s.Credentials == nil {
		writeError(w, http.StatusServiceUnavailable, "provider credential storage is not configured")
		return
	}

	var req createProviderCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validProviders[req.Provider] {
		writeError(w, http.StatusBadRequest, "provider must be one of openai, gemini, ollama")
		return
	}

	cred, err := s.Credentials.Create(r.Context(), uc.OrgID, req.Provider, req.APIKey, req.BaseURL)
	if err != nil {
		if errors.Is(err, crypto.ErrKeyNotConfigured) {
			writeError(w, http.StatusServiceUnavailable, "encryption is not configured for this deployment (FLUXEN_ENCRYPTION_KEY)")
			return
		}
		s.Logger.Error("api: failed to create provider credential", "error", err, "provider", req.Provider)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if s.CredentialSnapshot != nil {
		s.CredentialSnapshot.Invalidate(r.Context(), req.Provider)
	}

	writeJSON(w, http.StatusCreated, toProviderCredentialResponse(cred))
}

// handleRevokeProviderCredential revokes one credential.
func (s *Server) handleRevokeProviderCredential(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	if s.Credentials == nil {
		writeError(w, http.StatusServiceUnavailable, "provider credential storage is not configured")
		return
	}
	id := chi.URLParam(r, "credentialID")

	cred, err := s.Credentials.Revoke(r.Context(), uc.OrgID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "credential not found or already revoked")
			return
		}
		s.Logger.Error("api: failed to revoke provider credential", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if s.CredentialSnapshot != nil {
		s.CredentialSnapshot.Invalidate(r.Context(), cred.Provider)
	}

	writeJSON(w, http.StatusOK, toProviderCredentialResponse(cred))
}

// handleProviderHealthCheck resolves the credential and pings the
// provider's own Health() (Part I.6: "Providers ... health check").
func (s *Server) handleProviderHealthCheck(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	if s.Credentials == nil {
		writeError(w, http.StatusServiceUnavailable, "provider credential storage is not configured")
		return
	}
	id := chi.URLParam(r, "credentialID")

	cred, err := s.Credentials.Store.Get(r.Context(), uc.OrgID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "credential not found")
			return
		}
		s.Logger.Error("api: failed to look up provider credential", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	client, ok := s.ProviderClients[cred.Provider]
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "no client configured for this provider")
		return
	}

	checkErr := s.Credentials.HealthCheck(r.Context(), uc.OrgID, id, cred.Provider, client)
	status := "ok"
	var errMsg string
	if checkErr != nil {
		status = "error"
		errMsg = checkErr.Error()
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status, "error": errMsg})
}

// handleListProviderModels lists the models available for a provider —
// Gemini's static catalog, Ollama's live /api/tags, or OpenAI's
// passthrough /v1/models (Part D) — using the org's resolved credential.
func (s *Server) handleListProviderModels(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	provider := chi.URLParam(r, "provider")
	if !validProviders[provider] {
		writeError(w, http.StatusBadRequest, "unknown provider")
		return
	}
	if s.Credentials == nil {
		writeError(w, http.StatusServiceUnavailable, "provider credential storage is not configured")
		return
	}
	client, ok := s.ProviderClients[provider]
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "no client configured for this provider")
		return
	}

	cred, found, err := s.Credentials.Resolve(r.Context(), uc.OrgID, provider)
	if err != nil {
		s.Logger.Error("api: failed to resolve provider credential", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "no active credential for this provider")
		return
	}

	models, err := client.Models(r.Context(), cred)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to list models from the provider")
		return
	}
	writeJSON(w, http.StatusOK, models)
}
