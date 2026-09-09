package gonetic

import (
	"context"
	"io"
	"time"

	"github.com/joy-dx/gonetic/dto"
	"github.com/joy-dx/gonetic/relays"
)

// publishTransferUpdate is the unified notification function
func (s *NetSvc) publishTransferUpdate(state dto.TransferNotification) {
	s.transferState.Set(state.Destination, state)

	s.muListeners.Lock()
	listeners := append([]chan dto.TransferNotification(nil), s.listenersByURL[state.Source]...)
	s.muListeners.Unlock()

	isTerminal := state.Status == dto.COMPLETE ||
		state.Status == dto.ERROR ||
		state.Status == dto.STOPPED

	for _, ch := range listeners {
		if isTerminal {
			// Ensure terminal events are delivered.
			// Avoid deadlock: do NOT hold muListeners while sending.
			select {
			case ch <- state:
			default:
				// Buffer full: fall back to blocking send in a goroutine.
				// This guarantees delivery without stalling the publisher.
				go func(c chan dto.TransferNotification, n dto.TransferNotification) {
					// Best effort: if unsub closed the channel, recover.
					defer func() { _ = recover() }()
					c <- n
				}(ch, state)
			}
		} else {
			// Progress updates can be dropped
			select {
			case ch <- state:
			default:
			}
		}
	}

	if s.relay != nil {
		s.relay.Info(relays.RlyNetDownload{
			Source:      state.Source,
			Destination: state.Destination,
			Status:      state.Status,
			Percentage:  state.Percentage,
			Msg:         state.Message,
		})
	}
}

type progressReader struct {
	ctx        context.Context
	reader     io.Reader
	total      int64
	readSoFar  int64
	lastReport time.Time
	lastBytes  int64
	interval   time.Duration
	startTime  time.Time
	onProgress func(downloaded, total int64, percent float64, speed float64, eta time.Duration)
}

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
			deltaBytes := pr.readSoFar - pr.lastBytes
			deltaTime := now.Sub(pr.lastReport).Seconds()
			speed := float64(deltaBytes) / deltaTime // bytes/sec

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
				eta = time.Duration(remaining/speed) * time.Second
			}

			pr.onProgress(pr.readSoFar, pr.total, pct, speed, eta)
			pr.lastReport = now
			pr.lastBytes = pr.readSoFar
		}
	}

	return n, err
}
