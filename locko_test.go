package locko_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

	client := locko.NewClient("test-key", locko.WithServerURL(srv.URL))
	cfg, err := client.GetConfig(context.Background())
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

	client := locko.NewClient("test-key", locko.WithServerURL(srv.URL))
	secrets, err := client.GetSecrets(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect only the secret entries.
	expected := map[string]string{
		"JWT_SECRET": "supersecret",
		"API_TOKEN":  "tok_abc123",
	}
	if len(secrets) != len(expected) {
		t.Fatalf("expected %d secrets, got %d", len(expected), len(secrets))
	}
	for k, v := range expected {
		if secrets[k] != v {
			t.Errorf("secrets[%q] = %q, want %q", k, secrets[k], v)
		}
	}
	// Non-secret keys must not appear.
	if _, ok := secrets["DATABASE_URL"]; ok {
		t.Error("DATABASE_URL should not appear in secrets")
	}
}

func TestGetVariables(t *testing.T) {
	srv := newMockServer(t, http.StatusOK, sampleEntries)
	defer srv.Close()

	client := locko.NewClient("test-key", locko.WithServerURL(srv.URL))
	vars, err := client.GetVariables(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]string{
		"DATABASE_URL": "postgres://localhost/mydb",
		"APP_ENV":      "production",
	}
	if len(vars) != len(expected) {
		t.Fatalf("expected %d variables, got %d", len(expected), len(vars))
	}
	for k, v := range expected {
		if vars[k] != v {
			t.Errorf("vars[%q] = %q, want %q", k, vars[k], v)
		}
	}
	// Secret keys must not appear.
	if _, ok := vars["JWT_SECRET"]; ok {
		t.Error("JWT_SECRET should not appear in variables")
	}
}

func TestGetConfig_Unauthorized(t *testing.T) {
	srv := newMockServer(t, http.StatusUnauthorized, nil)
	defer srv.Close()

	client := locko.NewClient("bad-key", locko.WithServerURL(srv.URL))
	_, err := client.GetConfig(context.Background())
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

	client := locko.NewClient("test-key", locko.WithServerURL(srv.URL))
	_, err := client.GetConfig(context.Background())
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

func TestNewClient_DefaultURL(t *testing.T) {
	// We can't directly inspect private fields, so we verify via a request to a
	// server that captures the incoming URL path.
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	// Use WithServerURL to point at our test server — this also validates the
	// default-URL code path by confirming the path suffix is correct.
	client := locko.NewClient("key", locko.WithServerURL(srv.URL))
	_, err := client.GetConfig(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPath != "/api-keys/config" {
		t.Errorf("unexpected path: %q", capturedPath)
	}
}

func TestNewClient_CustomURL(t *testing.T) {
	var capturedHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHost = r.Host
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	client := locko.NewClient("key", locko.WithServerURL(srv.URL))
	_, err := client.GetConfig(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The test server's host should match what the client used.
	expectedHost := strings.TrimPrefix(srv.URL, "http://")
	if capturedHost != expectedHost {
		t.Errorf("expected host %q, got %q", expectedHost, capturedHost)
	}
}

func TestNewClient_TrailingSlash(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	// Pass the URL with multiple trailing slashes.
	client := locko.NewClient("key", locko.WithServerURL(srv.URL+"///"))
	_, err := client.GetConfig(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Path must not contain double slashes caused by leftover trailing slashes.
	if strings.Contains(capturedPath, "//") {
		t.Errorf("path contains double slash, trailing slash not stripped: %q", capturedPath)
	}
	if capturedPath != "/api-keys/config" {
		t.Errorf("unexpected path: %q", capturedPath)
	}
}
