package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSha256SumFileAndVerify_Golden(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tests := []struct {
		name      string
		path      string
		checksum  string
		wantSum   string
		wantErr   bool
		verifyErr bool
	}{
		{
			name:    "sum abc",
			path:    path,
			wantSum: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		},
		{
			name:      "verify ok",
			path:      path,
			checksum:  "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
			verifyErr: false,
		},
		{
			name:      "verify invalid",
			path:      path,
			checksum:  "deadbeef",
			verifyErr: true,
		},
		{
			name:    "sum missing file errors",
			path:    filepath.Join(dir, "missing.txt"),
			wantErr: true,
		},
		{
			name:      "verify missing file errors",
			path:      filepath.Join(dir, "missing2.txt"),
			checksum:  "whatever",
			verifyErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.wantSum != "" || tt.wantErr {
				got, err := Sha256SumFile(tt.path)
				if (err != nil) != tt.wantErr {
					t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
				}
				if tt.wantSum != "" && got != tt.wantSum {
					t.Fatalf("sum=%s want %s", got, tt.wantSum)
				}
			}

			if tt.checksum != "" || tt.verifyErr {
				err := Sha256SumVerify(tt.path, tt.checksum)
				if (err != nil) != tt.verifyErr {
					t.Fatalf("verify err=%v wantErr=%v", err, tt.verifyErr)
				}
			}
		})
	}
}
