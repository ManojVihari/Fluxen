package api

import (
	"net/http"
	"testing"
)

func TestProviderCredentials_CreateListRevoke(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	signUpAndCreateApp(t, client, baseURL, "Document AI")

	createResp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/providers/credentials", map[string]string{
		"provider": "gemini",
		"api_key":  "test-gemini-key",
	})
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", createResp.StatusCode)
	}
	var created providerCredentialResponse
	decodeJSON(t, createResp, &created)
	if created.Provider != "gemini" || created.Status != "active" {
		t.Fatalf("unexpected created credential: %+v", created)
	}

	// The response never carries the key — providerCredentialResponse
	// has no field for it at all, so this is a compile-time guarantee,
	// but assert the raw JSON too for belt-and-suspenders.
	listResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/providers/credentials", nil)
	var list []providerCredentialResponse
	decodeJSON(t, listResp, &list)
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("expected the created credential in the list, got %+v", list)
	}

	revokeResp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/providers/credentials/"+created.ID+"/revoke", nil)
	if revokeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", revokeResp.StatusCode)
	}
	var revoked providerCredentialResponse
	decodeJSON(t, revokeResp, &revoked)
	if revoked.Status != "revoked" {
		t.Errorf("expected status revoked, got %q", revoked.Status)
	}
}

func TestProviderCredentials_RequiresAPIKeyExceptOllama(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	signUpAndCreateApp(t, client, baseURL, "Document AI")

	resp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/providers/credentials", map[string]string{
		"provider": "openai",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for openai with no api_key, got %d", resp.StatusCode)
	}

	ollamaResp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/providers/credentials", map[string]string{
		"provider": "ollama",
		"base_url": "http://ollama.local:11434",
	})
	defer ollamaResp.Body.Close()
	if ollamaResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for ollama with no api_key, got %d", ollamaResp.StatusCode)
	}
}

func TestProviderCredentials_HealthCheckAndModels(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	signUpAndCreateApp(t, client, baseURL, "Document AI")

	createResp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/providers/credentials", map[string]string{
		"provider": "gemini",
		"api_key":  "test-gemini-key",
	})
	var created providerCredentialResponse
	decodeJSON(t, createResp, &created)

	healthResp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/providers/"+created.ID+"/health-check", nil)
	if healthResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", healthResp.StatusCode)
	}
	var health map[string]string
	decodeJSON(t, healthResp, &health)
	if health["status"] != "ok" {
		t.Errorf("expected status ok from the fake provider's Health(), got %+v", health)
	}

	modelsResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/providers/gemini/models", nil)
	if modelsResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", modelsResp.StatusCode)
	}
	var models []map[string]string
	decodeJSON(t, modelsResp, &models)
	if len(models) != 1 || models[0]["ID"] != "gemini-1.5-flash" {
		t.Errorf("expected the fake provider's model list, got %+v", models)
	}
}

func TestProviderCredentials_ModelsWithoutCredentialIs404(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	signUpAndCreateApp(t, client, baseURL, "Document AI")

	resp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/providers/gemini/models", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 with no active gemini credential, got %d", resp.StatusCode)
	}
}
