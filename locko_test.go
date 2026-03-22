package locko_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	locko "github.com/barelyacompany/locko-go-sdk"
)

// sampleEntries is the fixture used across multiple tests.
var sampleEntries = []locko.ConfigEntry{
	{Key: "DATABASE_URL", Value: "postgres://localhost/mydb", Secret: false},
	{Key: "APP_ENV", Value: "production", Secret: false},
	{Key: "JWT_SECRET", Value: "supersecret", Secret: true},
	{Key: "API_TOKEN", Value: "tok_abc123", Secret: true},
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
func newClient(t *testing.T, srv *httptest.Server) *locko.Client {
	t.Helper()
	return locko.NewClient("test-key", &http.Client{Transport: &redirectTransport{target: srv.URL}})
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

func TestGetConfig_Success(t *testing.T) {
	srv := newMockServer(t, http.StatusOK, sampleEntries)
	defer srv.Close()

	cfg, err := newClient(t, srv).GetConfig(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg) != len(sampleEntries) {
		t.Fatalf("expected %d entries, got %d", len(sampleEntries), len(cfg))
	}
	if cfg["DATABASE_URL"] != "postgres://localhost/mydb" {
		t.Errorf("unexpected value for DATABASE_URL: %q", cfg["DATABASE_URL"])
	}
	if cfg["JWT_SECRET"] != "supersecret" {
		t.Errorf("unexpected value for JWT_SECRET: %q", cfg["JWT_SECRET"])
	}
}

func TestGetSecrets(t *testing.T) {
	srv := newMockServer(t, http.StatusOK, sampleEntries)
	defer srv.Close()

	secrets, err := newClient(t, srv).GetSecrets(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]string{"JWT_SECRET": "supersecret", "API_TOKEN": "tok_abc123"}
	if len(secrets) != len(expected) {
		t.Fatalf("expected %d secrets, got %d", len(expected), len(secrets))
	}
	for k, v := range expected {
		if secrets[k] != v {
			t.Errorf("secrets[%q] = %q, want %q", k, secrets[k], v)
		}
	}
	if _, ok := secrets["DATABASE_URL"]; ok {
		t.Error("DATABASE_URL should not appear in secrets")
	}
}

func TestGetVariables(t *testing.T) {
	srv := newMockServer(t, http.StatusOK, sampleEntries)
	defer srv.Close()

	vars, err := newClient(t, srv).GetVariables(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]string{"DATABASE_URL": "postgres://localhost/mydb", "APP_ENV": "production"}
	if len(vars) != len(expected) {
		t.Fatalf("expected %d variables, got %d", len(expected), len(vars))
	}
	for k, v := range expected {
		if vars[k] != v {
			t.Errorf("vars[%q] = %q, want %q", k, vars[k], v)
		}
	}
	if _, ok := vars["JWT_SECRET"]; ok {
		t.Error("JWT_SECRET should not appear in variables")
	}
}

func TestGetConfig_Unauthorized(t *testing.T) {
	srv := newMockServer(t, http.StatusUnauthorized, nil)
	defer srv.Close()

	_, err := newClient(t, srv).GetConfig(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, locko.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestGetConfig_ServerError(t *testing.T) {
	srv := newMockServer(t, http.StatusInternalServerError, nil)
	defer srv.Close()

	_, err := newClient(t, srv).GetConfig(context.Background())
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
	_, err := client.GetConfig(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotKey != wantKey {
		t.Errorf("X-API-Key header = %q, want %q", gotKey, wantKey)
	}
}
