package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

// sha256SumFile computes the SHA-256 checksum for a given file path.
// It returns the checksum as a hex-encoded string.
func Sha256SumFile(path string) (string, error) {

	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to hash file: %w", err)
	}

	sum := hash.Sum(nil)
	return hex.EncodeToString(sum), nil
}

func Sha256SumVerify(path string, checksum string) error {
	targetHash, hashErr := Sha256SumFile(path)
	if hashErr != nil {
		return hashErr
	}

	if checksum != targetHash {
		return errors.New("invalid checksum")
	}
	return nil
}
