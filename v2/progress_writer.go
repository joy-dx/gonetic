package gonetic

import (
	"io"
	"time"
)

// downloadProgressWriter measures bytes accepted by a destination writer.
//
// downloaded may start at a nonzero resume offset so callbacks describe the
// complete artifact rather than just the current response body.
//
// Callbacks run synchronously from Write and are rate-limited by write activity;
// there is no background timer. The caller publishes initial and terminal
// notifications separately. This type is intended for a single copy operation,
// not concurrent writes.
type downloadProgressWriter struct {
	dst        io.Writer
	downloaded int64
	interval   time.Duration
	lastReport time.Time
	onProgress func(int64)
}

// Write forwards p and adds only the number of bytes accepted by dst.
//
// A short write without an error is converted to io.ErrShortWrite so callers
// such as io.CopyBuffer cannot mistake an incomplete write for success.
func (w *downloadProgressWriter) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	w.downloaded += int64(n)

	if n < len(p) && err == nil {
		err = io.ErrShortWrite
	}

	// Reporting follows the write, so even a partially successful write
	// contributes its accepted bytes. A stalled transfer produces no timed
	// callbacks until another Write occurs.
	if time.Since(w.lastReport) >= w.interval {
		w.lastReport = time.Now()
		w.onProgress(w.downloaded)
	}

	return n, err
}
