package utils

import "testing"

func TestFilenameFromUrl_Golden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{
			name: "simple path",
			in:   "https://example.com/a/b/c.txt",
			want: "c.txt",
		},
		{
			name: "escaped path",
			in:   "https://example.com/a/b/hello%20world.zip",
			want: "hello world.zip",
		},
		{
			name: "trailing slash yields base slash (.) semantics -> base of path",
			in:   "https://example.com/a/b/",
			want: "b",
		},
		{
			name:    "invalid url errors",
			in:      "http://[::1", // invalid
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := FilenameFromUrl(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("got=%q want %q", got, tt.want)
			}
		})
	}
}
