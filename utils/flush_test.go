package utils

import (
	"bytes"
	"testing"
)

func TestFlush_Golden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		initial  string
		want     string
		wantOk   bool
		wantLeft string
	}{
		{
			name:     "empty buffer",
			initial:  "",
			want:     "",
			wantOk:   false,
			wantLeft: "",
		},
		{
			name:     "non-empty flushes and resets",
			initial:  "hello",
			want:     "hello",
			wantOk:   true,
			wantLeft: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			buf := bytes.NewBufferString(tt.initial)
			got, ok := Flush(buf)

			if ok != tt.wantOk {
				t.Fatalf("ok=%v want %v", ok, tt.wantOk)
			}
			if got != tt.want {
				t.Fatalf("got=%q want %q", got, tt.want)
			}
			if buf.String() != tt.wantLeft {
				t.Fatalf("buffer left=%q want %q", buf.String(), tt.wantLeft)
			}
		})
	}
}
