# locko-go-sdk

Official Go SDK for [Locko](https://locko.barelyacompany.com) — a secrets and configuration management tool.

## Installation

```bash
go get github.com/barelyacompany/locko
```

Requires Go 1.21 or later. No external dependencies.

## Quick Start

> **You need an API key to fetch config.** The API key is the one credential you must supply yourself — as a real environment variable, a CI secret, or a vault entry. It authenticates the request to Locko; it cannot come from Locko itself.

Fetch your config and wire it up explicitly. This keeps your dependencies clear and your code testable.

```go
package main

import (
    "context"
    "database/sql"
    "log"
    "os"

    locko "github.com/barelyacompany/locko"
    _ "github.com/lib/pq"
)

func main() {
    // LOCKO_API_KEY must already be set before this runs
    client := locko.NewClient(os.Getenv("LOCKO_API_KEY"))

    cfg, err := client.GetConfig(context.Background())
    if err != nil {
        log.Fatalf("failed to load config from Locko: %v", err)
    }

    db, err := sql.Open("postgres", cfg["DATABASE_URL"])
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()
}
```

### Fetching specific subsets

```go
// Only secret entries
secrets, err := client.GetSecrets(ctx)

// Only plain (non-secret) variables
vars, err := client.GetVariables(ctx)
```

### Injecting into the process environment (optional)

If your codebase already reads broadly from `os.Getenv` and you want Locko values picked up automatically, you can inject them in:

```go
// false = won't overwrite keys already in the environment
if err := client.InjectIntoEnv(ctx, false); err != nil {
    log.Fatal(err)
}
// true = force-overwrite everything
if err := client.InjectIntoEnv(ctx, true); err != nil {
    log.Fatal(err)
}
```

## Configuration

### NewClient

```go
client := locko.NewClient(apiKey string, opts ...locko.Option) *locko.Client
```

Creates a new Locko client. Accepts optional functional options.

### Options

| Option | Description |
|--------|-------------|
| `locko.WithServerURL(url string)` | Override the default server URL (`https://api-locko.barelyacompany.com/api`). Trailing slashes are stripped automatically. |
| `locko.WithHTTPClient(client *http.Client)` | Use a custom `*http.Client` (e.g. to set timeouts or a custom transport). |

**Example with options:**

```go
import "net/http"
import "time"

httpClient := &http.Client{Timeout: 5 * time.Second}

client := locko.NewClient(
    "your-api-key",
    locko.WithHTTPClient(httpClient),
    locko.WithServerURL("https://my-self-hosted-locko.example.com/api"),
)
```

## Methods

All methods accept a `context.Context` as the first argument, which is forwarded to the underlying HTTP request.

---

### GetConfig

```go
func (c *Client) GetConfig(ctx context.Context) (map[string]string, error)
```

Returns all configuration entries (both secrets and plain variables) as a flat `key → value` map.

---

### GetSecrets

```go
func (c *Client) GetSecrets(ctx context.Context) (map[string]string, error)
```

Returns only entries marked as secrets (`"secret": true`) as a flat `key → value` map.

---

### GetVariables

```go
func (c *Client) GetVariables(ctx context.Context) (map[string]string, error)
```

Returns only entries **not** marked as secrets (`"secret": false`) as a flat `key → value` map.

---

### InjectIntoEnv

```go
func (c *Client) InjectIntoEnv(ctx context.Context, override bool) error
```

Fetches all entries and writes them into the process environment via `os.Setenv`. When `override` is `false`, keys that already exist in the environment are left untouched.

---

### GetConfigEntries

```go
func (c *Client) GetConfigEntries(ctx context.Context) ([]locko.ConfigEntry, error)
```

Returns the raw API response as a slice of `ConfigEntry`. Use this when you need access to the `Secret` flag alongside the key/value.

```go
type ConfigEntry struct {
    Key    string `json:"key"`
    Value  string `json:"value"`
    Secret bool   `json:"secret"`
}
```

## Error Handling

The SDK returns typed errors for known failure conditions.

| Error | Condition |
|-------|-----------|
| `locko.ErrUnauthorized` | HTTP 401 — invalid or missing API key |
| `locko.ErrNotFound` | HTTP 404 — resource not found |
| `*locko.ErrServer` | Any other non-200 status; carries `.StatusCode` |

**Example:**

```go
import "errors"

cfg, err := client.GetConfig(ctx)
if err != nil {
    if errors.Is(err, locko.ErrUnauthorized) {
        log.Fatal("invalid API key")
    }

    var serverErr *locko.ErrServer
    if errors.As(err, &serverErr) {
        log.Fatalf("server error: status %d", serverErr.StatusCode)
    }

    log.Fatal(err)
}
```

## License

MIT
