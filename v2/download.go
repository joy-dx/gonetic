package gonetic

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joy-dx/gonetic/v2/client/httpclient"
	"github.com/joy-dx/gonetic/v2/dto"
	"github.com/joy-dx/gonetic/v2/relays"
	"github.com/joy-dx/gonetic/v2/utils"
)

// downloadFileWithHTTP is the legacy net/http-only download implementation.
//
// It writes directly to the destination and measures progress as response
// bytes are read. The refactored DownloadFile method below does not call this
// helper; it uses a shared, transport-independent pipeline instead.
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
		return fmt.Errorf(
			"could not create destination folder %q: %w",
			destination,
			err,
		)
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
		// Treat cancellation and timeout as a stopped transfer rather than
		// an ordinary request failure.
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
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

	// This legacy path truncates the final destination immediately. It does
	// not provide the part-file commit behavior of DownloadFile.
	out, err := os.Create(destination)
	if err != nil {
		s.publishTransferUpdate(dto.TransferNotification{
			Source:      cfg.URL,
			Destination: destination,
			Status:      dto.ERROR,
			Message:     err.Error(),
		})
		return fmt.Errorf(
			"could not create output file %q: %w",
			destination,
			err,
		)
	}
	defer out.Close()

	total := resp.ContentLength
	if total <= 0 {
		s.relay.Warn(relays.RlyNetDownload{
			Source: cfg.URL,
			Msg:    "unknown file size",
		})
	}

	interval := s.cfg.DownloadCallbackInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}

	// progressReader also calculates speed and ETA, but this notification
	// currently publishes only byte counts and percentage.
	report := func(
		downloaded, total int64,
		percent float64,
		speed float64,
		eta time.Duration,
	) {
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
		return fmt.Errorf(
			"file transfer failed for %s: %w",
			cfg.URL,
			err,
		)
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

// downloadFileWithCurl is the legacy standalone curl downloader.
//
// Curl writes the destination itself, and this helper parses curl's progress
// output. The refactored DownloadFile method does not call this helper; it uses
// CurlTransport to feed the same pipeline used by native HTTP downloads.
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
		return fmt.Errorf(
			"could not create destination folder %q: %w",
			destination,
			err,
		)
	}

	curlCmd := exec.CommandContext(
		ctx,
		"curl",
		"-L",
		"--progress-bar",
		"-o",
		destination,
		cfg.URL,
	)
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

	// Wait independently so the loop can observe both progress ticks and
	// context cancellation while curl is running.
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
			// This older path derives percentage from curl's textual output,
			// rather than counting bytes accepted by the output file.
			if msg, newContent := utils.Flush(stderrBuf); newContent &&
				len(msg) >= 6 {
				if parsed, err := utils.ParsePercentage(
					msg[len(msg)-6:],
				); err == nil {
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
					return fmt.Errorf(
						"checksum verification failed: %w",
						checkErr,
					)
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

// DownloadFile downloads an HTTP/HTTPS resource and returns its destination.
//
// All agents share the same file-writing pipeline: select a response, validate
// any resumed range, stream bytes with progress accounting, synchronize and
// close the file, verify an optional checksum, and promote an optional part
// file to the final destination.
//
// Resume requires matching URL metadata and a strong ETag. Failed transfers
// retain their working file so a later invocation can attempt to resume.
// This method does not automatically retry failed transfers.
//
// An explicit cfg.Agent takes precedence over PreferCurlDownloads. Without an
// agent, the service uses either http.DefaultClient or a curl-backed client.
//
// Once a destination has been prepared, the method publishes a terminal
// notification on return. Notifications identify the final destination even
// when the actual writes go to a .part file.
func (s *NetSvc) DownloadFile(
	ctx context.Context,
	cfg *dto.DownloadFileConfig,
) (destination string, err error) {
	if cfg == nil {
		return "", errors.New("nil download configuration")
	}

	// Clone mutable request headers as well as the configuration structure.
	// Preparing this download must not modify configuration held by callers.
	local := *cfg
	local.Headers = cfg.Headers.Clone()
	cfg = &local

	if cfg.OutputFileName == "" {
		cfg.OutputFileName, err = utils.FilenameFromUrl(cfg.URL)
		if err != nil {
			return "", err
		}
	}

	destination = filepath.Join(
		cfg.DestinationFolder,
		cfg.OutputFileName,
	)

	// Keep the public destination separate from the working path. With part
	// files enabled, the final destination is not replaced during streaming.
	workPath := destination
	if cfg.UsePartFile {
		workPath += ".part"
	}

	var downloaded int64
	total := int64(-1)

	// Centralize notification construction so progress and terminal events
	// report the same destination, byte counts, and percentage semantics.
	publish := func(status dto.TransferStatus, message string) {
		percentage := float64(0)
		if total > 0 {
			percentage = math.Min(
				100,
				float64(downloaded)/float64(total)*100,
			)
		}
		if status == dto.COMPLETE {
			percentage = 100
		}

		s.publishTransferUpdate(dto.TransferNotification{
			Source:      cfg.URL,
			Destination: destination,
			Status:      status,
			Downloaded:  downloaded,
			TotalSize:   total,
			Percentage:  percentage,
			Message:     message,
		})
	}

	// Named return values let this single exit handler classify the result.
	// Check the original context because a transport may return a wrapped
	// subprocess or I/O error when cancellation caused the failure.
	//
	// This defer is registered before resource-cleanup defers, so those run
	// first. Validation failures above this point do not publish an event.
	defer func() {
		switch {
		case err == nil:
			publish(dto.COMPLETE, "download complete")
		case ctx.Err() != nil:
			publish(dto.STOPPED, ctx.Err().Error())
		default:
			publish(dto.ERROR, err.Error())
		}
	}()

	// Claim both paths: one invocation may use another invocation's final
	// destination as its working path. Claims apply only within this NetSvc.
	release, err := s.claimDownloadPaths(destination, workPath)
	if err != nil {
		return destination, err
	}
	defer release()

	if err := ctx.Err(); err != nil {
		return destination, err
	}

	if err := os.MkdirAll(filepath.Dir(workPath), 0o755); err != nil {
		return destination, fmt.Errorf(
			"create destination directory: %w",
			err,
		)
	}

	// A caller-supplied agent owns client configuration such as proxy,
	// authentication, timeouts, and redirect policy. Otherwise choose only
	// the transport; the rest of the download workflow remains identical.
	agent := cfg.Agent
	if agent == nil {
		if s.cfg.PreferCurlDownloads {
			agent = httpclient.DownloadHTTPClient(&http.Client{
				Transport: CurlTransport{},
			})
		} else {
			agent = httpclient.DownloadHTTPClient(http.DefaultClient)
		}
	}

	// Do not truncate yet. Existing bytes may be reusable, and the response
	// must be validated before deciding whether to append or restart.
	out, err := os.OpenFile(workPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return destination, fmt.Errorf("open download file: %w", err)
	}
	defer out.Close()

	info, err := out.Stat()
	if err != nil {
		return destination, fmt.Errorf("stat download file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return destination, errors.New("download target is not a regular file")
	}

	metaPath := workPath + ".resume.json"
	var metadata downloadMetadata
	var offset int64

	// File size alone is not sufficient evidence for resuming. Require a
	// saved validator associated with the exact configured URL.
	//
	// Missing, malformed, mismatched, or weak-validator metadata is treated
	// as a cache miss: continue with a full request rather than failing.
	if cfg.Resume && info.Size() > 0 {
		loaded, loadErr := readDownloadMetadata(metaPath)
		if loadErr == nil &&
			loaded.URL == cfg.URL &&
			isStrongETag(loaded.ETag) {
			metadata = loaded
			offset = info.Size()
		}
	}

	// The response helper may accept the requested offset or reset it to
	// zero when the server requires a fresh download.
	resp, offset, total, err := openDownloadResponse(
		ctx,
		agent,
		cfg,
		offset,
		metadata.ETag,
	)
	if err != nil {
		return destination, err
	}
	defer resp.Body.Close()

	if offset == 0 {
		// Invalidate the previous validator before changing the bytes it
		// described. A failed restart must not leave old metadata attached
		// to newly written content.
		if err := removeDownloadMetadata(metaPath); err != nil {
			return destination, err
		}
		if err := out.Truncate(0); err != nil {
			return destination, fmt.Errorf("truncate download: %w", err)
		}
	}

	if _, err := out.Seek(offset, io.SeekStart); err != nil {
		return destination, fmt.Errorf("seek download: %w", err)
	}

	// Save a usable validator before writing new response bytes. If this
	// transfer is interrupted, the next invocation can validate its prefix.
	// Without a strong ETag, a fresh transfer remains non-resumable.
	if cfg.Resume && isStrongETag(resp.Header.Get("ETag")) {
		metadata = downloadMetadata{
			URL:  cfg.URL,
			ETag: resp.Header.Get("ETag"),
		}
		if err := writeDownloadMetadata(metaPath, metadata); err != nil {
			return destination, fmt.Errorf(
				"write resume metadata: %w",
				err,
			)
		}
	}

	// A resumed prefix contributes to both byte counts and percentages.
	downloaded = offset
	publish(dto.IN_PROGRESS, "downloading")

	interval := s.cfg.DownloadCallbackInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}

	// Count bytes on the writer side so progress reflects bytes accepted by
	// the file, rather than bytes merely read from the network. Persistence
	// is checked separately by Sync after streaming completes.
	writer := &downloadProgressWriter{
		dst:        out,
		downloaded: offset,
		interval:   interval,
		lastReport: time.Now(),
		onProgress: func(value int64) {
			downloaded = value
			publish(dto.IN_PROGRESS, "downloading")
		},
	}

	n, copyErr := io.CopyBuffer(
		writer,
		resp.Body,
		make([]byte, 64*1024),
	)

	// Capture the exact final count even if no periodic callback was due.
	downloaded = offset + n

	// Close explicitly so a transport cleanup failure can prevent success.
	// The deferred close still covers earlier returns.
	closeErr := resp.Body.Close()

	if copyErr != nil {
		return destination, fmt.Errorf("download body: %w", copyErr)
	}
	if closeErr != nil {
		return destination, fmt.Errorf(
			"close download response: %w",
			closeErr,
		)
	}

	// Validate the complete artifact size, including any resumed prefix.
	// For a response without a known length, use the observed final count.
	if total >= 0 && downloaded != total {
		return destination, fmt.Errorf(
			"incomplete download: wrote %d bytes, expected %d",
			downloaded,
			total,
		)
	}
	if total < 0 {
		total = downloaded
	}

	// Flush file contents and check close errors before verification or
	// promotion. The deferred close remains a fallback for earlier exits.
	if err := out.Sync(); err != nil {
		return destination, fmt.Errorf("sync download: %w", err)
	}
	if err := out.Close(); err != nil {
		return destination, fmt.Errorf("close download file: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return destination, err
	}

	if cfg.Checksum != "" {
		if err := utils.Sha256SumVerify(workPath, cfg.Checksum); err != nil {
			// Keep the file for inspection, but invalidate its resume
			// metadata so known-invalid content is not reused next time.
			_ = removeDownloadMetadata(metaPath)
			return destination, fmt.Errorf(
				"checksum verification failed: %w",
				err,
			)
		}
	}

	// Verification may take time. Recheck cancellation before committing
	// the part file to the final destination.
	if err := ctx.Err(); err != nil {
		return destination, err
	}

	if cfg.UsePartFile {
		if err := os.Rename(workPath, destination); err != nil {
			return destination, fmt.Errorf(
				"commit completed download: %w",
				err,
			)
		}
	}

	// The artifact is complete. Metadata removal is best-effort so cleanup
	// failure does not turn an otherwise successful download into an error.
	_ = removeDownloadMetadata(metaPath)

	return destination, nil
}

// openDownloadResponse obtains a response suitable for writing to the working
// file and returns the accepted starting offset and complete artifact size.
//
// A valid 206 response preserves the existing prefix. A 200 response supplies
// a full replacement. An invalid resumed response or a 416 response triggers
// one retry without range headers.
//
// This helper validates response metadata but does not modify local files.
// The caller truncates or seeks only after an acceptable response is returned.
func openDownloadResponse(
	ctx context.Context,
	agent dto.DownloadAgent,
	cfg *dto.DownloadFileConfig,
	offset int64,
	etag string,
) (*http.Response, int64, int64, error) {
	for {
		// Build a new request for each attempt so a full-download fallback
		// cannot accidentally retain range headers from the resume attempt.
		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			cfg.URL,
			nil,
		)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("build download request: %w", err)
		}

		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return nil, 0, 0, errors.New(
				"downloads require an HTTP or HTTPS URL",
			)
		}

		req.Header = cfg.Headers.Clone()
		if req.Header == nil {
			req.Header = make(http.Header)
		}

		// Explicit configuration wins over a supplied header. Otherwise
		// preserve the caller's header or apply the download default.
		if cfg.UserAgent != "" {
			req.Header.Set("User-Agent", cfg.UserAgent)
		} else if req.Header.Get("User-Agent") == "" {
			req.Header.Set("User-Agent", "NetSvc/1.0")
		}

		// Range offsets must describe the same representation stored on disk.
		// Transparent decompression would make those offsets unreliable.
		req.Header.Set("Accept-Encoding", "identity")

		// Range selection belongs to the download engine, not caller headers.
		req.Header.Del("Range")
		req.Header.Del("If-Range")

		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
			req.Header.Set("If-Range", etag)
		}

		resp, err := agent.Do(req)
		if err != nil {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			return nil, 0, 0, fmt.Errorf("start download: %w", err)
		}
		if resp == nil || resp.Body == nil {
			return nil, 0, 0, errors.New(
				"download agent returned an invalid response",
			)
		}

		// Enforce representation consistency even for custom agents, which
		// may otherwise decode content despite the requested encoding.
		encoding := resp.Header.Get("Content-Encoding")
		if resp.Uncompressed ||
			(encoding != "" &&
				!strings.EqualFold(encoding, "identity")) {
			_ = resp.Body.Close()
			return nil, 0, 0, errors.New(
				"download agent/server returned an encoded representation",
			)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			// The server ignored Range, If-Range did not match, or this was
			// already a full request. Use this response without another
			// network round trip; the caller will discard the old prefix.
			return resp, 0, resp.ContentLength, nil

		case http.StatusPartialContent:
			start, end, total, rangeErr := parseDownloadRange(
				resp.Header.Get("Content-Range"),
			)

			// Accept only the requested suffix through the artifact's end,
			// with exactly the validator used to authorize the resume.
			valid := offset > 0 &&
				rangeErr == nil &&
				start == offset &&
				end == total-1 &&
				resp.Header.Get("ETag") == etag

			if valid && resp.ContentLength >= 0 {
				valid = resp.ContentLength == end-start+1
			}

			if valid {
				return resp, offset, total, nil
			}

			_ = resp.Body.Close()

			if offset == 0 {
				return nil, 0, 0, errors.New(
					"unexpected or invalid partial response",
				)
			}

			// Fall back once. If the full request also returns an invalid
			// partial response, the offset-zero branch above returns an error.
			offset = 0
			etag = ""

		case http.StatusRequestedRangeNotSatisfiable:
			_ = resp.Body.Close()

			if offset == 0 {
				return nil, 0, 0, errors.New(
					"server rejected a non-range download",
				)
			}

			// An unsatisfiable range may mean the local file is complete,
			// but size alone does not establish correctness. Restart.
			offset = 0
			etag = ""

		default:
			status := resp.Status
			_ = resp.Body.Close()
			return nil, 0, 0, fmt.Errorf(
				"unexpected download HTTP status: %s",
				status,
			)
		}
	}
}

// parseDownloadRange parses a concrete Content-Range of the form
// "bytes start-end/total".
//
// The end offset is inclusive. Unknown totals, unsatisfied-range forms, and
// bounds outside the complete representation are rejected.
func parseDownloadRange(value string) (
	start int64,
	end int64,
	total int64,
	err error,
) {
	invalid := errors.New("invalid Content-Range")

	if !strings.HasPrefix(value, "bytes ") {
		return 0, 0, 0, invalid
	}

	bounds, totalText, ok := strings.Cut(
		strings.TrimPrefix(value, "bytes "),
		"/",
	)
	if !ok {
		return 0, 0, 0, invalid
	}

	startText, endText, ok := strings.Cut(bounds, "-")
	if !ok {
		return 0, 0, 0, invalid
	}

	start, err = strconv.ParseInt(startText, 10, 64)
	if err != nil {
		return 0, 0, 0, invalid
	}
	end, err = strconv.ParseInt(endText, 10, 64)
	if err != nil {
		return 0, 0, 0, invalid
	}
	total, err = strconv.ParseInt(totalText, 10, 64)
	if err != nil {
		return 0, 0, 0, invalid
	}

	if start < 0 || end < start || total <= end {
		return 0, 0, 0, invalid
	}

	return start, end, total, nil
}

// claimDownloadPaths reserves paths against other downloads in this NetSvc.
//
// All paths are checked and claimed under one lock, so a conflicting request
// does not leave a partial reservation. The returned function releases the
// claims and should be deferred immediately.
//
// Keys are absolute lexical paths. This is not a filesystem lock and does not
// resolve symlink, hard-link, or filesystem-specific case aliases.
func (s *NetSvc) claimDownloadPaths(
	paths ...string,
) (func(), error) {
	keys := make(map[string]struct{}, len(paths))

	// Deduplicate paths, including the common case where the destination
	// and working path are identical because part files are disabled.
	for _, path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		keys[absolute] = struct{}{}
	}

	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()

	if s.activeDownloads == nil {
		s.activeDownloads = make(map[string]bool)
	}

	for path := range keys {
		if s.activeDownloads[path] {
			return nil, fmt.Errorf(
				"download path already in use: %s",
				path,
			)
		}
	}

	for path := range keys {
		s.activeDownloads[path] = true
	}

	return func() {
		s.downloadMu.Lock()
		defer s.downloadMu.Unlock()

		for path := range keys {
			delete(s.activeDownloads, path)
		}
	}, nil
}
