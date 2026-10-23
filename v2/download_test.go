package gonetic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joy-dx/gonetic/v2/config"
	"github.com/joy-dx/gonetic/v2/dto"
	"github.com/joy-dx/lockablemap"
)

type downloadTestAgent func(*http.Request) (*http.Response, error)

func (f downloadTestAgent) Do(
	req *http.Request,
) (*http.Response, error) {
	return f(req)
}

func newDownloadTestService(preferCurl bool) *NetSvc {
	cfg := config.DefaultNetSvcConfig()
	cfg.PreferCurlDownloads = preferCurl
	cfg.DownloadCallbackInterval = time.Millisecond

	return &NetSvc{
		cfg:            &cfg,
		relay:          &fakeRelay{},
		clients:        map[string]dto.NetClientInterface{},
		transferState:  *lockablemap.NewLockableMap[string, dto.TransferNotification](),
		listenersByURL: map[string][]chan dto.TransferNotification{},
	}
}

func testDownloadTransports(
	t *testing.T,
	run func(*testing.T, bool),
) {
	t.Helper()

	for _, transport := range []struct {
		name string
		curl bool
	}{
		{name: "http"},
		{name: "curl", curl: true},
	} {
		transport := transport

		t.Run(transport.name, func(t *testing.T) {
			t.Parallel()

			if transport.curl {
				if _, err := exec.LookPath("curl"); err != nil {
					t.Skip("curl not available on PATH")
				}
			}

			run(t, transport.curl)
		})
	}
}

func downloadTestChecksum(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func requireDownloadFile(
	t *testing.T,
	path string,
	want string,
) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}

	if string(content) != want {
		t.Fatalf("file content = %q, want %q", content, want)
	}
}

func requireDownloadPathAbsent(t *testing.T, path string) {
	t.Helper()

	_, err := os.Stat(path)
	if !os.IsNotExist(err) {
		t.Fatalf("expected %q to be absent, got %v", path, err)
	}
}

// DownloadFile publishes its terminal notification before returning.
// Require both that notification and closure of the subscription.
func requireDownloadTerminal(
	t *testing.T,
	ch <-chan dto.TransferNotification,
	want dto.TransferStatus,
) dto.TransferNotification {
	t.Helper()

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	var final dto.TransferNotification
	terminalCount := 0

	for {
		select {
		case update, ok := <-ch:
			if !ok {
				if terminalCount != 1 {
					t.Fatalf(
						"got %d terminal updates, want 1",
						terminalCount,
					)
				}
				if final.Status != want {
					t.Fatalf(
						"terminal status = %v, want %v: %+v",
						final.Status,
						want,
						final,
					)
				}
				return final
			}

			if terminalCount != 0 {
				t.Fatal("received an update after terminal notification")
			}

			switch update.Status {
			case dto.COMPLETE, dto.ERROR, dto.STOPPED:
				terminalCount++
				final = update
			}

		case <-timer.C:
			t.Fatal("timed out waiting for terminal notification")
		}
	}
}

func TestDownloadFile_Transports(t *testing.T) {
	testDownloadTransports(t, func(t *testing.T, preferCurl bool) {
		const content = "hello artifact\n"

		tests := []struct {
			name       string
			path       string
			body       string
			chunked    bool
			redirect   bool
			status     int
			encoding   string
			checksum   string
			wantStatus dto.TransferStatus
		}{
			{
				name:       "success",
				path:       "/artifact.txt",
				body:       content,
				wantStatus: dto.COMPLETE,
			},
			{
				name:       "empty file",
				path:       "/empty.txt",
				wantStatus: dto.COMPLETE,
			},
			{
				name:       "unknown content length",
				path:       "/chunked.txt",
				body:       content,
				chunked:    true,
				wantStatus: dto.COMPLETE,
			},
			{
				name:       "redirect",
				path:       "/redirect",
				body:       content,
				redirect:   true,
				wantStatus: dto.COMPLETE,
			},
			{
				name:       "valid checksum",
				path:       "/artifact.txt",
				body:       content,
				checksum:   downloadTestChecksum(content),
				wantStatus: dto.COMPLETE,
			},
			{
				name:       "invalid checksum",
				path:       "/artifact.txt",
				body:       content,
				checksum:   downloadTestChecksum("different content"),
				wantStatus: dto.ERROR,
			},
			{
				name:       "not found",
				path:       "/missing.txt",
				status:     http.StatusNotFound,
				wantStatus: dto.ERROR,
			},
			{
				name:       "encoded representation rejected",
				path:       "/encoded.txt",
				body:       content,
				encoding:   "gzip",
				wantStatus: dto.ERROR,
			},
		}

		for _, tt := range tests {
			tt := tt

			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				server := httptest.NewServer(http.HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) {
						if tt.redirect && r.URL.Path == "/redirect" {
							http.Redirect(
								w,
								r,
								"/artifact.txt",
								http.StatusFound,
							)
							return
						}

						if tt.encoding != "" {
							w.Header().Set(
								"Content-Encoding",
								tt.encoding,
							)
						}

						if !tt.chunked {
							w.Header().Set(
								"Content-Length",
								strconv.Itoa(len(tt.body)),
							)
						}

						status := tt.status
						if status == 0 {
							status = http.StatusOK
						}
						w.WriteHeader(status)

						if tt.chunked {
							w.(http.Flusher).Flush()
						}

						_, _ = io.WriteString(w, tt.body)
					},
				))
				t.Cleanup(server.Close)

				s := newDownloadTestService(preferCurl)
				cfg := dto.DownloadFileConfig{
					URL:               server.URL + tt.path,
					DestinationFolder: t.TempDir(),
					Checksum:          tt.checksum,
				}

				ch, unsubscribe := s.TransferListener(cfg.URL)
				defer unsubscribe()

				destination, err := s.DownloadFile(
					context.Background(),
					&cfg,
				)

				wantErr := tt.wantStatus != dto.COMPLETE
				if (err != nil) != wantErr {
					t.Fatalf("error = %v, wantErr = %v", err, wantErr)
				}

				final := requireDownloadTerminal(
					t,
					ch,
					tt.wantStatus,
				)

				wantDestination := filepath.Join(
					cfg.DestinationFolder,
					strings.TrimPrefix(tt.path, "/"),
				)
				if destination != wantDestination {
					t.Fatalf(
						"destination = %q, want %q",
						destination,
						wantDestination,
					)
				}

				// Filename derivation must not mutate caller config.
				if cfg.OutputFileName != "" {
					t.Fatalf(
						"caller filename changed to %q",
						cfg.OutputFileName,
					)
				}

				if tt.wantStatus != dto.COMPLETE {
					return
				}

				requireDownloadFile(t, destination, tt.body)

				wantSize := int64(len(tt.body))
				if final.Downloaded != wantSize ||
					final.TotalSize != wantSize ||
					final.Percentage != 100 {
					t.Fatalf("incorrect completion totals: %+v", final)
				}
			})
		}
	})
}

func TestDownloadFile_CustomAgentAndHeaders(t *testing.T) {
	const content = "custom client artifact"

	var calls int32
	var observed http.Header

	agent := downloadTestAgent(
		func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			observed = req.Header.Clone()

			return &http.Response{
				StatusCode:    http.StatusOK,
				Status:        "200 OK",
				Header:        make(http.Header),
				ContentLength: int64(len(content)),
				Body:          io.NopCloser(strings.NewReader(content)),
				Request:       req,
			}, nil
		},
	)

	// An explicit agent takes precedence over the curl preference.
	s := newDownloadTestService(true)
	cfg := dto.DownloadFileConfig{
		URL:               "https://example.invalid/artifact.bin",
		DestinationFolder: t.TempDir(),
		OutputFileName:    "artifact.bin",
		Agent:             agent,
		UserAgent:         "artifact-tests/2.0",
		Headers: http.Header{
			"Authorization":   {"Bearer test-token"},
			"X-Artifact":      {"one", "two"},
			"Accept-Encoding": {"gzip"},
			"Range":           {"bytes=123-"},
			"If-Range":        {`"caller-validator"`},
			"User-Agent":      {"caller-user-agent"},
		},
	}
	originalHeaders := cfg.Headers.Clone()

	destination, err := s.DownloadFile(context.Background(), &cfg)
	if err != nil {
		t.Fatal(err)
	}

	requireDownloadFile(t, destination, content)

	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("agent calls = %d, want 1", calls)
	}
	if observed.Get("Authorization") != "Bearer test-token" {
		t.Fatalf("authorization header missing: %v", observed)
	}
	if !reflect.DeepEqual(
		observed.Values("X-Artifact"),
		[]string{"one", "two"},
	) {
		t.Fatalf("multi-value header changed: %v", observed)
	}
	if observed.Get("User-Agent") != "artifact-tests/2.0" {
		t.Fatalf("incorrect user agent: %v", observed)
	}
	if observed.Get("Accept-Encoding") != "identity" {
		t.Fatalf("incorrect accept encoding: %v", observed)
	}
	if observed.Get("Range") != "" || observed.Get("If-Range") != "" {
		t.Fatalf("caller range headers were not removed: %v", observed)
	}
	if !reflect.DeepEqual(cfg.Headers, originalHeaders) {
		t.Fatalf("caller headers mutated: %v", cfg.Headers)
	}
}

func TestDownloadFile_UserAgent(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		userAgent string
		want      string
	}{
		{
			name: "default",
			want: "NetSvc/1.0",
		},
		{
			name:   "header",
			header: "header-agent/1.0",
			want:   "header-agent/1.0",
		},
		{
			name:      "explicit option overrides header",
			header:    "header-agent/1.0",
			userAgent: "option-agent/2.0",
			want:      "option-agent/2.0",
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			s := newDownloadTestService(false)
			cfg := dto.DownloadFileConfig{
				URL:               "https://example.invalid/file",
				DestinationFolder: t.TempDir(),
				OutputFileName:    "file",
				UserAgent:         tt.userAgent,
				Headers:           make(http.Header),
				Agent: downloadTestAgent(
					func(req *http.Request) (*http.Response, error) {
						if got := req.UserAgent(); got != tt.want {
							t.Errorf(
								"User-Agent = %q, want %q",
								got,
								tt.want,
							)
						}

						return &http.Response{
							StatusCode:    http.StatusOK,
							Header:        make(http.Header),
							ContentLength: 0,
							Body:          http.NoBody,
							Request:       req,
						}, nil
					},
				),
			}
			if tt.header != "" {
				cfg.Headers.Set("User-Agent", tt.header)
			}

			if _, err := s.DownloadFile(
				context.Background(),
				&cfg,
			); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDownloadFile_InterruptedDownloadResumes(t *testing.T) {
	testDownloadTransports(t, func(t *testing.T, preferCurl bool) {
		const content = "a complete artifact downloaded in two requests"
		const prefixLength = 11
		const etag = `"artifact-v1"`

		var requests int32

		server := httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				call := atomic.AddInt32(&requests, 1)
				w.Header().Set("ETag", etag)

				switch call {
				case 1:
					if r.Header.Get("Range") != "" {
						t.Error("initial request unexpectedly has Range")
					}

					// Advertise the full length, then end prematurely.
					w.Header().Set(
						"Content-Length",
						strconv.Itoa(len(content)),
					)
					_, _ = io.WriteString(
						w,
						content[:prefixLength],
					)

				case 2:
					wantRange := fmt.Sprintf(
						"bytes=%d-",
						prefixLength,
					)
					if got := r.Header.Get("Range"); got != wantRange {
						t.Errorf("Range = %q, want %q", got, wantRange)
					}
					if got := r.Header.Get("If-Range"); got != etag {
						t.Errorf("If-Range = %q, want %q", got, etag)
					}

					w.Header().Set(
						"Content-Range",
						fmt.Sprintf(
							"bytes %d-%d/%d",
							prefixLength,
							len(content)-1,
							len(content),
						),
					)
					w.Header().Set(
						"Content-Length",
						strconv.Itoa(len(content)-prefixLength),
					)
					w.WriteHeader(http.StatusPartialContent)
					_, _ = io.WriteString(
						w,
						content[prefixLength:],
					)

				default:
					t.Errorf("unexpected request %d", call)
					http.Error(w, "unexpected request", 500)
				}
			},
		))
		t.Cleanup(server.Close)

		s := newDownloadTestService(preferCurl)
		cfg := dto.DownloadFileConfig{
			URL:               server.URL + "/artifact.bin",
			DestinationFolder: t.TempDir(),
			OutputFileName:    "artifact.bin",
			UsePartFile:       true,
			Resume:            true,
			Checksum:          downloadTestChecksum(content),
		}

		destination := filepath.Join(
			cfg.DestinationFolder,
			cfg.OutputFileName,
		)
		partPath := destination + ".part"
		metaPath := partPath + ".resume.json"

		ch, unsubscribe := s.TransferListener(cfg.URL)
		defer unsubscribe()

		if _, err := s.DownloadFile(
			context.Background(),
			&cfg,
		); err == nil {
			t.Fatal("expected interrupted download to fail")
		}
		requireDownloadTerminal(t, ch, dto.ERROR)

		requireDownloadPathAbsent(t, destination)
		requireDownloadFile(t, partPath, content[:prefixLength])

		metadata, err := readDownloadMetadata(metaPath)
		if err != nil {
			t.Fatal(err)
		}
		if metadata.URL != cfg.URL || metadata.ETag != etag {
			t.Fatalf("incorrect resume metadata: %+v", metadata)
		}

		ch, unsubscribeAgain := s.TransferListener(cfg.URL)
		defer unsubscribeAgain()

		got, err := s.DownloadFile(context.Background(), &cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got != destination {
			t.Fatalf("destination = %q, want %q", got, destination)
		}

		final := requireDownloadTerminal(t, ch, dto.COMPLETE)
		if final.Downloaded != int64(len(content)) ||
			final.TotalSize != int64(len(content)) {
			t.Fatalf("incorrect resumed totals: %+v", final)
		}

		requireDownloadFile(t, destination, content)
		requireDownloadPathAbsent(t, partPath)
		requireDownloadPathAbsent(t, metaPath)

		if got := atomic.LoadInt32(&requests); got != 2 {
			t.Fatalf("requests = %d, want 2", got)
		}
	})
}

func TestDownloadFile_ResumeFallback(t *testing.T) {
	testDownloadTransports(t, func(t *testing.T, preferCurl bool) {
		const content = "new artifact"
		const oldETag = `"old-version"`

		tests := []struct {
			name         string
			metadata     string
			response     int
			responseETag string
			contentRange string
			wantRange    bool
			wantRequests int32
		}{
			{
				name:         "missing metadata",
				wantRequests: 1,
			},
			{
				name:         "malformed metadata",
				metadata:     "not-json",
				wantRequests: 1,
			},
			{
				name: "metadata URL differs",
				metadata: `{
					"url": "https://different.invalid/file",
					"etag": "\"old-version\""
				}`,
				wantRequests: 1,
			},
			{
				name: "weak validator",
				metadata: `{
					"url": "$URL",
					"etag": "W/\"old-version\""
				}`,
				wantRequests: 1,
			},
			{
				name:         "range ignored",
				wantRange:    true,
				response:     http.StatusOK,
				wantRequests: 1,
			},
			{
				name:         "range unsatisfiable",
				wantRange:    true,
				response:     http.StatusRequestedRangeNotSatisfiable,
				wantRequests: 2,
			},
			{
				name:         "incorrect range",
				wantRange:    true,
				response:     http.StatusPartialContent,
				responseETag: oldETag,
				contentRange: "bytes 1-11/12",
				wantRequests: 2,
			},
			{
				name:         "changed validator",
				wantRange:    true,
				response:     http.StatusPartialContent,
				responseETag: `"new-version"`,
				contentRange: "bytes 3-11/12",
				wantRequests: 2,
			},
		}

		for _, tt := range tests {
			tt := tt

			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				var requests int32

				server := httptest.NewServer(http.HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) {
						call := atomic.AddInt32(&requests, 1)
						expectRange := tt.wantRange && call == 1

						if expectRange {
							if got := r.Header.Get("Range"); got != "bytes=3-" {
								t.Errorf("Range = %q, want bytes=3-", got)
							}
							if got := r.Header.Get("If-Range"); got != oldETag {
								t.Errorf(
									"If-Range = %q, want %q",
									got,
									oldETag,
								)
							}
						} else if r.Header.Get("Range") != "" ||
							r.Header.Get("If-Range") != "" {
							t.Error("full request contains range headers")
						}

						if expectRange &&
							tt.response != http.StatusOK {
							if tt.responseETag != "" {
								w.Header().Set(
									"ETag",
									tt.responseETag,
								)
							}
							if tt.contentRange != "" {
								w.Header().Set(
									"Content-Range",
									tt.contentRange,
								)
							}

							w.WriteHeader(tt.response)
							_, _ = io.WriteString(w, "invalid partial")
							return
						}

						w.Header().Set("ETag", `"new-version"`)
						_, _ = io.WriteString(w, content)
					},
				))
				t.Cleanup(server.Close)

				s := newDownloadTestService(preferCurl)
				cfg := dto.DownloadFileConfig{
					URL:               server.URL + "/artifact.bin",
					DestinationFolder: t.TempDir(),
					OutputFileName:    "artifact.bin",
					UsePartFile:       true,
					Resume:            true,
				}

				destination := filepath.Join(
					cfg.DestinationFolder,
					cfg.OutputFileName,
				)
				partPath := destination + ".part"
				metaPath := partPath + ".resume.json"

				if err := os.WriteFile(
					partPath,
					[]byte("old"),
					0o644,
				); err != nil {
					t.Fatal(err)
				}

				switch {
				case tt.wantRange:
					err := writeDownloadMetadata(
						metaPath,
						downloadMetadata{
							URL:  cfg.URL,
							ETag: oldETag,
						},
					)
					if err != nil {
						t.Fatal(err)
					}

				case tt.metadata != "":
					data := strings.ReplaceAll(
						tt.metadata,
						"$URL",
						cfg.URL,
					)
					if err := os.WriteFile(
						metaPath,
						[]byte(data),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}

				if _, err := s.DownloadFile(
					context.Background(),
					&cfg,
				); err != nil {
					t.Fatal(err)
				}

				requireDownloadFile(t, destination, content)
				requireDownloadPathAbsent(t, partPath)
				requireDownloadPathAbsent(t, metaPath)

				if got := atomic.LoadInt32(&requests); got != tt.wantRequests {
					t.Fatalf(
						"requests = %d, want %d",
						got,
						tt.wantRequests,
					)
				}
			})
		}
	})
}

func TestDownloadFile_ChecksumFailurePreservesDestination(t *testing.T) {
	testDownloadTransports(t, func(t *testing.T, preferCurl bool) {
		const content = "invalid replacement"

		server := httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", `"version-1"`)
				_, _ = io.WriteString(w, content)
			},
		))
		t.Cleanup(server.Close)

		s := newDownloadTestService(preferCurl)
		cfg := dto.DownloadFileConfig{
			URL:               server.URL + "/artifact.bin",
			DestinationFolder: t.TempDir(),
			OutputFileName:    "artifact.bin",
			UsePartFile:       true,
			Resume:            true,
			Checksum:          downloadTestChecksum("expected content"),
		}

		destination := filepath.Join(
			cfg.DestinationFolder,
			cfg.OutputFileName,
		)
		if err := os.WriteFile(
			destination,
			[]byte("previous artifact"),
			0o644,
		); err != nil {
			t.Fatal(err)
		}

		ch, unsubscribe := s.TransferListener(cfg.URL)
		defer unsubscribe()

		if _, err := s.DownloadFile(
			context.Background(),
			&cfg,
		); err == nil {
			t.Fatal("expected checksum failure")
		}

		requireDownloadTerminal(t, ch, dto.ERROR)
		requireDownloadFile(t, destination, "previous artifact")
		requireDownloadFile(t, destination+".part", content)
		requireDownloadPathAbsent(
			t,
			destination+".part.resume.json",
		)
	})
}

func TestDownloadFile_Cancellation(t *testing.T) {
	testDownloadTransports(t, func(t *testing.T, preferCurl bool) {
		server := httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", `"version-1"`)
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, "first chunk")
				w.(http.Flusher).Flush()

				// Keep the response open until the client cancels.
				<-r.Context().Done()
			},
		))
		t.Cleanup(server.Close)

		s := newDownloadTestService(preferCurl)
		cfg := dto.DownloadFileConfig{
			URL:               server.URL + "/artifact.bin",
			DestinationFolder: t.TempDir(),
			OutputFileName:    "artifact.bin",
			UsePartFile:       true,
			Resume:            true,
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ch, unsubscribe := s.TransferListener(cfg.URL)
		defer unsubscribe()

		done := make(chan error, 1)
		go func() {
			_, err := s.DownloadFile(ctx, &cfg)
			done <- err
		}()

		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()

		// Cancel only once the response has opened and progress started.
		select {
		case update, ok := <-ch:
			if !ok || update.Status != dto.IN_PROGRESS {
				t.Fatalf("expected initial progress, got %+v", update)
			}
			cancel()

		case <-timer.C:
			t.Fatal("download did not start")
		}

		select {
		case err := <-done:
			if err == nil {
				t.Fatal("expected cancellation error")
			}

		case <-timer.C:
			t.Fatal("download did not stop after cancellation")
		}

		requireDownloadTerminal(t, ch, dto.STOPPED)
		requireDownloadPathAbsent(
			t,
			filepath.Join(
				cfg.DestinationFolder,
				cfg.OutputFileName,
			),
		)
	})
}

func TestDownloadFile_RejectsInvalidConfiguration(t *testing.T) {
	s := newDownloadTestService(false)

	if _, err := s.DownloadFile(
		context.Background(),
		nil,
	); err == nil {
		t.Fatal("expected nil configuration error")
	}

	cfg := dto.DownloadFileConfig{
		URL:               "file:///tmp/artifact.bin",
		DestinationFolder: t.TempDir(),
		OutputFileName:    "artifact.bin",
	}

	ch, unsubscribe := s.TransferListener(cfg.URL)
	defer unsubscribe()

	if _, err := s.DownloadFile(
		context.Background(),
		&cfg,
	); err == nil {
		t.Fatal("expected unsupported URL scheme error")
	}

	requireDownloadTerminal(t, ch, dto.ERROR)
}

func TestClaimDownloadPaths(t *testing.T) {
	s := newDownloadTestService(false)
	destination := filepath.Join(t.TempDir(), "artifact.bin")
	partPath := destination + ".part"

	release, err := s.claimDownloadPaths(destination, partPath)
	if err != nil {
		t.Fatal(err)
	}

	// Repeated paths in one claim must not conflict with themselves.
	defer release()

	if _, err := s.claimDownloadPaths(partPath); err == nil {
		t.Fatal("expected overlapping path claim to fail")
	}

	release()

	releaseAgain, err := s.claimDownloadPaths(
		destination,
		destination,
		partPath,
	)
	if err != nil {
		t.Fatalf("paths were not released: %v", err)
	}
	releaseAgain()
}
