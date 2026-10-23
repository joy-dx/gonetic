# Gonetic

A small network “service” layer that abstracts request execution behind a common interface. It provides:

- A **NetSvc** facade to execute requests through registered clients
- A default **HTTP client**
- An **S3 client** (get/put/list/delete) with middleware support
- **Retry behavior** with pluggable delay strategies
- A **file downloader** with interchangeable HTTP/curl transports,
  progress notifications, resumable downloads, custom clients, and
  optional SHA-256 verification

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
go get github.com/joy-dx/gonetic/v2
```

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/joy-dx/gonetic/v2"
	"github.com/joy-dx/gonetic/v2/config"
	relayDTO "github.com/joy-dx/relay/v2/dto"
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

`DownloadFile` streams an HTTP or HTTPS resource to disk and returns its
destination path.

Downloads support:

- Periodic progress notifications.
- Optional SHA-256 verification.
- Optional `.part` files.
- Resume using HTTP byte ranges and strong ETags.
- Per-download headers and user agents.
- Custom HTTP clients or download agents.
- Native `net/http` or curl-backed transport.

### Basic download

```go
destination, err := svc.DownloadFile(
	ctx,
	&dto.DownloadFileConfig{
		URL:               "https://example.com/artifact.tar.gz",
		DestinationFolder: "/var/cache/artifacts",
		OutputFileName:    "artifact.tar.gz",
	},
)
if err != nil {
	return err
}

fmt.Println("Downloaded:", destination)
```

If `OutputFileName` is empty, the filename is derived from the URL.
The destination directory is created automatically.

`DownloadFile` copies the configuration and its headers before preparing
the request; it does not modify the caller's configuration.

### Download configuration

| Option | Behavior |
| --- | --- |
| `URL` | HTTP or HTTPS resource to download. |
| `DestinationFolder` | Directory containing the downloaded file. |
| `OutputFileName` | Optional filename; derived from the URL when empty. |
| `UsePartFile` | Download to `<destination>.part`, then rename after successful transfer and verification. |
| `Resume` | Attempt to continue an existing file using saved URL and strong ETag metadata. |
| `Checksum` | Optional expected SHA-256 checksum. |
| `Headers` | Per-download `http.Header`, including multi-value headers. |
| `UserAgent` | Overrides the `User-Agent` header when nonempty. |
| `Agent` | Optional client implementing `Do(*http.Request) (*http.Response, error)`. |

Without an explicit `UserAgent`, an existing `User-Agent` header is
preserved. Otherwise the download user agent defaults to `NetSvc/1.0`.

### Part files, resume, and checksums

```go
destination, err := svc.DownloadFile(
	ctx,
	&dto.DownloadFileConfig{
		URL:               artifactURL,
		DestinationFolder: "/var/cache/artifacts",
		OutputFileName:    "artifact.tar.gz",
		UsePartFile:       true,
		Resume:            true,
		Checksum:          expectedSHA256,
		UserAgent:         "artifact-service/2.0",
		Headers: http.Header{
			"Authorization": {"Bearer " + token},
			"X-Request-ID":  {requestID},
		},
	},
)
if err != nil {
	return err
}

fmt.Println("Downloaded:", destination)
```

With `UsePartFile: true`, the downloader writes to:

```text
/var/cache/artifacts/artifact.tar.gz.part
```

It renames the part file to the final destination only after the transfer,
file synchronization, and optional checksum verification succeed.
A failed transfer or checksum verification does not replace an existing
destination file.

Without `UsePartFile`, the destination itself is the working file and may
contain incomplete data after an error.

#### Resume behavior

Resume is attempted only when:

- `Resume` is enabled.
- The working file contains data.
- Its resume metadata matches the requested URL.
- The metadata contains a strong ETag.

Metadata is stored beside the working file:

```text
artifact.tar.gz.part.resume.json
```

Without part files, the metadata path is:

```text
artifact.tar.gz.resume.json
```

A resume request sends `Range` and `If-Range`. The downloader validates the
returned byte range, total size, and ETag before appending bytes.

If the server ignores the range, returns an invalid partial response, or
rejects the range, the downloader restarts from the beginning. Missing,
invalid, URL-mismatched, or weak-ETag metadata also causes a fresh
download.

Interrupted downloads retain their working file and any valid resume
metadata. Call `DownloadFile` again with the same URL and destination to
attempt resume.

Successful downloads remove the metadata. Checksum failures invalidate
the metadata so known-invalid data is not resumed.

Resume is not an automatic retry loop: a failed transfer returns an error,
and the caller decides when to retry.

### Custom HTTP clients and agents

Set `Agent` to use an existing `*http.Client`, for example one configured
with authentication, proxy settings, a timeout, or a custom transport:

```go
client := &http.Client{
	Timeout: 10 * time.Minute,
}

destination, err := svc.DownloadFile(
	ctx,
	&dto.DownloadFileConfig{
		URL:               artifactURL,
		DestinationFolder: "/var/cache/artifacts",
		OutputFileName:    "artifact.tar.gz",
		UsePartFile:       true,
		Resume:            true,
		Agent:             client,
	},
)
if err != nil {
	return err
}

fmt.Println("Downloaded:", destination)
```

An explicit `Agent` takes precedence over `PreferCurlDownloads`.

Custom agents must return a valid response with a non-nil body. They
should honor the request context and preserve the requested byte
representation.

The downloader owns the `Range` and `If-Range` headers; caller-provided
values are removed. It sets `Accept-Encoding: identity` and rejects
encoded or automatically decompressed responses so byte offsets remain
consistent with the file on disk.

### curl vs net/http

When `Agent` is not provided:

- `PreferCurlDownloads: false` uses `http.DefaultClient`.
- `PreferCurlDownloads: true` uses an `http.Client` backed by
  `CurlTransport`.

Both transports use the same download pipeline for progress accounting,
resume validation, checksums, part files, and final notifications.

Curl acts as a streaming transport. It does not write the destination
file directly, and progress is not parsed from curl's progress output.
Redirect handling belongs to the wrapping `http.Client`.

`CurlTransport` supports HTTP/HTTPS GET requests without a request body.
It uses the `curl` executable on `PATH` by default. An alternative
executable can be configured explicitly:

```go
client := &http.Client{
	Transport: gonetic.CurlTransport{
		Executable: "/usr/local/bin/curl",
	},
}
```

Pass that client as the download `Agent`.

The curl transport itself does not fall back to `net/http` if launching
curl fails.

### Progress updates and listeners

Subscribe before starting the download:

```go
artifactURL := "https://example.com/artifact.tar.gz"

updates, unsubscribe := svc.TransferListener(artifactURL)
defer unsubscribe()

go func() {
	for update := range updates {
		fmt.Printf(
			"%s: %d / %d bytes (%.1f%%)\n",
			update.Status,
			update.Downloaded,
			update.TotalSize,
			update.Percentage,
		)
	}
}()

destination, err := svc.DownloadFile(
	ctx,
	&dto.DownloadFileConfig{
		URL:               artifactURL,
		DestinationFolder: "/tmp",
		UsePartFile:       true,
		Resume:            true,
	},
)
if err != nil {
	return err
}

fmt.Println("Downloaded:", destination)
```

Notifications include:

- `IN_PROGRESS`: transfer started or periodic progress.
- `COMPLETE`: transfer and optional verification succeeded.
- `ERROR`: download failed.
- `STOPPED`: the download context was canceled or expired.

`DownloadCallbackInterval` controls periodic progress updates and defaults
to two seconds when configured with a nonpositive duration.

Progress counts bytes written to disk, including an existing resumed
prefix. If the total size is unknown, percentage remains zero during the
transfer; successful completion reports the actual size and 100%.

Listeners are buffered. Intermediate progress snapshots may be replaced
when a subscriber falls behind, but terminal notifications are retained.
A terminal notification closes all current listeners for that URL.

Calling the unsubscribe function is safe more than once, including after
a terminal notification. To close all listeners for a URL explicitly:

```go
svc.TransferListenerClose(artifactURL)
```

Listeners are keyed by URL, not by an individual download invocation.

### Cancellation and concurrent downloads

Cancel or expire the context passed to `DownloadFile` to stop a transfer.

A stopped transfer does not promote a part file to the final destination.
When resume metadata is available, a later invocation can attempt to
continue the working file.

A `NetSvc` instance rejects overlapping active destination or working-file
paths. This protection is local to that service instance; it is not a
cross-process filesystem lock.

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

