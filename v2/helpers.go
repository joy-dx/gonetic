package gonetic

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/joy-dx/gonetic/v2/dto"
)

// TransferListener subscribes to future notifications for sourceURL.
//
// Subscribe before starting a download to receive its initial updates. The
// returned channel is buffered and is closed after a terminal notification,
// explicit URL-wide closure, or unsubscription.
//
// The unsubscribe function is safe to call repeatedly and after the channel
// has already been closed by the service.
//
// Subscriptions are keyed by URL, not by download invocation. Concurrent
// downloads of the same URL therefore share the same listener group.
func (s *NetSvc) TransferListener(
	sourceURL string,
) (<-chan dto.TransferNotification, func()) {
	s.muListeners.Lock()

	ch := make(chan dto.TransferNotification, 10)
	s.listenersByURL[sourceURL] = append(
		s.listenersByURL[sourceURL],
		ch,
	)

	s.muListeners.Unlock()

	var once sync.Once

	unsubscribe := func() {
		once.Do(func() {
			// Publication and every close operation use the same lock,
			// preventing a publisher from sending to a closed channel.
			s.muListeners.Lock()
			defer s.muListeners.Unlock()

			channels := s.listenersByURL[sourceURL]

			for i, existing := range channels {
				if existing != ch {
					continue
				}

				channels = append(channels[:i], channels[i+1:]...)

				if len(channels) == 0 {
					delete(s.listenersByURL, sourceURL)
				} else {
					s.listenersByURL[sourceURL] = channels
				}

				close(ch)
				return
			}

			// Terminal publication or explicit closure already removed
			// this channel. There is nothing left to close.
		})
	}

	return ch, unsubscribe
}

// publishTransferUpdate sends a snapshot to the current listeners for its URL.
//
// Slow subscribers do not block transfer progress: when a channel is full, its
// oldest buffered snapshot is discarded to make room for the new one.
//
// Terminal updates are queued before channels are closed, allowing subscribers
// to drain the final event. No historical update is replayed to later listeners.
func (s *NetSvc) publishTransferUpdate(
	update dto.TransferNotification,
) {
	s.muListeners.Lock()
	defer s.muListeners.Unlock()

	terminal := update.Status == dto.COMPLETE ||
		update.Status == dto.ERROR ||
		update.Status == dto.STOPPED

	for _, ch := range s.listenersByURL[update.Source] {
		select {
		case ch <- update:
		default:
			// All publishers hold muListeners, so no other sender can fill
			// the slot between discarding a snapshot and sending this one.
			// Subscribers may only make additional room by receiving.
			select {
			case <-ch:
			default:
			}
			ch <- update
		}

		if terminal {
			close(ch)
		}
	}

	if terminal {
		delete(s.listenersByURL, update.Source)
	}
}

// TransferListenerClose closes and removes every listener for sourceURL.
//
// This only ends notification subscriptions. It does not cancel the download
// and does not synthesize a terminal notification.
func (s *NetSvc) TransferListenerClose(sourceURL string) {
	s.muListeners.Lock()
	defer s.muListeners.Unlock()

	for _, ch := range s.listenersByURL[sourceURL] {
		close(ch)
	}

	delete(s.listenersByURL, sourceURL)
}

// progressReader is the read-side progress wrapper used by the legacy HTTP
// download helper.
//
// It measures bytes received from reader, unlike downloadProgressWriter, which
// measures bytes accepted by the output writer. Callbacks run synchronously
// during Read and include interval-based speed and ETA estimates.
//
// The refactored DownloadFile pipeline uses downloadProgressWriter instead.
type progressReader struct {
	ctx        context.Context
	reader     io.Reader
	total      int64
	readSoFar  int64
	lastReport time.Time
	lastBytes  int64
	interval   time.Duration
	startTime  time.Time
	onProgress func(
		downloaded, total int64,
		percent float64,
		speed float64,
		eta time.Duration,
	)
}

// Read checks cancellation, reads from the underlying reader, and periodically
// reports progress when bytes arrive.
//
// The context check occurs before the underlying Read. Interrupting a Read
// already blocked on I/O depends on the underlying reader honoring cancellation
// or being closed by its owner.
func (pr *progressReader) Read(p []byte) (int, error) {
	select {
	case <-pr.ctx.Done():
		return 0, pr.ctx.Err()
	default:
	}

	n, err := pr.reader.Read(p)
	if n > 0 {
		pr.readSoFar += int64(n)
		now := time.Now()
		if now.Sub(pr.lastReport) >= pr.interval {
			// Estimate current speed from the bytes received since the
			// previous report, rather than averaging the whole transfer.
			deltaBytes := pr.readSoFar - pr.lastBytes
			deltaTime := now.Sub(pr.lastReport).Seconds()
			speed := float64(deltaBytes) / deltaTime

			// Unknown totals have no meaningful percentage or ETA.
			var pct float64
			if pr.total > 0 {
				pct = float64(pr.readSoFar) / float64(pr.total) * 100
				if pct > 100 {
					pct = 100
				}
			}

			var eta time.Duration
			if pr.total > 0 && speed > 0 {
				remaining := float64(pr.total - pr.readSoFar)

				// The conversion before multiplication gives this legacy
				// estimate whole-second precision.
				eta = time.Duration(remaining/speed) * time.Second
			}

			pr.onProgress(pr.readSoFar, pr.total, pct, speed, eta)
			pr.lastReport = now
			pr.lastBytes = pr.readSoFar
		}
	}

	return n, err
}
