// Package credentials is the business-logic layer over
// store.ProviderCredentials: encrypt-on-write, decrypt-only-for-internal-
// use (Part E.1's "keys never returned by any API" — Service.List always
// strips the encrypted key; only Resolve, called from the gateway's own
// process, ever decrypts one). It replaces the env-var-based provider
// credentials (OPENAI_API_KEY, GEMINI_API_KEY, OLLAMA_BASE_URL) Phase
// 1-6 used as a documented simplification.
package credentials

import (
	"context"
	"errors"
	"fmt"

	"fluxen/internal/crypto"
	"fluxen/internal/store"
	"fluxen/pkg/providers"
	"fluxen/pkg/types"
)

// ErrKeyRequired is returned by Create for openai/gemini without an API
// key — only Ollama may have none (a stock local install has no auth at
// all, mirroring the gateway's own credentialConfigured rule).
var ErrKeyRequired = errors.New("credentials: an API key is required for this provider")

// Service is the read/write path every credentials-aware caller (the
// control API, the gateway's Snapshot) goes through.
type Service struct {
	Store *store.ProviderCredentials
	Box   *crypto.Box // nil means encryption is not configured — Create fails clearly rather than storing plaintext
}

func NewService(s *store.ProviderCredentials, box *crypto.Box) *Service {
	return &Service{Store: s, Box: box}
}

// Create encrypts and stores a new active credential for (org,
// provider), revoking whatever was previously active for that provider
// (store.ProviderCredentials.Upsert's own guarantee).
func (s *Service) Create(ctx context.Context, orgID types.OrgID, provider, apiKey, baseURL string) (store.ProviderCredential, error) {
	if apiKey == "" && provider != "ollama" {
		return store.ProviderCredential{}, ErrKeyRequired
	}

	var encrypted []byte
	if apiKey != "" {
		if s.Box == nil {
			return store.ProviderCredential{}, crypto.ErrKeyNotConfigured
		}
		sealed, err := s.Box.Seal([]byte(apiKey))
		if err != nil {
			return store.ProviderCredential{}, fmt.Errorf("credentials: failed to encrypt api key: %w", err)
		}
		encrypted = sealed
	}

	var baseURLPtr *string
	if baseURL != "" {
		baseURLPtr = &baseURL
	}

	return s.Store.Upsert(ctx, orgID, provider, encrypted, baseURLPtr)
}

// List returns every credential for an org with its encrypted key
// stripped — callers (the API layer) render only provider/status/
// base_url/health-check fields, never key material (Part G's security
// pass).
func (s *Service) List(ctx context.Context, orgID types.OrgID) ([]store.ProviderCredential, error) {
	creds, err := s.Store.ListByOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	for i := range creds {
		creds[i].APIKeyEncrypted = nil
	}
	return creds, nil
}

func (s *Service) Revoke(ctx context.Context, orgID types.OrgID, id string) (store.ProviderCredential, error) {
	c, err := s.Store.Revoke(ctx, orgID, id)
	c.APIKeyEncrypted = nil
	return c, err
}

// Resolve decrypts and returns the active credential for (org,
// provider) as a providers.Credential — the only path that ever
// decrypts a stored key, and only ever called from within the gateway/
// health-check process itself, never serialized back into an API
// response.
func (s *Service) Resolve(ctx context.Context, orgID types.OrgID, provider string) (providers.Credential, bool, error) {
	c, err := s.Store.GetActive(ctx, orgID, provider)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return providers.Credential{}, false, nil
		}
		return providers.Credential{}, false, err
	}

	cred := providers.Credential{}
	if c.BaseURL != nil {
		cred.BaseURL = *c.BaseURL
	}
	if len(c.APIKeyEncrypted) > 0 {
		if s.Box == nil {
			return providers.Credential{}, false, crypto.ErrKeyNotConfigured
		}
		plain, err := s.Box.Open(c.APIKeyEncrypted)
		if err != nil {
			return providers.Credential{}, false, fmt.Errorf("credentials: failed to decrypt api key: %w", err)
		}
		cred.APIKey = string(plain)
	}
	return cred, true, nil
}

// HealthCheck resolves the org's active credential for provider and
// calls the given client's Health method, recording the outcome
// (Part I.6: "Providers ... health check").
func (s *Service) HealthCheck(ctx context.Context, orgID types.OrgID, id, provider string, client providers.Provider) error {
	cred, ok, err := s.Resolve(ctx, orgID, provider)
	if err != nil {
		errMsg := err.Error()
		_ = s.Store.RecordHealthCheck(ctx, orgID, id, "error", &errMsg)
		return err
	}
	if !ok {
		errMsg := "no active credential"
		_ = s.Store.RecordHealthCheck(ctx, orgID, id, "error", &errMsg)
		return errors.New(errMsg)
	}

	if healthErr := client.Health(ctx, cred); healthErr != nil {
		errMsg := healthErr.Error()
		_ = s.Store.RecordHealthCheck(ctx, orgID, id, "error", &errMsg)
		return healthErr
	}

	return s.Store.RecordHealthCheck(ctx, orgID, id, "ok", nil)
}
