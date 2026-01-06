package gonetic

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/joy-dx/gonetic/dto"
	"github.com/joy-dx/gonetic/relays"
	"github.com/joy-dx/gonetic/utils"
)

// downloadFileWithHTTP streams via net/http with progress
func (s *NetSvc) downloadFileWithHTTP(
	ctx context.Context,
	cfg *dto.DownloadFileConfig,
	destination string,
) error {
	s.relay.Debug(relays.RlyNetDownload{
		Source:      cfg.URL,
		Destination: destination,
		Msg:         "Downloading via net/http",
		Status:      dto.IN_PROGRESS,
	})

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		s.publishTransferUpdate(dto.TransferNotification{
			Source:      cfg.URL,
			Destination: destination,
			Status:      dto.ERROR,
			Message:     err.Error(),
		})
		return fmt.Errorf("could not create destination folder %q: %w", destination, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.URL, nil)
	if err != nil {
		s.publishTransferUpdate(dto.TransferNotification{
			Source:      cfg.URL,
			Destination: destination,
			Status:      dto.ERROR,
			Message:     err.Error(),
		})
		return fmt.Errorf("failed to build request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// If ctx was canceled, prefer STOPPED (so listeners close consistently)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			s.publishTransferUpdate(dto.TransferNotification{
				Source:      cfg.URL,
				Destination: destination,
				Status:      dto.STOPPED,
				Message:     err.Error(),
			})
			return err
		}

		s.publishTransferUpdate(dto.TransferNotification{
			Source:      cfg.URL,
			Destination: destination,
			Status:      dto.ERROR,
			Message:     err.Error(),
		})
		return fmt.Errorf("failed to start download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		s.publishTransferUpdate(dto.TransferNotification{
			Source:      cfg.URL,
			Destination: destination,
			Status:      dto.ERROR,
			Message:     fmt.Sprintf("bad HTTP status: %s", resp.Status),
		})
		return fmt.Errorf("bad HTTP status: %s", resp.Status)
	}

	out, err := os.Create(destination)
	if err != nil {
		s.publishTransferUpdate(dto.TransferNotification{
			Source:      cfg.URL,
			Destination: destination,
			Status:      dto.ERROR,
			Message:     err.Error(),
		})
		return fmt.Errorf("could not create output file %q: %w", destination, err)
	}
	defer out.Close()

	total := resp.ContentLength
	if total <= 0 {
		s.relay.Warn(relays.RlyNetDownload{Source: cfg.URL, Msg: "unknown file size"})
	}

	interval := s.cfg.DownloadCallbackInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}

	report := func(downloaded, total int64, percent float64, speed float64, eta time.Duration) {
		s.publishTransferUpdate(dto.TransferNotification{
			Source:      cfg.URL,
			Destination: destination,
			Status:      dto.IN_PROGRESS,
			Downloaded:  downloaded,
			TotalSize:   total,
			Percentage:  percent,
		})
	}

	pr := &progressReader{
		ctx:        ctx,
		reader:     resp.Body,
		total:      total,
		interval:   interval,
		lastReport: time.Now(),
		startTime:  time.Now(),
		onProgress: report,
	}

	buf := make([]byte, 64*1024)
	_, err = io.CopyBuffer(out, pr, buf)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			s.publishTransferUpdate(dto.TransferNotification{
				Source:      cfg.URL,
				Destination: destination,
				Status:      dto.STOPPED,
			})
			return ctx.Err()
		}

		s.publishTransferUpdate(dto.TransferNotification{
			Source:      cfg.URL,
			Destination: destination,
			Status:      dto.ERROR,
			Message:     err.Error(),
		})
		return fmt.Errorf("file transfer failed for %s: %w", cfg.URL, err)
	}

	if cfg.Checksum != "" {
		checkErr := utils.Sha256SumVerify(destination, cfg.Checksum)
		if checkErr != nil {
			s.publishTransferUpdate(dto.TransferNotification{
				Source:      cfg.URL,
				Destination: destination,
				Status:      dto.ERROR,
				Percentage:  100,
				Message:     "failed to verify checksum",
			})
			return fmt.Errorf("checksum verification failed: %w", checkErr)
		}
	}

	s.publishTransferUpdate(dto.TransferNotification{
		Source:      cfg.URL,
		Destination: destination,
		Status:      dto.COMPLETE,
		Downloaded:  total,
		TotalSize:   total,
		Percentage:  100,
		Message:     "download complete",
	})
	return nil
}

// =====================================================================
// Curl Downloader Implementation
// =====================================================================

func (s *NetSvc) downloadFileWithCurl(
	ctx context.Context,
	cfg *dto.DownloadFileConfig,
	destination string,
) error {
	s.relay.Debug(relays.RlyNetDownload{
		Source:      cfg.URL,
		Destination: destination,
		Msg:         "Downloading via curl",
		Status:      dto.IN_PROGRESS,
	})

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("could not create destination folder %q: %w", destination, err)
	}

	curlCmd := exec.CommandContext(ctx, "curl", "-L", "--progress-bar", "-o", destination, cfg.URL)
	stdoutBuf := new(bytes.Buffer)
	stderrBuf := new(bytes.Buffer)
	curlCmd.Stdout = stdoutBuf
	curlCmd.Stderr = stderrBuf

	if err := curlCmd.Start(); err != nil {
		return fmt.Errorf("failed to start curl: %w", err)
	}

	interval := s.cfg.DownloadCallbackInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	done := make(chan error, 1)

	go func() {
		err := curlCmd.Wait()
		select {
		case done <- err:
		case <-ctx.Done():
		}
	}()

	for {
		select {
		case <-ticker.C:
			if msg, newContent := utils.Flush(stderrBuf); newContent && len(msg) >= 6 {
				if parsed, err := utils.ParsePercentage(msg[len(msg)-6:]); err == nil {
					if parsed > 100 {
						parsed = 100
					}
					s.publishTransferUpdate(dto.TransferNotification{
						Source:      cfg.URL,
						Destination: destination,
						Status:      dto.IN_PROGRESS,
						Percentage:  parsed,
					})
				}
			}
		case <-ctx.Done():
			if curlCmd.Process != nil {
				_ = curlCmd.Process.Kill()
			}
			s.publishTransferUpdate(dto.TransferNotification{
				Source:      cfg.URL,
				Destination: destination,
				Status:      dto.STOPPED,
			})
			return ctx.Err()

		case err := <-done:
			ticker.Stop()
			if err != nil {
				s.publishTransferUpdate(dto.TransferNotification{
					Source:      cfg.URL,
					Destination: destination,
					Status:      dto.ERROR,
					Message:     err.Error(),
				})
				return fmt.Errorf("curl download failed: %w", err)
			}

			if cfg.Checksum != "" {
				checkErr := utils.Sha256SumVerify(destination, cfg.Checksum)
				if checkErr != nil {
					s.publishTransferUpdate(dto.TransferNotification{
						Source:      cfg.URL,
						Destination: destination,
						Status:      dto.ERROR,
						Percentage:  100,
						Message:     "failed to verify checksum",
					})
					return fmt.Errorf("checksum verification failed: %w", checkErr)
				}
			}

			s.publishTransferUpdate(dto.TransferNotification{
				Source:      cfg.URL,
				Destination: destination,
				Status:      dto.COMPLETE,
				Percentage:  100,
				Message:     "download complete",
			})
			return nil
		}
	}
}

func (s *NetSvc) DownloadFile(ctx context.Context, cfg *dto.DownloadFileConfig) error {

	if cfg.OutputFileName == "" {
		// Try and get the filename from the URL and use the destination folder instead
		filename, err := utils.FilenameFromUrl(cfg.URL)
		if err != nil {
			return err
		}
		cfg.OutputFileName = filename
	}

	destination := filepath.Join(cfg.DestinationFolder, cfg.OutputFileName)

	s.relay.Info(relays.RlyNetDownload{
		Source:      cfg.URL,
		Destination: destination,
		Status:      dto.IN_PROGRESS,
		Percentage:  0,
		Msg:         fmt.Sprintf("starting download: %s", cfg.URL),
	})

	if s.cfg.PreferCurlDownloads {
		return s.downloadFileWithCurl(ctx, cfg, destination)
	}

	return s.downloadFileWithHTTP(ctx, cfg, destination)
}
