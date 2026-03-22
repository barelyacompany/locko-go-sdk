# locko-go-sdk

Official Go SDK for [Locko](https://barelyacompany.com) — a secrets and configuration management tool.

## Installation

```bash
go get github.com/barelyacompany/locko-go-sdk
```

Requires Go 1.21 or later. No external dependencies.

## Quick Start

### Early initialisation (recommended)

Call `InjectIntoEnv` at the very top of `main()` — before initialising any subsystem that reads from `os.Getenv` (databases, caches, HTTP clients, etc.). This writes every Locko value directly into the process environment so the rest of your app boots with the correct config already in place.

```go
package main

import (
    "context"
    "database/sql"
    "log"
    "os"

    locko "github.com/barelyacompany/locko-go-sdk"
    _ "github.com/lib/pq"
)

func main() {
    client := locko.NewClient(os.Getenv("LOCKO_API_KEY"))

    if err := client.InjectIntoEnv(context.Background(), false); err != nil {
        log.Fatalf("failed to load config from Locko: %v", err)
    }

    // os.Getenv now returns values from Locko
    db, err := sql.Open("postgres", os.Getenv("DATABASE_URL"))
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()
}
```

The second argument (`override`) controls whether Locko values overwrite variables that are already set in the environment:

```go
client.InjectIntoEnv(ctx, false) // safe — won't clobber existing env
client.InjectIntoEnv(ctx, true)  // force-overwrite everything
```

### Fetching config manually

If you prefer to work with the values directly rather than writing to the environment:

```go
cfg, err := client.GetConfig(context.Background())
if err != nil {
    log.Fatal(err)
}
fmt.Println("DATABASE_URL:", cfg["DATABASE_URL"])
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
