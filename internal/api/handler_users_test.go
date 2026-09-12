package api

import (
	"net/http"
	"testing"
)

func TestUsers_ListAndInvite(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	signUpAndCreateApp(t, client, baseURL, "Document AI")

	listResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/users", nil)
	var list []userResponse
	decodeJSON(t, listResp, &list)
	if len(list) != 1 || list[0].Email != "owner@example.com" || list[0].Role != "owner" {
		t.Fatalf("expected the setup owner in the list, got %+v", list)
	}

	inviteResp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/users", inviteUserRequest{
		Email: "member@example.com", Role: "member",
	})
	if inviteResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", inviteResp.StatusCode)
	}
	var invited inviteUserResponse
	decodeJSON(t, inviteResp, &invited)
	if invited.Email != "member@example.com" || invited.Role != "member" {
		t.Fatalf("unexpected invited user: %+v", invited)
	}
	if invited.TemporaryPassword == "" {
		t.Error("expected a temporary password in the invite response")
	}

	// The new member can actually log in with the generated password.
	loginResp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/auth/login", map[string]string{
		"email": "member@example.com", "password": invited.TemporaryPassword,
	})
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("expected the invited user to log in with the temporary password, got %d", loginResp.StatusCode)
	}
	loginResp.Body.Close()

	listResp2 := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/users", nil)
	var list2 []userResponse
	decodeJSON(t, listResp2, &list2)
	if len(list2) != 2 {
		t.Errorf("expected 2 users after the invite, got %d", len(list2))
	}
}

func TestUsers_InviteRejectsDuplicateEmail(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	signUpAndCreateApp(t, client, baseURL, "Document AI")

	resp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/users", inviteUserRequest{
		Email: "owner@example.com", Role: "member",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for a duplicate email, got %d", resp.StatusCode)
	}
}
