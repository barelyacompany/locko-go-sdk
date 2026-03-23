package locko

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const apiURL = "https://api-locko.barelyacompany.com/api/api-keys/config"

const defaultTimeout = 3 * time.Second

type ConfigEntry struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

var ErrUnauthorized = errors.New("locko: unauthorized — check your API key")

var ErrNotFound = errors.New("locko: resource not found")

type ErrServer struct {
	StatusCode int
}

func (e *ErrServer) Error() string {
	return fmt.Sprintf("locko: server error (status %d)", e.StatusCode)
}

type prefetchResult struct {
	entries []ConfigEntry
	warning string
}

type Client struct {
	apiKey     string
	httpClient *http.Client
	resultCh   chan prefetchResult
	once       sync.Once
	cached     prefetchResult
}

func NewClient(apiKey string, httpClient ...*http.Client) *Client {
	hc := &http.Client{Timeout: defaultTimeout}
	if len(httpClient) > 0 && httpClient[0] != nil {
		hc = httpClient[0]
	}
	c := &Client{
		apiKey:     apiKey,
		httpClient: hc,
		resultCh:   make(chan prefetchResult, 1),
	}
	go c.backgroundFetch()
	return c
}

func (c *Client) backgroundFetch() {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	entries, err := c.GetConfigEntries(ctx)
	if err != nil {
		c.resultCh <- prefetchResult{
			warning: fmt.Sprintf(
				"failed to fetch remote config — %v. Falling back to process environment.", err,
			),
		}
	} else {
		c.resultCh <- prefetchResult{entries: entries}
	}
	close(c.resultCh)
}

func (c *Client) await() prefetchResult {
	c.once.Do(func() {
		c.cached = <-c.resultCh
	})
	return c.cached
}

func (c *Client) Initialize() {
	c.await()
}

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

func (c *Client) GetConfig(override bool) map[string]string {
	r := c.await()
	if r.warning != "" {
		fmt.Fprintf(os.Stderr, "[Locko] WARNING: %s\n", r.warning)
	}

	envMap := processEnvMap()
	if r.entries == nil {
		return envMap
	}

	result := make(map[string]string, len(r.entries)+len(envMap))
	for _, e := range r.entries {
		result[e.Key] = e.Value
	}
	if override {
		for k, v := range envMap {
			if _, exists := result[k]; !exists {
				result[k] = v
			}
		}
	} else {
		for k, v := range envMap {
			result[k] = v
		}
	}
	return result
}

func (c *Client) GetSecrets(override bool) map[string]string {
	r := c.await()
	if r.warning != "" {
		fmt.Fprintf(os.Stderr, "[Locko] WARNING: %s\n", r.warning)
	}

	if r.entries == nil {
		return processEnvMap()
	}

	result := make(map[string]string)
	for _, e := range r.entries {
		if !e.Secret {
			continue
		}
		if !override {
			if v := os.Getenv(e.Key); v != "" {
				result[e.Key] = v
				continue
			}
		}
		result[e.Key] = e.Value
	}
	return result
}

func (c *Client) GetVariables(override bool) map[string]string {
	r := c.await()
	if r.warning != "" {
		fmt.Fprintf(os.Stderr, "[Locko] WARNING: %s\n", r.warning)
	}

	if r.entries == nil {
		return processEnvMap()
	}

	result := make(map[string]string)
	for _, e := range r.entries {
		if e.Secret {
			continue
		}
		if !override {
			if v := os.Getenv(e.Key); v != "" {
				result[e.Key] = v
				continue
			}
		}
		result[e.Key] = e.Value
	}
	return result
}

func (c *Client) InjectIntoEnv(override bool) error {
	r := c.await()
	if r.warning != "" {
		fmt.Fprintf(os.Stderr, "[Locko] WARNING: %s\n", r.warning)
		return nil
	}
	for _, e := range r.entries {
		if override || os.Getenv(e.Key) == "" {
			if err := os.Setenv(e.Key, e.Value); err != nil {
				return fmt.Errorf("locko: failed to set env var %q: %w", e.Key, err)
			}
		}
	}
	return nil
}

func processEnvMap() map[string]string {
	raw := os.Environ()
	m := make(map[string]string, len(raw))
	for _, kv := range raw {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}
