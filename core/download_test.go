package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func withFastRetries(t *testing.T) {
	t.Helper()
	oldBase, oldStall := downloadRetryBase, downloadStallTimeout
	downloadRetryBase = time.Millisecond
	downloadStallTimeout = 2 * time.Second
	t.Cleanup(func() {
		downloadRetryBase, downloadStallTimeout = oldBase, oldStall
	})
}

// downloadServer serves an EAPI file link and a CDN body. attempts counts
// CDN hits so retry behaviour is observable.
func downloadServer(t *testing.T, body string, cdnStatus func(attempt int32) int) (*httptest.Server, *int32) {
	t.Helper()
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/eapi/book/1/h/file":
			_, _ = io.WriteString(w, `{"success":1,"file":{"downloadLink":"`+srvURL(r)+`/cdn/book.epub"}}`)
		case r.URL.Path == "/cdn/book.epub":
			n := atomic.AddInt32(&attempts, 1)
			if status := cdnStatus(n); status != 0 {
				w.WriteHeader(status)
				return
			}
			w.Header().Set("Content-Disposition", `attachment; filename="Dune (z-library.sk, 2026).epub"`)
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			_, _ = io.WriteString(w, body)
		default:
			http.NotFound(w, r)
		}
	}))
	return srv, &attempts
}

// srvURL reconstructs the request origin inside a handler.
func srvURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func TestDownloadWritesAtomicallyAndCleansFilename(t *testing.T) {
	withFastRetries(t)
	srv, _ := downloadServer(t, "book-bytes", func(int32) int { return 0 })
	defer srv.Close()

	dir := t.TempDir()
	var lastDone, lastTotal int64
	result, err := testClient(t, srv.URL).DownloadWithContext(
		context.Background(),
		srv.URL+"/eapi/book/1/h/file",
		dir,
		func(done, total int64) { lastDone, lastTotal = done, total },
	)
	if err != nil {
		t.Fatalf("DownloadWithContext: %v", err)
	}
	if result.FilePath != filepath.Join(dir, "Dune.epub") {
		t.Errorf("path = %q", result.FilePath)
	}
	data, err := os.ReadFile(result.FilePath)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	if string(data) != "book-bytes" {
		t.Errorf("content = %q", data)
	}
	if lastDone != int64(len("book-bytes")) || lastTotal != int64(len("book-bytes")) {
		t.Errorf("progress = %d/%d", lastDone, lastTotal)
	}
	parts, _ := filepath.Glob(filepath.Join(dir, ".zlib-*.part"))
	if len(parts) != 0 {
		t.Errorf("temporary files left behind: %v", parts)
	}
}

func TestDownloadFailurePreservesExistingFile(t *testing.T) {
	withFastRetries(t)
	srv, _ := downloadServer(t, "", func(int32) int { return http.StatusInternalServerError })
	defer srv.Close()

	dir := t.TempDir()
	existing := filepath.Join(dir, "Dune.epub")
	if err := os.WriteFile(existing, []byte("old-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := testClient(t, srv.URL).DownloadWithContext(
		context.Background(), srv.URL+"/eapi/book/1/h/file", dir, nil)
	if err == nil {
		t.Fatal("download succeeded against a 500 response")
	}
	data, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("existing file disappeared: %v", err)
	}
	if string(data) != "old-content" {
		t.Errorf("existing file was modified: %q", data)
	}
	parts, _ := filepath.Glob(filepath.Join(dir, ".zlib-*.part"))
	if len(parts) != 0 {
		t.Errorf("temporary files left behind: %v", parts)
	}
}

func TestDownloadRetriesGatewayErrors(t *testing.T) {
	withFastRetries(t)
	srv, attempts := downloadServer(t, "ok", func(attempt int32) int {
		if attempt <= 2 {
			return http.StatusBadGateway
		}
		return 0
	})
	defer srv.Close()

	dir := t.TempDir()
	result, err := testClient(t, srv.URL).DownloadWithContext(
		context.Background(), srv.URL+"/eapi/book/1/h/file", dir, nil)
	if err != nil {
		t.Fatalf("download after retries: %v", err)
	}
	if got := atomic.LoadInt32(attempts); got < 3 {
		t.Errorf("CDN attempts = %d, want at least 3", got)
	}
	if _, err := os.Stat(result.FilePath); err != nil {
		t.Errorf("result missing: %v", err)
	}
}

func TestDownloadDoesNotRetryInternalServerError(t *testing.T) {
	withFastRetries(t)
	srv, attempts := downloadServer(t, "", func(int32) int { return http.StatusInternalServerError })
	defer srv.Close()

	_, err := testClient(t, srv.URL).DownloadWithContext(
		context.Background(), srv.URL+"/eapi/book/1/h/file", t.TempDir(), nil)
	if err == nil {
		t.Fatal("download succeeded")
	}
	if got := atomic.LoadInt32(attempts); got != 1 {
		t.Errorf("CDN attempts = %d, want exactly 1 (500 is not retryable)", got)
	}
}

func TestDownloadRequiresLogin(t *testing.T) {
	c := NewClient()
	if _, err := c.DownloadWithContext(context.Background(), "https://z-lib.gd/x", t.TempDir(), nil); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("err = %v, want ErrNotLoggedIn", err)
	}
}

func TestDownloadCancellation(t *testing.T) {
	withFastRetries(t)
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/file") {
			_, _ = io.WriteString(w, `{"success":1,"file":{"downloadLink":"`+srvURL(r)+`/cdn/book.epub"}}`)
			return
		}
		close(started)
		<-release
	}))
	defer srv.Close()
	defer close(release)

	c := testClient(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := c.DownloadWithContext(ctx, srv.URL+"/eapi/book/1/h/file", t.TempDir(), nil)
		errCh <- err
	}()
	<-started
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("download did not stop after cancellation")
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"../../evil.epub":        "evil.epub",
		`..\..\evil.epub`:        "evil.epub",
		"/etc/passwd":            "passwd",
		"..":                     "download",
		".":                      "download",
		"":                       "download",
		"a:b?.epub":              "a-b-.epub",
		"trailing. ":             "trailing",
		"book (z-library.sk, 1)": "book",
	}
	for in, want := range cases {
		if got := sanitizeFilename(cleanFilename(in)); got != want {
			t.Errorf("sanitize(clean(%q)) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanFilenameKeepsNonMirrorSuffix(t *testing.T) {
	if got := cleanFilename("Book (2nd Edition).epub"); got != "Book (2nd Edition).epub" {
		t.Errorf("cleanFilename changed a normal name: %q", got)
	}
}

// The EAPI file link ends in /redirection and the CDN sends no
// Content-Disposition: without a preferred name the file lands as
// "redirection" with no extension. The book metadata should stand in.
func TestDownloadBookFallsBackToPreferredName(t *testing.T) {
	withFastRetries(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/eapi/book/1/h/file":
			_, _ = io.WriteString(w, `{"success":1,"file":{"downloadLink":"`+srvURL(r)+`/redirection"}}`)
		case r.URL.Path == "/redirection":
			w.Header().Set("Content-Length", strconv.Itoa(len("book-bytes")))
			_, _ = io.WriteString(w, "book-bytes")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	result, err := testClient(t, srv.URL).DownloadBook(
		context.Background(), srv.URL+"/eapi/book/1/h/file", dir, "书名:重定向?测试.epub", nil)
	if err != nil {
		t.Fatalf("DownloadBook: %v", err)
	}
	want := filepath.Join(dir, "书名-重定向-测试.epub")
	if result.FilePath != want {
		t.Fatalf("path = %q, want %q", result.FilePath, want)
	}
	data, err := os.ReadFile(result.FilePath)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	if string(data) != "book-bytes" {
		t.Errorf("content = %q", data)
	}
}

// Without a preferred name the extensionless response name stands, so the
// fallback only applies when a caller actually supplies one.
func TestDownloadBookKeepsServerNameWithoutPreferred(t *testing.T) {
	withFastRetries(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/eapi/book/1/h/file":
			_, _ = io.WriteString(w, `{"success":1,"file":{"downloadLink":"`+srvURL(r)+`/redirection"}}`)
		case r.URL.Path == "/redirection":
			w.Header().Set("Content-Length", strconv.Itoa(len("x")))
			_, _ = io.WriteString(w, "x")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	result, err := testClient(t, srv.URL).DownloadBook(
		context.Background(), srv.URL+"/eapi/book/1/h/file", dir, "", nil)
	if err != nil {
		t.Fatalf("DownloadBook: %v", err)
	}
	if got := filepath.Base(result.FilePath); got != "redirection" {
		t.Errorf("base name = %q, want %q", got, "redirection")
	}
}
