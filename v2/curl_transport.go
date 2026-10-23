package gonetic

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
)

// CurlTransport implements http.RoundTripper by running curl as a subprocess.
//
// Curl supplies the response headers and body, while the surrounding
// http.Client remains responsible for client-level behavior such as redirects.
// The download pipeline remains responsible for writing files, reporting
// progress, validating resumed responses, and verifying checksums.
//
// Only HTTP/HTTPS GET requests without a request body are supported.
type CurlTransport struct {
	// Executable is the curl binary to run. It defaults to "curl", resolved
	// using PATH.
	Executable string
}

// RoundTrip executes one HTTP request through curl and returns a streaming
// response.
//
// The curl process stays associated with the returned response body. Reading
// that body to EOF waits for curl and checks its exit status. Closing the body
// early cancels the process.
//
// As with other RoundTrippers, an HTTP error status is returned as a response,
// not as a transport error. The caller decides which statuses are acceptable.
func (t CurlTransport) RoundTrip(
	req *http.Request,
) (*http.Response, error) {
	if req.Method != http.MethodGet || req.Body != nil {
		return nil, errors.New("curl download transport only supports GET")
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return nil, errors.New("curl download requires HTTP or HTTPS")
	}

	executable := t.Executable
	if executable == "" {
		executable = "curl"
	}

	// The request context controls the subprocess. A separate cancel function
	// also lets Body.Close stop curl when the caller abandons the response.
	ctx, cancel := context.WithCancel(req.Context())

	// Pass request-specific values through curl's configuration input rather
	// than constructing shell commands or placing headers in command arguments.
	// No shell is involved, but values still need curl-config quoting.
	var config strings.Builder
	fmt.Fprintf(
		&config,
		"url = %s\n",
		curlConfigQuote(req.URL.String()),
	)

	for name, values := range req.Header {
		if !validDownloadHeaderName(name) {
			cancel()
			return nil, fmt.Errorf("invalid header name %q", name)
		}

		for _, value := range values {
			// Reject line breaks and NULs before serializing a header.
			if strings.ContainsAny(value, "\r\n\x00") {
				cancel()
				return nil, fmt.Errorf(
					"invalid value for header %q",
					name,
				)
			}

			// Preserve multiple values as separate header entries.
			fmt.Fprintf(
				&config,
				"header = %s\n",
				curlConfigQuote(name+": "+value),
			)
		}
	}

	// Go represents an explicit Host override in Request.Host rather than
	// Request.Header, so forward it separately.
	if req.Host != "" {
		if strings.ContainsAny(req.Host, "\r\n\x00") {
			cancel()
			return nil, errors.New("invalid request host")
		}
		fmt.Fprintf(
			&config,
			"header = %s\n",
			curlConfigQuote("Host: "+req.Host),
		)
	}

	// --disable must be the first curl option: it prevents a user's default
	// curl configuration from changing this transport's behavior.
	//
	// Redirects are disabled here so the wrapping http.Client handles them.
	// HTTP/1.1 and --include produce a response stream http.ReadResponse can
	// parse. --raw preserves transfer encoding, including chunk framing, for
	// Go's HTTP parser to consume.
	//
	// Suppressing proxy CONNECT headers prevents a proxy handshake response
	// from being mistaken for the origin server's response.
	cmd := exec.CommandContext(
		ctx,
		executable,
		"--disable",
		"--silent",
		"--show-error",
		"--no-buffer",
		"--no-location",
		"--http1.1",
		"--raw",
		"--include",
		"--suppress-connect-headers",
		"--globoff",
		"--proto",
		"=http,https",
		"--config",
		"-",
	)
	cmd.Stdin = strings.NewReader(config.String())

	// Keep a bounded prefix of curl's diagnostics for error messages.
	stderr := &limitedDownloadBuffer{limit: 64 * 1024}
	cmd.Stderr = stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		cancel()
		_ = stdout.Close()
		return nil, fmt.Errorf("start curl: %w", err)
	}

	// Reuse this buffered reader for both headers and body. Parsing headers
	// may read ahead into the body, so switching back to stdout would lose
	// any bytes already buffered here.
	reader := bufio.NewReader(stdout)

	var resp *http.Response
	for {
		resp, err = http.ReadResponse(reader, req)
		if err != nil {
			// No response body can be returned to own cleanup. Stop and reap
			// the process here before accessing its completed diagnostics.
			cancel()
			_ = stdout.Close()
			_ = cmd.Wait()

			return nil, fmt.Errorf(
				"read curl response: %w: %s",
				err,
				strings.TrimSpace(stderr.String()),
			)
		}

		// Informational responses may precede the final response, for example
		// 103 Early Hints. Continue parsing until a final status is received.
		if resp.StatusCode >= 100 && resp.StatusCode < 200 {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusSwitchingProtocols {
				cancel()
				_ = stdout.Close()
				_ = cmd.Wait()
				return nil, errors.New(
					"protocol upgrades are not supported",
				)
			}
			continue
		}

		break
	}

	// Transfer subprocess ownership to the body. The caller must consume
	// the body to EOF or close it, just as with a normal HTTP response.
	resp.Body = &curlDownloadBody{
		body:   resp.Body,
		stdout: stdout,
		cmd:    cmd,
		cancel: cancel,
		stderr: stderr,
	}

	return resp, nil
}

// curlConfigQuote returns a double-quoted curl configuration value.
//
// This is curl-config escaping, not shell escaping. Backslashes are escaped
// first so escape sequences introduced for other characters remain intact.
func curlConfigQuote(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"\"", "\\\"",
		"\n", "\\n",
		"\r", "\\r",
		"\t", "\\t",
	)
	return `"` + replacer.Replace(value) + `"`
}

// validDownloadHeaderName reports whether name contains only HTTP token
// characters. Empty names, separators, whitespace, controls, and non-ASCII
// bytes are rejected.
func validDownloadHeaderName(name string) bool {
	if name == "" {
		return false
	}

	for i := 0; i < len(name); i++ {
		c := name[i]
		if c <= 32 || c >= 127 ||
			strings.ContainsRune("()<>@,;:\\\"/[]?={}", rune(c)) {
			return false
		}
	}

	return true
}

// limitedDownloadBuffer retains at most limit bytes while accepting all writes.
//
// It bounds diagnostic memory use without causing curl's stderr copy to fail
// once the retained prefix is full. It is not independently synchronized.
type limitedDownloadBuffer struct {
	bytes.Buffer
	limit int
}

// Write stores the portion of p that fits and discards the rest.
//
// Returning the original length tells the writer that all input was accepted,
// including bytes intentionally discarded after reaching the limit.
func (b *limitedDownloadBuffer) Write(p []byte) (int, error) {
	originalLength := len(p)

	remaining := b.limit - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}

	return originalLength, nil
}

// curlDownloadBody couples the parsed HTTP body to its curl subprocess.
//
// Cleanup is performed once, whether triggered by EOF or an explicit Close.
// The stored result is returned by subsequent cleanup calls.
type curlDownloadBody struct {
	body   io.ReadCloser
	stdout io.ReadCloser
	cmd    *exec.Cmd
	cancel context.CancelFunc
	stderr *limitedDownloadBuffer

	once sync.Once
	err  error
}

// Read streams parsed response bytes.
//
// HTTP body completion alone does not prove curl exited successfully. At EOF,
// wait for the process and replace EOF with a process error when necessary.
func (b *curlDownloadBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if err == io.EOF {
		if waitErr := b.finish(false); waitErr != nil {
			return n, waitErr
		}
	}
	return n, err
}

// Close releases the response and stops curl if cleanup has not already run.
//
// Callers should also close the body after a non-EOF read error, because such
// errors do not trigger finish from Read.
func (b *curlDownloadBody) Close() error {
	return b.finish(true)
}

// finish closes the body, reaps the subprocess, and caches the cleanup result.
//
// abort distinguishes an explicit close from normal EOF. When aborting, cancel
// and close stdout before closing the parsed body so cleanup does not attempt
// to drain a still-running download.
func (b *curlDownloadBody) finish(abort bool) error {
	b.once.Do(func() {
		if abort {
			b.cancel()
			_ = b.stdout.Close()
		}

		closeErr := b.body.Close()
		waitErr := b.cmd.Wait()

		b.cancel()
		_ = b.stdout.Close()

		// Wait completes stderr collection before diagnostics are read.
		// Prefer a process failure over an HTTP body close failure.
		switch {
		case waitErr != nil:
			b.err = fmt.Errorf(
				"curl failed: %w: %s",
				waitErr,
				strings.TrimSpace(b.stderr.String()),
			)
		case closeErr != nil:
			b.err = closeErr
		}
	})

	return b.err
}
