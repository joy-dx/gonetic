package utils

import "testing"

func TestMapToHeader_Golden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   map[string]string
		want map[string]string
	}{
		{
			name: "empty",
			in:   map[string]string{},
			want: map[string]string{},
		},
		{
			name: "sets values",
			in:   map[string]string{"A": "1", "B": "x"},
			want: map[string]string{"A": "1", "B": "x"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := MapToHeader(tt.in)
			for k, wantV := range tt.want {
				if got := h.Get(k); got != wantV {
					t.Fatalf("header %s=%q want %q", k, got, wantV)
				}
			}
		})
	}
}
