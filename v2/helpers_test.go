package gonetic

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/joy-dx/gonetic/v2/dto"
)

type downloadTestWriter func([]byte) (int, error)

func (f downloadTestWriter) Write(p []byte) (int, error) {
	return f(p)
}

func TestDownloadProgressWriter(t *testing.T) {
	writeErr := errors.New("write failed")

	tests := []struct {
		name    string
		n       int
		err     error
		wantErr error
	}{
		{
			name: "full write",
			n:    4,
		},
		{
			name:    "short write",
			n:       2,
			wantErr: io.ErrShortWrite,
		},
		{
			name:    "partial write with error",
			n:       2,
			err:     writeErr,
			wantErr: writeErr,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			var reported int64

			writer := &downloadProgressWriter{
				dst: downloadTestWriter(
					func(p []byte) (int, error) {
						return tt.n, tt.err
					},
				),
				downloaded: 10,
				interval:   time.Second,
				lastReport: time.Now().Add(-2 * time.Second),
				onProgress: func(n int64) {
					reported = n
				},
			}

			n, err := writer.Write([]byte("data"))
			if n != tt.n || !errors.Is(err, tt.wantErr) {
				t.Fatalf(
					"Write = (%d, %v), want (%d, %v)",
					n,
					err,
					tt.n,
					tt.wantErr,
				)
			}

			want := int64(10 + tt.n)
			if writer.downloaded != want || reported != want {
				t.Fatalf(
					"downloaded = %d, reported = %d, want %d",
					writer.downloaded,
					reported,
					want,
				)
			}
		})
	}
}

func TestDownloadProgressWriter_ThrottlesReports(t *testing.T) {
	var destination bytes.Buffer
	calls := 0

	writer := &downloadProgressWriter{
		dst:        &destination,
		interval:   time.Hour,
		lastReport: time.Now(),
		onProgress: func(int64) {
			calls++
		},
	}

	if _, err := writer.Write([]byte("artifact")); err != nil {
		t.Fatal(err)
	}

	if calls != 0 {
		t.Fatalf("progress calls = %d, want 0", calls)
	}
	if writer.downloaded != int64(len("artifact")) {
		t.Fatalf("incorrect byte accounting: %d", writer.downloaded)
	}
}

func TestParseDownloadRange(t *testing.T) {
	tests := []struct {
		value string
		start int64
		end   int64
		total int64
		valid bool
	}{
		{
			value: "bytes 0-9/10",
			start: 0,
			end:   9,
			total: 10,
			valid: true,
		},
		{
			value: "bytes 5-9/10",
			start: 5,
			end:   9,
			total: 10,
			valid: true,
		},
		{value: ""},
		{value: "items 0-9/10"},
		{value: "bytes */10"},
		{value: "bytes 0-9/*"},
		{value: "bytes 9-5/10"},
		{value: "bytes 0-10/10"},
		{value: "bytes 0-9/0"},
		{value: "bytes x-9/10"},
		{value: "bytes 0-9/9223372036854775808"},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.value, func(t *testing.T) {
			start, end, total, err := parseDownloadRange(tt.value)
			if (err == nil) != tt.valid {
				t.Fatalf("error = %v, valid = %v", err, tt.valid)
			}

			if tt.valid &&
				(start != tt.start ||
					end != tt.end ||
					total != tt.total) {
				t.Fatalf(
					"range = (%d, %d, %d), want (%d, %d, %d)",
					start,
					end,
					total,
					tt.start,
					tt.end,
					tt.total,
				)
			}
		})
	}
}

func TestDownloadMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.resume.json")

	want := downloadMetadata{
		URL:  "https://example.com/artifact.bin",
		ETag: `"version-1"`,
	}

	if err := writeDownloadMetadata(path, want); err != nil {
		t.Fatal(err)
	}

	got, err := readDownloadMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("metadata = %+v, want %+v", got, want)
	}

	// Updating existing metadata must replace its contents.
	want.ETag = `"version-2"`
	if err := writeDownloadMetadata(path, want); err != nil {
		t.Fatal(err)
	}

	got, err = readDownloadMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("updated metadata = %+v, want %+v", got, want)
	}

	if err := removeDownloadMetadata(path); err != nil {
		t.Fatal(err)
	}
	if err := removeDownloadMetadata(path); err != nil {
		t.Fatalf("removing missing metadata: %v", err)
	}

	requireDownloadPathAbsent(t, path)
}

func TestIsStrongETag(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: `"version-1"`, want: true},
		{value: `""`, want: true},
		{value: `W/"version-1"`},
		{value: "version-1"},
		{value: ""},
	}

	for _, tt := range tests {
		if got := isStrongETag(tt.value); got != tt.want {
			t.Errorf(
				"isStrongETag(%q) = %v, want %v",
				tt.value,
				got,
				tt.want,
			)
		}
	}
}

func TestTransferListener_TerminalSurvivesFullBuffer(t *testing.T) {
	s := newDownloadTestService(false)
	const source = "https://example.com/artifact.bin"

	ch, unsubscribe := s.TransferListener(source)
	defer unsubscribe()

	// Fill and overflow the listener buffer without consuming it.
	for i := 0; i < 25; i++ {
		s.publishTransferUpdate(dto.TransferNotification{
			Source:     source,
			Status:     dto.IN_PROGRESS,
			Downloaded: int64(i),
		})
	}

	s.publishTransferUpdate(dto.TransferNotification{
		Source: source,
		Status: dto.COMPLETE,
	})

	requireDownloadTerminal(t, ch, dto.COMPLETE)

	// Both calls must be safe after terminal publication closed it.
	unsubscribe()
	s.TransferListenerClose(source)
}

func TestTransferListener_Unsubscribe(t *testing.T) {
	s := newDownloadTestService(false)
	const source = "https://example.com/artifact.bin"

	ch, unsubscribe := s.TransferListener(source)
	unsubscribe()
	unsubscribe()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("unsubscribed listener remains open")
		}
	case <-time.After(time.Second):
		t.Fatal("unsubscribe did not close listener")
	}

	// Publishing after unsubscribe must not send to a closed channel.
	s.publishTransferUpdate(dto.TransferNotification{
		Source: source,
		Status: dto.COMPLETE,
	})
}
