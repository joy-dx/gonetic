package utils

import (
	"errors"
	"testing"
)

type tempErr struct{ temp bool }

func (e tempErr) Error() string { return "temp" }
func (e tempErr) Temporary() bool {
	return e.temp
}

func TestIsTemporaryErr_Golden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "has Temporary true",
			err:  tempErr{temp: true},
			want: true,
		},
		{
			name: "has Temporary false",
			err:  tempErr{temp: false},
			want: false,
		},
		{
			name: "wrapped Temporary false",
			err:  errors.New("outer: " + tempErr{temp: false}.Error()),
			// NOTE: errors.As will not match here because we didn't wrap tempErr.
			// This case documents current behavior: falls through -> true.
			want: true,
		},
		{
			name: "nil error treated as transient (current behavior)",
			err:  nil,
			want: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsTemporaryErr(tt.err); got != tt.want {
				t.Fatalf("got=%v want %v", got, tt.want)
			}
		})
	}
}
