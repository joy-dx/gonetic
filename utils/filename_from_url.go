package utils

import (
	"net/url"
	"path/filepath"
)

// FilenameFromUrl takes input as a escaped url & outputs filename from it (unescaped - normal one)
func FilenameFromUrl(inputUrl string) (string, error) {
	u, err := url.Parse(inputUrl)
	if err != nil {
		return "", err
	}
	x, _ := url.QueryUnescape(u.EscapedPath())
	return filepath.Base(x), nil
}
