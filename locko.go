// Package locko provides a Go client for the Locko secrets and config management API.
package locko

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
)

const apiURL = "https://api-locko.barelyacompany.com/api/api-keys/config"

// ConfigEntry represents a single configuration or secret entry returned by the Locko API.
type ConfigEntry struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

// ErrUnauthorized is returned when the API responds with HTTP 401.
var ErrUnauthorized = errors.New("locko: unauthorized — check your API key")

// ErrNotFound is returned when the API responds with HTTP 404.
var ErrNotFound = errors.New("locko: resource not found")

// ErrServer represents an unexpected server-side error and carries the HTTP status code.
type ErrServer struct {
	StatusCode int
}

func (e *ErrServer) Error() string {
	return fmt.Sprintf("locko: server error (status %d)", e.StatusCode)
}

// Client is the Locko API client. Create one with NewClient.
type Client struct {
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new Locko Client with the given API key.
// Optionally provide a custom *http.Client (e.g. to set timeouts).
func NewClient(apiKey string, httpClient ...*http.Client) *Client {
	c := &Client{apiKey: apiKey, httpClient: &http.Client{}}
	if len(httpClient) > 0 && httpClient[0] != nil {
		c.httpClient = httpClient[0]
	}
	return c
}

// GetConfigEntries fetches all configuration entries (both secrets and plain variables)
// from the Locko API and returns them as a slice of ConfigEntry.
func (c *Client) GetConfigEntries(ctx context.Context) ([]ConfigEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("locko: failed to build request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("locko: request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// handled below
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case http.StatusNotFound:
		return nil, ErrNotFound
	default:
		return nil, &ErrServer{StatusCode: resp.StatusCode}
	}

	var entries []ConfigEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("locko: failed to decode response: %w", err)
	}
	return entries, nil
}

// GetConfig fetches all configuration entries and returns them as a flat key→value map,
// including both secrets and plain variables.
func (c *Client) GetConfig(ctx context.Context) (map[string]string, error) {
	entries, err := c.GetConfigEntries(ctx)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(entries))
	for _, e := range entries {
		result[e.Key] = e.Value
	}
	return result, nil
}

// GetSecrets fetches all configuration entries and returns only those marked as secrets,
// as a flat key→value map.
func (c *Client) GetSecrets(ctx context.Context) (map[string]string, error) {
	entries, err := c.GetConfigEntries(ctx)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for _, e := range entries {
		if e.Secret {
			result[e.Key] = e.Value
		}
	}
	return result, nil
}

// GetVariables fetches all configuration entries and returns only those NOT marked as
// secrets, as a flat key→value map.
func (c *Client) GetVariables(ctx context.Context) (map[string]string, error) {
	entries, err := c.GetConfigEntries(ctx)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for _, e := range entries {
		if !e.Secret {
			result[e.Key] = e.Value
		}
	}
	return result, nil
}

// InjectIntoEnv fetches all configuration entries and writes them into the
// process environment via os.Setenv.
//
// When override is false, keys already present in the environment are left untouched.
func (c *Client) InjectIntoEnv(ctx context.Context, override bool) error {
	entries, err := c.GetConfigEntries(ctx)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if override || os.Getenv(e.Key) == "" {
			if err := os.Setenv(e.Key, e.Value); err != nil {
				return fmt.Errorf("locko: failed to set env var %q: %w", e.Key, err)
			}
		}
	}
	return nil
}
