package store

import (
	"context"
	"testing"
)

func TestProviderCredentials_UpsertRevokesPriorActive(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	creds := NewProviderCredentials(pool)

	first, err := creds.Upsert(context.Background(), orgID, "openai", []byte("cipher-1"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.Status != "active" {
		t.Fatalf("expected the first credential to be active, got %q", first.Status)
	}

	second, err := creds.Upsert(context.Background(), orgID, "openai", []byte("cipher-2"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := creds.Get(context.Background(), orgID, first.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != "revoked" {
		t.Errorf("expected the first credential to be revoked after a second Upsert, got %q", got.Status)
	}

	active, err := creds.GetActive(context.Background(), orgID, "openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if active.ID != second.ID {
		t.Errorf("expected the second credential to be active, got %q", active.ID)
	}
}

func TestProviderCredentials_ListAndRevoke(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	creds := NewProviderCredentials(pool)

	c, err := creds.Upsert(context.Background(), orgID, "gemini", []byte("cipher"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list, err := creds.ListByOrg(context.Background(), orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 || list[0].ID != c.ID {
		t.Fatalf("expected the credential in the list, got %+v", list)
	}

	revoked, err := creds.Revoke(context.Background(), orgID, c.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if revoked.Status != "revoked" {
		t.Errorf("expected status revoked, got %q", revoked.Status)
	}

	if _, err := creds.GetActive(context.Background(), orgID, "gemini"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound for an org with no active gemini credential, got %v", err)
	}

	// Revoking an already-revoked credential is a real error (WHERE
	// status='active' matches nothing), not a silent no-op.
	if _, err := creds.Revoke(context.Background(), orgID, c.ID); err == nil {
		t.Error("expected revoking an already-revoked credential to fail")
	}
}

func TestProviderCredentials_RecordHealthCheck(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	creds := NewProviderCredentials(pool)

	c, err := creds.Upsert(context.Background(), orgID, "ollama", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := creds.RecordHealthCheck(context.Background(), orgID, c.ID, "ok", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := creds.Get(context.Background(), orgID, c.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.LastHealthCheckStatus == nil || *got.LastHealthCheckStatus != "ok" {
		t.Errorf("expected last_health_check_status=ok, got %v", got.LastHealthCheckStatus)
	}
	if got.LastHealthCheckAt == nil {
		t.Error("expected last_health_check_at to be set")
	}
}
