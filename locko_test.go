package locko_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	locko "github.com/barelyacompany/locko"
)

// sampleEntries is the fixture used across multiple tests.
// Keys use a unique prefix to avoid collisions with real process env vars.
var sampleEntries = []locko.ConfigEntry{
	{Key: "LOCKO_TEST_DATABASE_URL", Value: "postgres://localhost/mydb", Secret: false},
	{Key: "LOCKO_TEST_APP_ENV", Value: "production", Secret: false},
	{Key: "LOCKO_TEST_JWT_SECRET", Value: "supersecret", Secret: true},
	{Key: "LOCKO_TEST_API_TOKEN", Value: "tok_abc123", Secret: true},
}

// redirectTransport rewrites every request's host to the given target,
// allowing tests to intercept the fixed production URL.
type redirectTransport struct {
	target string
}

func (rt *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	parsed, _ := url.Parse(rt.target)
	r2 := req.Clone(req.Context())
	r2.URL.Scheme = parsed.Scheme
	r2.URL.Host = parsed.Host
	return http.DefaultTransport.RoundTrip(r2)
}

// newClient returns a client whose HTTP traffic is redirected to srv.
// The client's background prefetch runs immediately against the mock server.
func newClient(t *testing.T, srv *httptest.Server) *locko.Client {
	t.Helper()
	return locko.NewClient("test-key", &http.Client{Transport: &redirectTransport{target: srv.URL}})
}

// newInitializedClient returns a client that has completed Initialize().
func newInitializedClient(t *testing.T, srv *httptest.Server) *locko.Client {
	t.Helper()
	c := newClient(t, srv)
	c.Initialize()
	return c
}

func newMockServer(t *testing.T, statusCode int, body any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if body != nil {
			if err := json.NewEncoder(w).Encode(body); err != nil {
				t.Errorf("mock server encode error: %v", err)
			}
		}
	}))
}

// cleanupTestEnv removes all prefixed test env vars set during a test.
func cleanupTestEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"LOCKO_TEST_DATABASE_URL",
		"LOCKO_TEST_APP_ENV",
		"LOCKO_TEST_JWT_SECRET",
		"LOCKO_TEST_API_TOKEN",
		"LOCKO_TEST_OVERLAP",
		"LOCKO_TEST_ONLY_ENV",
	}
	for _, k := range keys {
		os.Unsetenv(k)
	}
}

// ---------------------------------------------------------------------------
// GetConfigEntries (raw — still returns errors, still takes ctx)
// ---------------------------------------------------------------------------

func TestGetConfigEntries_Success(t *testing.T) {
	srv := newMockServer(t, http.StatusOK, sampleEntries)
	defer srv.Close()

	entries, err := newClient(t, srv).GetConfigEntries(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != len(sampleEntries) {
		t.Fatalf("expected %d entries, got %d", len(sampleEntries), len(entries))
	}
}

func TestGetConfigEntries_Unauthorized(t *testing.T) {
	srv := newMockServer(t, http.StatusUnauthorized, nil)
	defer srv.Close()

	_, err := newClient(t, srv).GetConfigEntries(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, locko.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestGetConfigEntries_ServerError(t *testing.T) {
	srv := newMockServer(t, http.StatusInternalServerError, nil)
	defer srv.Close()

	_, err := newClient(t, srv).GetConfigEntries(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var serverErr *locko.ErrServer
	if !errors.As(err, &serverErr) {
		t.Fatalf("expected *locko.ErrServer, got %T: %v", err, err)
	}
	if serverErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", serverErr.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Initialize — blocks until prefetch settles
// ---------------------------------------------------------------------------

func TestInitialize_IsIdempotent(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	c := newClient(t, srv)
	c.Initialize()
	c.Initialize() // second call must not trigger another fetch
	c.Initialize()

	if calls != 1 {
		t.Errorf("expected exactly 1 HTTP call, got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// GetConfig — synchronous after Initialize
// ---------------------------------------------------------------------------

func TestGetConfig_ReturnsLockoValues(t *testing.T) {
	defer cleanupTestEnv(t)
	srv := newMockServer(t, http.StatusOK, sampleEntries)
	defer srv.Close()

	cfg := newInitializedClient(t, srv).GetConfig(false)

	if cfg["LOCKO_TEST_DATABASE_URL"] != "postgres://localhost/mydb" {
		t.Errorf("unexpected value: %q", cfg["LOCKO_TEST_DATABASE_URL"])
	}
	if cfg["LOCKO_TEST_JWT_SECRET"] != "supersecret" {
		t.Errorf("unexpected value: %q", cfg["LOCKO_TEST_JWT_SECRET"])
	}
}

func TestGetConfig_ProcessEnvWinsOverLocko(t *testing.T) {
	defer cleanupTestEnv(t)
	os.Setenv("LOCKO_TEST_OVERLAP", "from-process")

	srv := newMockServer(t, http.StatusOK, []locko.ConfigEntry{
		{Key: "LOCKO_TEST_OVERLAP", Value: "from-locko", Secret: false},
	})
	defer srv.Close()

	cfg := newInitializedClient(t, srv).GetConfig(false)
	if cfg["LOCKO_TEST_OVERLAP"] != "from-process" {
		t.Errorf("expected process env to win, got %q", cfg["LOCKO_TEST_OVERLAP"])
	}
}

func TestGetConfig_LockoWinsWhenOverrideTrue(t *testing.T) {
	defer cleanupTestEnv(t)
	os.Setenv("LOCKO_TEST_OVERLAP", "from-process")

	srv := newMockServer(t, http.StatusOK, []locko.ConfigEntry{
		{Key: "LOCKO_TEST_OVERLAP", Value: "from-locko", Secret: false},
	})
	defer srv.Close()

	cfg := newInitializedClient(t, srv).GetConfig(true)
	if cfg["LOCKO_TEST_OVERLAP"] != "from-locko" {
		t.Errorf("expected Locko to win with override=true, got %q", cfg["LOCKO_TEST_OVERLAP"])
	}
}

func TestGetConfig_IncludesProcessEnvOnlyKeys(t *testing.T) {
	defer cleanupTestEnv(t)
	os.Setenv("LOCKO_TEST_ONLY_ENV", "env-only")

	srv := newMockServer(t, http.StatusOK, []locko.ConfigEntry{})
	defer srv.Close()

	cfg := newInitializedClient(t, srv).GetConfig(false)
	if cfg["LOCKO_TEST_ONLY_ENV"] != "env-only" {
		t.Errorf("expected env-only key in result, got %q", cfg["LOCKO_TEST_ONLY_ENV"])
	}
}

func TestGetConfig_FallsBackToEnvOnUnauthorized(t *testing.T) {
	defer cleanupTestEnv(t)
	os.Setenv("LOCKO_TEST_ONLY_ENV", "fallback-value")

	srv := newMockServer(t, http.StatusUnauthorized, nil)
	defer srv.Close()

	cfg := newInitializedClient(t, srv).GetConfig(false)
	if cfg["LOCKO_TEST_ONLY_ENV"] != "fallback-value" {
		t.Errorf("expected fallback env value, got %q", cfg["LOCKO_TEST_ONLY_ENV"])
	}
}

func TestGetConfig_FallsBackToEnvOnServerError(t *testing.T) {
	defer cleanupTestEnv(t)
	os.Setenv("LOCKO_TEST_ONLY_ENV", "fallback-value")

	srv := newMockServer(t, http.StatusInternalServerError, nil)
	defer srv.Close()

	cfg := newInitializedClient(t, srv).GetConfig(false)
	if cfg["LOCKO_TEST_ONLY_ENV"] != "fallback-value" {
		t.Errorf("expected fallback env value, got %q", cfg["LOCKO_TEST_ONLY_ENV"])
	}
}

// ---------------------------------------------------------------------------
// GetSecrets
// ---------------------------------------------------------------------------

func TestGetSecrets_ReturnsOnlySecrets(t *testing.T) {
	defer cleanupTestEnv(t)
	srv := newMockServer(t, http.StatusOK, sampleEntries)
	defer srv.Close()

	secrets := newInitializedClient(t, srv).GetSecrets(false)

	if _, ok := secrets["LOCKO_TEST_DATABASE_URL"]; ok {
		t.Error("LOCKO_TEST_DATABASE_URL should not appear in secrets")
	}
	if secrets["LOCKO_TEST_JWT_SECRET"] != "supersecret" {
		t.Errorf("unexpected value: %q", secrets["LOCKO_TEST_JWT_SECRET"])
	}
}

func TestGetSecrets_ProcessEnvWins(t *testing.T) {
	defer cleanupTestEnv(t)
	os.Setenv("LOCKO_TEST_JWT_SECRET", "from-process")

	srv := newMockServer(t, http.StatusOK, []locko.ConfigEntry{
		{Key: "LOCKO_TEST_JWT_SECRET", Value: "from-locko", Secret: true},
	})
	defer srv.Close()

	secrets := newInitializedClient(t, srv).GetSecrets(false)
	if secrets["LOCKO_TEST_JWT_SECRET"] != "from-process" {
		t.Errorf("expected process env to win, got %q", secrets["LOCKO_TEST_JWT_SECRET"])
	}
}

func TestGetSecrets_FallsBackToEnvOnAPIFailure(t *testing.T) {
	defer cleanupTestEnv(t)
	os.Setenv("LOCKO_TEST_ONLY_ENV", "fallback-secret")

	srv := newMockServer(t, http.StatusInternalServerError, nil)
	defer srv.Close()

	secrets := newInitializedClient(t, srv).GetSecrets(false)
	if secrets["LOCKO_TEST_ONLY_ENV"] != "fallback-secret" {
		t.Errorf("expected fallback env value, got %q", secrets["LOCKO_TEST_ONLY_ENV"])
	}
}

// ---------------------------------------------------------------------------
// GetVariables
// ---------------------------------------------------------------------------

func TestGetVariables_ReturnsOnlyVariables(t *testing.T) {
	defer cleanupTestEnv(t)
	srv := newMockServer(t, http.StatusOK, sampleEntries)
	defer srv.Close()

	vars := newInitializedClient(t, srv).GetVariables(false)

	if _, ok := vars["LOCKO_TEST_JWT_SECRET"]; ok {
		t.Error("LOCKO_TEST_JWT_SECRET should not appear in variables")
	}
	if vars["LOCKO_TEST_DATABASE_URL"] != "postgres://localhost/mydb" {
		t.Errorf("unexpected value: %q", vars["LOCKO_TEST_DATABASE_URL"])
	}
}

func TestGetVariables_FallsBackToEnvOnAPIFailure(t *testing.T) {
	defer cleanupTestEnv(t)
	os.Setenv("LOCKO_TEST_ONLY_ENV", "fallback-var")

	srv := newMockServer(t, http.StatusInternalServerError, nil)
	defer srv.Close()

	vars := newInitializedClient(t, srv).GetVariables(false)
	if vars["LOCKO_TEST_ONLY_ENV"] != "fallback-var" {
		t.Errorf("expected fallback env value, got %q", vars["LOCKO_TEST_ONLY_ENV"])
	}
}

// ---------------------------------------------------------------------------
// API key header
// ---------------------------------------------------------------------------

func TestGetConfig_SendsAPIKeyHeader(t *testing.T) {
	const wantKey = "my-api-key"
	var gotKey string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	client := locko.NewClient(wantKey, &http.Client{Transport: &redirectTransport{target: srv.URL}})
	client.Initialize()
	client.GetConfig(false)

	if gotKey != wantKey {
		t.Errorf("X-API-Key header = %q, want %q", gotKey, wantKey)
	}
}
