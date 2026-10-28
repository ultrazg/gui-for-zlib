package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DownloadResult is what a successful transfer produces.
type DownloadResult struct {
	FilePath string
	Size     int64
}

// Download is DownloadWithContext with a background context.
func (c *Client) Download(downloadURL, destDir string, progress func(written, total int64)) (DownloadResult, error) {
	return c.DownloadWithContext(context.Background(), downloadURL, destDir, progress)
}

// DownloadWithContext runs three steps: resolve the EAPI file link, fetch
// it with retries, then write it atomically, named from the response
// (Content-Disposition, else the URL basename).
func (c *Client) DownloadWithContext(ctx context.Context, downloadURL, destDir string, progress func(written, total int64)) (DownloadResult, error) {
	return c.download(ctx, downloadURL, destDir, "", progress)
}

// DownloadBook is DownloadWithContext with a preferredName fallback: when
// the response's name has no file extension (e.g. the EAPI "redirection"
// link names the file "redirection"), the book-derived name stands in.
func (c *Client) DownloadBook(ctx context.Context, downloadURL, destDir, preferredName string, progress func(written, total int64)) (DownloadResult, error) {
	return c.download(ctx, downloadURL, destDir, preferredName, progress)
}

func (c *Client) download(ctx context.Context, downloadURL, destDir, preferredName string, progress func(written, total int64)) (DownloadResult, error) {
	if !c.LoggedIn() {
		return DownloadResult{}, ErrNotLoggedIn
	}
	if strings.TrimSpace(destDir) == "" {
		destDir = "."
	}

	resolved, err := c.resolveEAPIFileURL(ctx, downloadURL)
	if err != nil {
		return DownloadResult{}, err
	}

	// No total timeout: a huge book may take as long as it needs. Only a
	// dead connection is fatal, which the stall timer detects.
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stall := time.AfterFunc(downloadStallTimeout, func() { cancel(errDownloadStalled) })
	defer stall.Stop()

	resp, err := c.openDownload(ctx, resolved, stall)
	if err != nil {
		return DownloadResult{}, err
	}
	defer resp.Body.Close()

	return c.saveDownload(ctx, resp, resolved, destDir, preferredName, progress, stall)
}

// openDownload retries only transient failures: connection errors and
// 502/503/504. A response that starts delivering bytes is never retried.
func (c *Client) openDownload(ctx context.Context, rawURL string, stall *time.Timer) (*http.Response, error) {
	client := c.downloadClient()
	attempts := c.downloadRetries + 1
	var lastErr error

	for attempt := 0; attempt < attempts; attempt++ {
		req, err := c.newDownloadRequest(ctx, rawURL)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDownloadFailed, err)
		}

		resp, err := client.Do(req)
		switch {
		case err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300:
			return resp, nil
		case err == nil && isRetryableStatus(resp.StatusCode):
			drainAndClose(resp.Body)
			lastErr = &HTTPStatusError{StatusCode: resp.StatusCode, URL: rawURL}
		case err == nil:
			drainAndClose(resp.Body)
			return nil, &HTTPStatusError{StatusCode: resp.StatusCode, URL: rawURL}
		default:
			if ctx.Err() != nil {
				return nil, cancellationCause(ctx)
			}
			lastErr = err
		}

		if attempt < attempts-1 {
			if err := sleepContext(ctx, downloadRetryBase<<attempt); err != nil {
				return nil, cancellationCause(ctx)
			}
			stall.Reset(downloadStallTimeout)
		}
	}
	if ctx.Err() != nil {
		return nil, cancellationCause(ctx)
	}
	return nil, fmt.Errorf("%w: %v", ErrDownloadFailed, lastErr)
}

func isRetryableStatus(code int) bool {
	return code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusGatewayTimeout
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, body, downloadDrainLimit)
	_ = body.Close()
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// cancellationCause turns the stall/ctx pair into errors the GUI can
// classify: context.Canceled for a user cancel, ErrDownloadFailed for a
// stalled connection.
func cancellationCause(ctx context.Context) error {
	cause := context.Cause(ctx)
	switch {
	case errors.Is(cause, errDownloadStalled):
		return fmt.Errorf("%w: no data received for %s", ErrDownloadFailed, downloadStallTimeout)
	case errors.Is(cause, context.Canceled):
		return context.Canceled
	case cause != nil:
		return cause
	default:
		return context.Canceled
	}
}

// saveDownload streams to a .part file in destDir and renames on success.
// Any error removes the temporary file and leaves an existing target
// exactly as it was. name picks the response's filename, unless it carries
// no extension and preferredName does; preferredName is already sanitized.
func (c *Client) saveDownload(ctx context.Context, resp *http.Response, rawURL, destDir, preferredName string, progress func(written, total int64), stall *time.Timer) (DownloadResult, error) {
	name := sanitizeFilename(cleanFilename(filenameFromResponse(resp, rawURL)))
	if path.Ext(name) == "" && preferredName != "" {
		name = sanitizeFilename(cleanFilename(preferredName))
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return DownloadResult{}, fmt.Errorf("%w: create directory: %v", ErrDownloadFailed, err)
	}
	tmp, err := os.CreateTemp(destDir, ".zlib-*.part")
	if err != nil {
		return DownloadResult{}, fmt.Errorf("%w: create temp file: %v", ErrDownloadFailed, err)
	}
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmp.Name())
		}
	}()

	total := resp.ContentLength
	if total < 0 {
		total = 0
	}

	buf := make([]byte, downloadBufferSize)
	var written int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			stall.Reset(downloadStallTimeout)
			if _, writeErr := tmp.Write(buf[:n]); writeErr != nil {
				return DownloadResult{}, fmt.Errorf("%w: write: %v", ErrDownloadFailed, writeErr)
			}
			written += int64(n)
			if progress != nil {
				progress(written, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			if ctx.Err() != nil {
				return DownloadResult{}, cancellationCause(ctx)
			}
			return DownloadResult{}, fmt.Errorf("%w: read: %v", ErrDownloadFailed, readErr)
		}
	}

	if err := tmp.Chmod(0o644); err != nil {
		return DownloadResult{}, fmt.Errorf("%w: chmod: %v", ErrDownloadFailed, err)
	}
	if err := tmp.Sync(); err != nil {
		return DownloadResult{}, fmt.Errorf("%w: sync: %v", ErrDownloadFailed, err)
	}
	if err := tmp.Close(); err != nil {
		return DownloadResult{}, fmt.Errorf("%w: close: %v", ErrDownloadFailed, err)
	}

	destPath := filepath.Join(destDir, name)
	if err := os.Rename(tmp.Name(), destPath); err != nil {
		return DownloadResult{}, fmt.Errorf("%w: rename: %v", ErrDownloadFailed, err)
	}
	committed = true
	return DownloadResult{FilePath: destPath, Size: written}, nil
}

// filenameFromResponse prefers Content-Disposition, then the URL basename.
func filenameFromResponse(resp *http.Response, rawURL string) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if name := strings.TrimSpace(params["filename"]); name != "" {
				return name
			}
		}
	}
	if u, err := url.Parse(rawURL); err == nil {
		if base := path.Base(u.Path); base != "" && base != "/" && base != "." {
			if decoded, err := url.PathUnescape(base); err == nil {
				return decoded
			}
			return base
		}
	}
	return "download"
}

var ebookExtensionPattern = regexp.MustCompile(`(?i)\.(epub|pdf|mobi|azw3?|fb2|djvu?|lit|rtf|txt)\b`)

// cleanFilename drops the "(z-library.sk, ...)" mirror suffix that CDNs
// append to book files. If cut removes the extension along with the
// suffix ("Dune (z-library.sk, 2026).epub"), the extension is recovered
// from the removed segment.
func cleanFilename(name string) string {
	name = strings.TrimSpace(name)
	i := strings.LastIndex(name, " (")
	if i <= 0 {
		return name
	}
	tail := strings.ToLower(name[i:])
	if !strings.Contains(tail, "z-lib") && !strings.Contains(tail, "zlib") {
		return name
	}
	base := strings.TrimSpace(name[:i])
	if path.Ext(base) == "" {
		if ext := ebookExtensionPattern.FindString(tail); ext != "" {
			base += ext
		}
	}
	return base
}

// sanitizeFilename removes directory components. A malicious mirror can put
// "../../evil" in Content-Disposition, and filepath.Join would happily
// escape destDir.
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.ReplaceAll(name, "\x00", "")
	base := path.Base(name)
	switch base {
	case "", ".", "..", "/":
		return "download"
	}
	// Windows rejects these in file names; replacing keeps the download
	// possible instead of failing at rename time.
	replacer := strings.NewReplacer(
		":", "-", "*", "-", "?", "-", `"`, "-", "<", "-", ">", "-", "|", "-",
	)
	base = replacer.Replace(base)
	base = strings.TrimRight(base, " .")
	if base == "" {
		return "download"
	}
	return base
}
