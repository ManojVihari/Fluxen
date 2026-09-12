package api

import (
	"net/http"
	"testing"
)

func TestSettings_GetDefaultsAndPatch(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	signUpAndCreateApp(t, client, baseURL, "Document AI")

	getResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/settings", nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", getResp.StatusCode)
	}
	var settings settingsResponse
	decodeJSON(t, getResp, &settings)
	if settings.RequestsRetentionDays != 90 || settings.BodyRetentionDays != 7 || settings.BodyCaptureEnabled {
		t.Errorf("expected defaults 90/7/false, got %+v", settings)
	}

	patchResp := doJSON(t, client, http.MethodPatch, baseURL+"/api/v1/settings", settingsResponse{
		RequestsRetentionDays: 30, BodyRetentionDays: 3, BodyCaptureEnabled: true,
	})
	if patchResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", patchResp.StatusCode)
	}

	getResp2 := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/settings", nil)
	var updated settingsResponse
	decodeJSON(t, getResp2, &updated)
	if updated.RequestsRetentionDays != 30 || updated.BodyRetentionDays != 3 || !updated.BodyCaptureEnabled {
		t.Errorf("expected updated 30/3/true, got %+v", updated)
	}
}

func TestSettings_PatchRejectsNonPositiveRetention(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	signUpAndCreateApp(t, client, baseURL, "Document AI")

	resp := doJSON(t, client, http.MethodPatch, baseURL+"/api/v1/settings", settingsResponse{
		RequestsRetentionDays: 0, BodyRetentionDays: 7,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a zero retention window, got %d", resp.StatusCode)
	}
}
