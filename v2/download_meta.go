package gonetic

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// downloadMetadata associates a working file with its source and validator.
//
// The current resume offset comes from the working file's size, not from this
// sidecar. A subsequent range request validates that prefix against the server.
type downloadMetadata struct {
	URL  string `json:"url"`
	ETag string `json:"etag"`
}

// isStrongETag reports whether value has the surrounding quotes expected for
// a strong ETag.
//
// A weak ETag starts with W/ and therefore does not pass this check. This is a
// lightweight shape check, not a complete parser for the ETag grammar.
func isStrongETag(value string) bool {
	return len(value) >= 2 &&
		strings.HasPrefix(value, `"`) &&
		strings.HasSuffix(value, `"`)
}

// readDownloadMetadata loads a resume sidecar.
//
// Reading and JSON decoding are separate from semantic validation: the caller
// checks the stored URL and ETag before deciding whether the file is resumable.
func readDownloadMetadata(path string) (downloadMetadata, error) {
	var metadata downloadMetadata

	data, err := os.ReadFile(path)
	if err != nil {
		return metadata, err
	}

	err = json.Unmarshal(data, &metadata)
	return metadata, err
}

// writeDownloadMetadata writes a complete replacement sidecar using a temporary
// file in the destination directory.
//
// The temporary file is encoded, synchronized, and closed before it is renamed
// into place. This avoids exposing a partially written JSON document through
// the target path. Rename replacement semantics depend on the platform.
//
// The containing directory is not synchronized, so this is not a complete
// power-loss durability guarantee.
func writeDownloadMetadata(
	path string,
	metadata downloadMetadata,
) error {
	// Keep the temporary file in the same directory to avoid a
	// cross-filesystem rename.
	file, err := os.CreateTemp(filepath.Dir(path), ".download-meta-*")
	if err != nil {
		return err
	}

	tempPath := file.Name()

	// Remove the temporary file on failure. After a successful rename, its
	// original pathname no longer exists and this removal is harmless.
	defer os.Remove(tempPath)

	if err := json.NewEncoder(file).Encode(metadata); err != nil {
		_ = file.Close()
		return err
	}

	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	return os.Rename(tempPath, path)
}

// removeDownloadMetadata removes a resume sidecar.
//
// A missing file is already in the desired state, so repeated removal is
// successful. Other filesystem errors are returned to the caller.
func removeDownloadMetadata(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
