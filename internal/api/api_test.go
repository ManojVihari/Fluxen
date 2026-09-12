package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	"fluxen/internal/auth"
	"fluxen/internal/policy"
	"fluxen/internal/store"
	"fluxen/pkg/pricing"
)

// newTestEnv stands up disposable Postgres + Redis containers, applies
// migrations, and returns a fully-wired API Server plus an httptest
// client to exercise it — the same combined-binary wiring cmd/fluxen uses
// in production. It also returns the pool so rollup-endpoint tests can
// seed request_rollup_daily/application_daily directly (internal/rollup's
// compute step has its own tests; these exercise the read-side API only).
func newTestEnv(t *testing.T) (*Server, *http.Client, string, *pgxpool.Pool) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping testcontainers-based api test in -short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	pgContainer, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("fluxen"),
		tcpostgres.WithUsername("fluxen"),
		tcpostgres.WithPassword("fluxen"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Skipf("skipping: could not start postgres testcontainer: %v", err)
	}
	t.Cleanup(func() { _ = pgContainer.Terminate(context.Background()) })

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get postgres connection string: %v", err)
	}

	sqlDB, err := store.OpenSQLDB(dsn)
	if err != nil {
		t.Fatalf("failed to open sql.DB: %v", err)
	}
	if err := store.MigrateUp(sqlDB); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	sqlDB.Close()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	redisContainer, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Skipf("skipping: could not start redis testcontainer: %v", err)
	}
	t.Cleanup(func() { _ = redisContainer.Terminate(context.Background()) })

	redisURL, err := redisContainer.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get redis connection string: %v", err)
	}
	redisOpts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("failed to parse redis URL: %v", err)
	}
	redisClient := redis.NewClient(redisOpts)
	t.Cleanup(func() { _ = redisClient.Close() })

	apps := store.NewApplications(pool)
	keys := store.NewAPIKeys(pool)
	opportunities := store.NewOpportunities(pool)
	simulations := store.NewSimulations(pool)
	policyStore := policy.NewStore(pool)
	catalog, err := pricing.LoadEmbedded()
	if err != nil {
		t.Fatalf("failed to load pricing catalog: %v", err)
	}
	srv := NewServer(Server{
		Orgs:          store.NewOrganizations(pool),
		Users:         store.NewUsers(pool),
		Apps:          apps,
		Keys:          keys,
		Requests:      store.NewRequests(pool),
		Rollups:       store.NewRollups(pool),
		Opportunities: opportunities,
		Simulations:   simulations,
		Policies:      policyStore,
		Applier: &policy.Applier{
			Policies: policyStore, Opportunities: opportunities, Simulations: simulations,
		},
		Catalog:  catalog,
		Sessions: auth.NewSessionStore(redisClient),
	})

	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	client := ts.Client()
	client.Jar = mustCookieJar(t)

	return srv, client, ts.URL, pool
}

func mustCookieJar(t *testing.T) http.CookieJar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("failed to create cookie jar: %v", err)
	}
	return jar
}

func doJSON(t *testing.T, client *http.Client, method, url string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("failed to encode request body: %v", err)
		}
	}
	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
}

func TestSetup_StatusFalseBeforeSetup(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)

	resp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/setup", nil)
	defer resp.Body.Close()

	var status setupStatusResponse
	decodeJSON(t, resp, &status)
	if status.Complete {
		t.Error("expected setup status to be incomplete before any org exists")
	}
}

func TestFullJourney_SetupLoginCreateAppIssueKeyRevoke(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)

	// 1. Setup
	resp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/setup", setupRequest{
		OrgName: "Acme", Email: "owner@example.com", Password: "supersecret123",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 from setup, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 2. Setup is now marked complete, and a second setup call is
	// rejected.
	statusResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/setup", nil)
	var status setupStatusResponse
	decodeJSON(t, statusResp, &status)
	if !status.Complete {
		t.Fatal("expected setup status to be complete after setup")
	}

	dupeResp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/setup", setupRequest{
		OrgName: "Acme2", Email: "other@example.com", Password: "supersecret123",
	})
	if dupeResp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for a second setup call, got %d", dupeResp.StatusCode)
	}
	dupeResp.Body.Close()

	// 3. The setup response already logged us in (session cookie set) —
	// confirm /auth/session reflects it.
	sessionResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/auth/session", nil)
	if sessionResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /auth/session after setup, got %d", sessionResp.StatusCode)
	}
	var session sessionResponse
	decodeJSON(t, sessionResp, &session)
	if session.Email != "owner@example.com" {
		t.Errorf("expected session email owner@example.com, got %q", session.Email)
	}

	// 4. Create an application.
	appResp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/applications", createApplicationRequest{Name: "Document AI"})
	if appResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating an application, got %d", appResp.StatusCode)
	}
	var app applicationResponse
	decodeJSON(t, appResp, &app)
	if app.Slug != "document-ai" {
		t.Errorf("expected slug 'document-ai', got %q", app.Slug)
	}

	// 5. List applications includes it.
	listResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/applications", nil)
	var list []applicationResponse
	decodeJSON(t, listResp, &list)
	if len(list) != 1 || list[0].ID != app.ID {
		t.Fatalf("expected the created application in the list, got %+v", list)
	}

	// 6. Issue an API key for it.
	keyResp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/applications/"+app.ID+"/keys", createKeyRequest{Name: "default"})
	if keyResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 issuing a key, got %d", keyResp.StatusCode)
	}
	var key createKeyResponse
	decodeJSON(t, keyResp, &key)
	if key.Key == "" || key.Key[:8] != auth.KeyPrefix {
		t.Fatalf("expected a raw key starting with %q, got %q", auth.KeyPrefix, key.Key)
	}

	// 7. Revoke it.
	revokeResp := doJSON(t, client, http.MethodDelete, baseURL+"/api/v1/keys/"+key.ID, nil)
	if revokeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 revoking a key, got %d", revokeResp.StatusCode)
	}
	revokeResp.Body.Close()

	// 8. Logout clears the session.
	logoutResp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/auth/logout", nil)
	logoutResp.Body.Close()

	postLogoutResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/auth/session", nil)
	if postLogoutResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 from /auth/session after logout, got %d", postLogoutResp.StatusCode)
	}
	postLogoutResp.Body.Close()
}

func TestLogin_WrongPasswordRejected(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)

	setupResp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/setup", setupRequest{
		OrgName: "Acme", Email: "owner@example.com", Password: "supersecret123",
	})
	setupResp.Body.Close()
	doJSON(t, client, http.MethodPost, baseURL+"/api/v1/auth/logout", nil).Body.Close()

	resp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/auth/login", loginRequest{
		Email: "owner@example.com", Password: "wrong-password",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong password, got %d", resp.StatusCode)
	}
}

func TestApplications_RequireAuth(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)

	resp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/applications", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a session, got %d", resp.StatusCode)
	}
}

// setUpAndCreateApp is the common prefix for the rollup-endpoint tests:
// setup (which also logs in) plus one application.
func setUpAndCreateApp(t *testing.T, client *http.Client, baseURL string) applicationResponse {
	t.Helper()
	doJSON(t, client, http.MethodPost, baseURL+"/api/v1/setup", setupRequest{
		OrgName: "Acme", Email: "owner@example.com", Password: "supersecret123",
	}).Body.Close()

	appResp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/applications", createApplicationRequest{Name: "Document AI"})
	var app applicationResponse
	decodeJSON(t, appResp, &app)
	return app
}

func TestApplicationSummary_ReflectsSeededRollups(t *testing.T) {
	_, client, baseURL, pool := newTestEnv(t)
	app := setUpAndCreateApp(t, client, baseURL)

	today := time.Now().UTC().Truncate(24 * time.Hour)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO application_daily (app_id, day, requests, errors, input_tokens, output_tokens, total_tokens, cost_micro, duration_ms_sum)
		VALUES ($1, $2, 100, 5, 50000, 20000, 70000, 12000, 45000)
	`, app.ID, today)
	if err != nil {
		t.Fatalf("unexpected error seeding application_daily: %v", err)
	}

	resp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/applications/"+app.ID+"/summary", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var summary summaryResponse
	decodeJSON(t, resp, &summary)

	if summary.Requests != 100 || summary.Errors != 5 || summary.CostMicro != 12000 {
		t.Errorf("expected the summary to reflect seeded rollups, got %+v", summary)
	}
	if summary.AvgDurationMS != 450 {
		t.Errorf("expected avg_duration_ms=450 (45000/100), got %v", summary.AvgDurationMS)
	}
}

func TestApplicationSummary_UnknownApplicationIs404(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	doJSON(t, client, http.MethodPost, baseURL+"/api/v1/setup", setupRequest{
		OrgName: "Acme", Email: "owner@example.com", Password: "supersecret123",
	}).Body.Close()

	resp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/applications/00000000-0000-0000-0000-000000000000/summary", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown application, got %d", resp.StatusCode)
	}
}

func TestApplicationTimeseries_ReturnsChronologicalPoints(t *testing.T) {
	_, client, baseURL, pool := newTestEnv(t)
	app := setUpAndCreateApp(t, client, baseURL)

	today := time.Now().UTC().Truncate(24 * time.Hour)
	yesterday := today.AddDate(0, 0, -1)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO application_daily (app_id, day, requests, errors, input_tokens, output_tokens, total_tokens, cost_micro, duration_ms_sum)
		VALUES ($1, $2, 10, 0, 1000, 500, 1500, 100, 2000), ($1, $3, 20, 1, 2000, 1000, 3000, 200, 4000)
	`, app.ID, yesterday, today)
	if err != nil {
		t.Fatalf("unexpected error seeding application_daily: %v", err)
	}

	resp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/applications/"+app.ID+"/timeseries?range=7d", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var points []dailyPointResponse
	decodeJSON(t, resp, &points)

	if len(points) != 2 {
		t.Fatalf("expected 2 daily points, got %d: %+v", len(points), points)
	}
	if points[0].Requests != 10 || points[1].Requests != 20 {
		t.Errorf("expected points in chronological order, got %+v", points)
	}
}

func TestApplicationModels_SortedByCostDescending(t *testing.T) {
	_, client, baseURL, pool := newTestEnv(t)
	app := setUpAndCreateApp(t, client, baseURL)

	today := time.Now().UTC().Truncate(24 * time.Hour)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_rollup_daily (org_id, app_id, day, provider, model, status, requests, input_tokens, output_tokens, total_tokens, cost_micro, duration_ms_sum)
		VALUES
			($1, $2, $3, 'openai', 'gpt-4o-mini', 'ok', 80, 8000, 4000, 12000, 40, 16000),
			($1, $2, $3, 'openai', 'gpt-4o', 'ok', 20, 6000, 3000, 9000, 400, 6000)
	`, "99999999-9999-9999-9999-999999999999", app.ID, today)
	if err != nil {
		t.Fatalf("unexpected error seeding request_rollup_daily: %v", err)
	}

	resp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/applications/"+app.ID+"/models", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var breakdown []modelBreakdownResponse
	decodeJSON(t, resp, &breakdown)

	if len(breakdown) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(breakdown), breakdown)
	}
	if breakdown[0].Model != "gpt-4o" {
		t.Errorf("expected gpt-4o (higher cost) first, got %+v", breakdown[0])
	}
	if breakdown[1].Model != "gpt-4o-mini" || breakdown[1].Requests != 80 {
		t.Errorf("expected gpt-4o-mini second with 80 requests, got %+v", breakdown[1])
	}
}
