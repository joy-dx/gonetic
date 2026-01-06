package gonetic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joy-dx/gonetic/config"
	"github.com/joy-dx/gonetic/dto"
	"github.com/joy-dx/lockablemap"
)

func TestDownloadFile_HTTP_Golden(t *testing.T) {
	t.Parallel()

	// Serve fixed content
	content := []byte("hello world\n")
	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/file2.txt": // the checksum-success case
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
			return
		default:
			// keep the streaming body for cancel test etc
			w.WriteHeader(http.StatusOK)
			fl, _ := w.(http.Flusher)
			for i := 0; i < 256; i++ {
				_, _ = w.Write([]byte(strings.Repeat("x", 8*1024)))
				if fl != nil {
					fl.Flush()
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
	}))
	t.Cleanup(ts.Close)

	tests := []struct {
		name        string
		cfg         dto.DownloadFileConfig
		cancelAfter time.Duration
		wantStatus  dto.TransferStatus
		wantFile    bool
		wantErr     bool
	}{
		{
			name: "success no explicit filename derives from url",
			cfg: dto.DownloadFileConfig{
				URL:               ts.URL + "/file.txt",
				DestinationFolder: t.TempDir(),
			},
			wantStatus: dto.COMPLETE,
			wantFile:   true,
		},
		{
			name: "success with checksum",
			cfg: dto.DownloadFileConfig{
				URL:               ts.URL + "/file2.txt",
				DestinationFolder: t.TempDir(),
				OutputFileName:    "out.txt",
				Checksum:          checksum,
			},
			wantStatus: dto.COMPLETE,
			wantFile:   true,
		},
		{
			name: "bad checksum -> error",
			cfg: dto.DownloadFileConfig{
				URL:               ts.URL + "/file3.txt",
				DestinationFolder: t.TempDir(),
				OutputFileName:    "out.txt",
				Checksum:          "deadbeef",
			},
			wantStatus: dto.ERROR,
			wantFile:   true, // file is written then checksum fails
			wantErr:    true,
		},
		{
			name: "cancel mid download -> stopped",
			cfg: dto.DownloadFileConfig{
				URL:               ts.URL + "/file4.txt",
				DestinationFolder: t.TempDir(),
				OutputFileName:    "out.txt",
			},
			cancelAfter: 30 * time.Millisecond,
			wantStatus:  dto.STOPPED,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := config.DefaultNetSvcConfig()
			cfg.PreferCurlDownloads = false
			cfg.DownloadCallbackInterval = 5 * time.Millisecond

			s := &NetSvc{
				cfg:            &cfg,
				relay:          &fakeRelay{},
				clients:        map[string]dto.NetClientInterface{},
				transferState:  *lockablemap.NewLockableMap[string, dto.TransferNotification](),
				listenersByURL: map[string][]chan dto.TransferNotification{},
			}

			ctx, cancel := context.WithCancel(context.Background())
			if tt.cancelAfter > 0 {
				time.AfterFunc(tt.cancelAfter, cancel)
			}
			defer cancel()

			ch, _ := s.TransferListener(tt.cfg.URL)

			err := s.DownloadFile(ctx, &tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}

			// Collect final notification (COMPLETE/ERROR/STOPPED).
			var final dto.TransferNotification
			timeout := time.NewTimer(2 * time.Second)
			defer timeout.Stop()

			for {
				select {
				case n := <-ch:
					if n.Status == dto.COMPLETE || n.Status == dto.ERROR || n.Status == dto.STOPPED {
						final = n
						goto done
					}
				case <-timeout.C:
					t.Fatalf("timed out waiting for final notification")
				}
			}
		done:

			if final.Status != tt.wantStatus {
				t.Fatalf("final status=%s want %s (final=%+v)", final.Status, tt.wantStatus, final)
			}

			dest := filepath.Join(tt.cfg.DestinationFolder, tt.cfg.OutputFileName)
			if tt.cfg.OutputFileName == "" {
				// derived from URL
				u, _ := url.Parse(tt.cfg.URL)
				dest = filepath.Join(tt.cfg.DestinationFolder, filepath.Base(u.Path))
			}

			_, statErr := os.Stat(dest)
			if tt.wantFile && statErr != nil {
				t.Fatalf("expected file at %s, stat err: %v", dest, statErr)
			}
		})
	}
}

func TestDownloadFile_HTTP_BadStatus_Golden(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	t.Cleanup(ts.Close)

	cfg := config.DefaultNetSvcConfig()
	cfg.PreferCurlDownloads = false

	s := &NetSvc{
		cfg:            &cfg,
		relay:          &fakeRelay{},
		clients:        map[string]dto.NetClientInterface{},
		transferState:  *lockablemap.NewLockableMap[string, dto.TransferNotification](),
		listenersByURL: map[string][]chan dto.TransferNotification{},
	}

	dl := dto.DownloadFileConfig{
		URL:               ts.URL + "/missing.bin",
		DestinationFolder: t.TempDir(),
		OutputFileName:    "out.bin",
	}

	ch, _ := s.TransferListener(dl.URL)
	err := s.DownloadFile(context.Background(), &dl)
	if err == nil {
		t.Fatalf("expected error")
	}

	// Expect an ERROR notification at some point.
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case n, ok := <-ch:
			if !ok {
				t.Fatalf("listener closed without ERROR notification")
			}
			if n.Status == dto.ERROR {
				return
			}
		case <-timeout.C:
			t.Fatalf("timed out waiting for ERROR notification")
		}
	}
}

func TestDownloadFile_Curl_Golden(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not found on PATH; skipping curl downloader tests")
	}

	// Simple server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// big-ish body so curl has time to emit progress sometimes
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, strings.Repeat("x", 256*1024))
	}))
	t.Cleanup(ts.Close)

	cfg := config.DefaultNetSvcConfig()
	cfg.PreferCurlDownloads = true
	cfg.DownloadCallbackInterval = 50 * time.Millisecond

	s := &NetSvc{
		cfg:            &cfg,
		relay:          &fakeRelay{},
		clients:        map[string]dto.NetClientInterface{},
		transferState:  *lockablemap.NewLockableMap[string, dto.TransferNotification](),
		listenersByURL: map[string][]chan dto.TransferNotification{},
	}

	dl := dto.DownloadFileConfig{
		URL:               ts.URL + "/blob.bin",
		DestinationFolder: t.TempDir(),
		OutputFileName:    "blob.bin",
	}

	ch, _ := s.TransferListener(dl.URL)
	err := s.DownloadFile(context.Background(), &dl)
	if err != nil {
		t.Fatalf("DownloadFile err: %v", err)
	}

	// Wait for COMPLETE.
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case n, ok := <-ch:
			if !ok {
				// closed after COMPLETE, fine
				return
			}
			if n.Status == dto.COMPLETE {
				return
			}
		case <-timeout.C:
			t.Fatalf("timed out waiting for COMPLETE")
		}
	}
}
