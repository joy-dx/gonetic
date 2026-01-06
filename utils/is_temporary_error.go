package utils

import "errors"

func IsTemporaryErr(err error) bool {
	// You could enhance this to check for net.Error timeouts etc.
	var netErr interface{ Temporary() bool }
	if errors.As(err, &netErr) {
		return netErr.Temporary()
	}
	// consider all network-level issues as transient
	return true
}
