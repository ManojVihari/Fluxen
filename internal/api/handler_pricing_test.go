package api

import (
	"net/http"
	"testing"
)

func TestPricing_GetCatalog(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	signUpAndCreateApp(t, client, baseURL, "Document AI")

	resp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/pricing/catalog", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var out pricingCatalogResponse
	decodeJSON(t, resp, &out)
	if out.Version == "" {
		t.Error("expected a non-empty catalog version")
	}
	if len(out.Models) == 0 {
		t.Fatal("expected at least one model in the catalog")
	}
	foundGemini := false
	for _, m := range out.Models {
		if m.Provider == "gemini" {
			foundGemini = true
		}
	}
	if !foundGemini {
		t.Error("expected at least one gemini model in the catalog")
	}
}
