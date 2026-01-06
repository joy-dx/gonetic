# Gonetic

A small network “service” layer that abstracts request execution behind a common interface. It provides:

- A **NetSvc** facade to execute requests through registered clients
- A default **HTTP client**
- An **S3 client** (get/put/list/delete) with middleware support
- **Retry behavior** with pluggable delay strategies
- A **file downloader** with progress callbacks and optional SHA-256 verification

## Concepts

### NetSvc

- holds global network state/config (headers, timeouts, download options)
- maintains a registry of clients (`ref` → client)
- executes `RequestOnce` / `RequestWithRetry` by dispatching to the correct client
- publishes transfer notifications for downloads

### RequestConfig

- `ClientRef` selects the registered client (default: `dto.NET_DEFAULT_CLIENT_REF`)
- `ReqConfig` is a client-specific request spec implementing `dto.ReqConfigInterface`
- `Timeout` applies a context timeout per call
- `MaxRetries` + `Delay` control retry behavior
- `ResponseObject` optionally unmarshals JSON into a provided struct

## Installation

```bash
go get github.com/joy-dx/gonetic
```

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/joy-dx/gonetic"
	"github.com/joy-dx/gonetic/config"
	"github.com/joy-dx/gonetic/dto"
	relayDTO "github.com/joy-dx/relay/dto"
)

func main() {
	ctx := context.Background()

	var relay relayDTO.RelayInterface = /* your relay impl */

	netCfg := config.DefaultNetSvcConfig().
		WithRelay(relay).
		WithPreferCurl(false) // optional

	svc := gonetic.ProvideNetSvc(&netCfg)

	if err := svc.Hydrate(ctx); err != nil {
		panic(err)
	}

	resp, err := svc.Get(ctx, "https://api.github.com", true)
	if err != nil {
		panic(err)
	}

	fmt.Println("status:", resp.StatusCode)
	fmt.Println("bytes:", len(resp.Body))
}
```
 
## Configuration

The main service comes with the following options

```go
ExtraHeaders             dto.ExtraHeaders
RequestTimeout           time.Duration
UserAgent                string
BlacklistDomains         []string
WhitelistDomains         []string
DownloadCallbackInterval time.Duration
PreferCurlDownloads      bool
```

### Defaults

- `DownloadCallbackInterval`: 2s
- `PreferCurlDownloads`: false

## HTTP client

### Client Configuration

```go
AuthProvider  dto.AuthProvider
OAuthSource   oauth2.TokenSource
RefreshBuffer time.Duration
Middlewares   []Middleware
```

### Request Configuration

```go
Method   string
URL      string
Body     map[string]interface{}
BodyType string // application/json or application/x-www-form-urlencoded
Headers  map[string]string
```

### Defaults
- `Method`: `GET`
- `BodyType`: `application/json`

### HTTP middleware

#### Static headers on every request

```go
httpclient.StaticHeaderMiddleware(map[string]string{
	"X-App-Version": "1.2.3",
})
```

#### Custom Logging

```go
httpclient.LoggingMiddleware(func(msg string) {
	fmt.Println(msg)
})
// logs: [HTTP] GET https://...
```

#### Response Body injection

```go
httpclient.InjectFieldMiddleware("tenant_id", "t-123")
```

## S3 client

### Client Configuration

```go
Region         string
Credentials    aws.CredentialsProvider
Middlewares    []Middleware
ForcePathStyle bool
Endpoint       string // optional custom endpoint
```

### Middleware

#### Add default metadata on PUTs:

```go
s3client.StaticS3MetaMiddleware(map[string]string{
	"owner": "platform-team",
})
```

#### Custom Logging

```go
s3client.LoggingMiddleware(func(msg string) {
	fmt.Println(msg)
})
```
## Request helpers through NetSvc

### GET / POST shortcuts

```go
resp, err := svc.Get(ctx, "https://example.com", true)
resp, err := svc.Post(ctx, "https://example.com", map[string]any{"a": 1}, true)
```

### RequestOnce

`RequestOnce` performs:

- validation (`ClientRef`, `ReqConfig`)
- client lookup from registry
- type-safety check: `netClient.Type() == cfg.ReqConfig.Ref()`
- optional per-call timeout via `context.WithTimeout`
- dispatch to `netClient.ProcessRequest`
- optional JSON unmarshal into `cfg.ResponseObject`

### RequestWithRetry

`RequestWithRetry` retries failures for reliability:

- retries up to `MaxRetries` (attempts = `MaxRetries + 1`)
- uses `cfg.Delay.Wait(cfg.TaskName, attempt)` between attempts (for attempt > 0)
- treats errors as transient if `utils.IsTemporaryErr(err)` returns true
    - current implementation is permissive: if it can’t prove otherwise, it returns `true`
- retries on HTTP `>= 500` responses as server errors

If retries are exhausted on 5xx:
- it returns the last `dto.Response` plus an error indicating attempts were exhausted

### Delay strategies

#### Constant delay:

```go
utils.ConstantDelay{Period: 1} // seconds
```

#### Exponential backoff with jitter (capped at 10s base):

```go
utils.ExponentialBackoff{}
```

Default request config uses:
- `Timeout`: 20s
- `MaxRetries`: 3
- `Delay`: `utils.ExponentialBackoff{}`

## File downloads

NetSvc supports downloading to a destination folder with progress notifications.

```go
cfg := &dto.DownloadFileConfig{
	URL:               "https://host/path/file.zip",
	DestinationFolder: "/tmp/downloads",
	OutputFileName:    "file.zip", // optional; derived from URL if empty
	Checksum:          "",          // optional sha256 hex
}

err := svc.DownloadFile(ctx, cfg)
```

### curl vs net/http

`NetSvcConfig.PreferCurlDownloads` controls the download engine:

- If `PreferCurlDownloads == true`, NetSvc executes:

```bash
curl -L --progress-bar -o <destination> <url>
```

- Otherwise it streams using `net/http` and `io.CopyBuffer`

Special behavior:
- On macOS, `Hydrate()` forces curl preference to align with download security policy.
- If curl is preferred but missing from `$PATH`, it falls back to `net/http`.

### Progress updates and listeners

Both download paths publish `dto.TransferNotification` updates (in-progress, stopped, error, complete).

You can subscribe by URL:

```go
ch, unsub := svc.TransferListener(url)
defer unsub()

go func() {
	for n := range ch {
		// n.Percentage, n.Downloaded, n.TotalSize, n.Status
	}
}()
```

To force-close all listeners for a URL:

```go
svc.TransferListenerClose(url)
```

Notes:
- `downloadFileWithHTTP` reports downloaded/total/percentage periodically (interval from `DownloadCallbackInterval`)
- `downloadFileWithCurl` parses percentage from curl’s progress output and publishes percentage updates

## Examples

### 1) Custom HTTP request + typed response

```go
type GitHubResp struct {
	CurrentUserURL string `json:"current_user_url"`
}

httpCfg := httpclient.DefaultHTTPRequestConfig().
	WithURL("https://api.github.com")

var out GitHubResp

req := dto.DefaultRequestConfig().
	WithClientRef(dto.NET_DEFAULT_CLIENT_REF).
	WithReqConfig(&httpCfg).
	WithResponseObject(&out).
	WithTaskName("github root").
	WithTimeout(10 * time.Second).
	WithMaxRetries(2).
	WithDelay(utils.ExponentialBackoff{})

resp, err := svc.RequestWithRetry(ctx, req)
if err != nil {
	// resp may still be useful on 5xx exhaustion
	panic(err)
}

fmt.Println(resp.StatusCode, out.CurrentUserURL)
```

### 2) Register a second HTTP client (e.g., different middleware/auth)

You create the HTTP client instance (constructor not shown in artefacts, but `Hydrate()` uses `httpclient.NewHTTPClient(ref, netCfg, httpClientCfg)`), then register it:

```go
hcCfg := httpclient.DefaultHTTPClientConfig().
	WithMiddleware(
		httpclient.LoggingMiddleware(log.Println),
		httpclient.StaticHeaderMiddleware(map[string]string{
			"X-Tenant": "t-123",
		}),
	)

custom := httpclient.NewHTTPClient("custom-http", svcCfg, &hcCfg)
svc.RegisterClient("custom-http", custom)
```

Then send a request with `ClientRef: "custom-http"`.

### 3) Download with progress listener

```go
url := "https://example.com/big.tar.gz"
ch, unsub := svc.TransferListener(url)
defer unsub()

go func() {
	for n := range ch {
		fmt.Printf("%s %.1f%%\n", n.Status, n.Percentage)
	}
}()

err := svc.DownloadFile(ctx, &dto.DownloadFileConfig{
	URL:               url,
	DestinationFolder: "/tmp",
})
if err != nil {
	panic(err)
}
```
