package core

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestBackendRestoresSessionAndLogsOut(t *testing.T) {
	t.Setenv(EnvConfigDir, t.TempDir())
	t.Setenv(EnvDomain, "https://z-lib.gd")
	t.Setenv(EnvProxy, "")

	if err := SaveSession(&SessionFile{
		Cookies: map[string]string{"remix_userid": "7", "remix_userkey": "key"},
		Domain:  "https://z-lib.gd",
		Mode:    "eapi",
	}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if err := SaveAppConfig(&AppConfig{LastLoginEmail: "reader@example.com"}); err != nil {
		t.Fatalf("SaveAppConfig: %v", err)
	}

	b := NewBackend()
	if b.CurrentDomain() != "https://z-lib.gd" {
		t.Errorf("domain = %q", b.CurrentDomain())
	}
	session := b.CurrentSession()
	if session == nil {
		t.Fatal("session was not restored")
	}
	if session.Email != "reader@example.com" || session.Name != "reader" {
		t.Errorf("session = %+v", session)
	}
	if got := b.LastLoginEmail(); got != "reader@example.com" {
		t.Errorf("LastLoginEmail = %q", got)
	}

	if err := b.Logout(); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if b.CurrentSession() != nil {
		t.Error("CurrentSession returned a session after logout")
	}
	if _, err := os.Stat(SessionPath()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("session.json still exists after logout: %v", err)
	}
}

func TestBackendRequiresLoginForLists(t *testing.T) {
	t.Setenv(EnvConfigDir, t.TempDir())
	t.Setenv(EnvDomain, "https://z-lib.gd")
	t.Setenv(EnvProxy, "")

	b := NewBackend()
	ctx := context.Background()
	if _, err := b.Favorites(ctx, 1, 10); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("Favorites err = %v, want ErrNotLoggedIn", err)
	}
	if _, err := b.Profile(ctx); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("Profile err = %v, want ErrNotLoggedIn", err)
	}
	if _, err := b.Download(ctx, Book{ID: "1", Hash: "h"}, t.TempDir(), nil); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("Download err = %v, want ErrNotLoggedIn", err)
	}
}

func TestBackendGUISettingsRoundTrip(t *testing.T) {	t.Setenv(EnvConfigDir, t.TempDir())
	t.Setenv(EnvDomain, "https://z-lib.gd")
	t.Setenv(EnvProxy, "")

	want := GUISettings{
		Theme:            "Gruvbox Dark",
		DownloadDir:      "C:/books",
		DefaultExtension: "EPUB",
		PageCount:        25,
		ProxyEnabled:     true,
		ProxyScheme:      "socks5",
		ProxyAddr:        "127.0.0.1",
		ProxyPort:        "7890",
		NotifyOnDownload: true,
	}
	if err := NewBackend().SaveGUISettings(want); err != nil {
		t.Fatalf("SaveGUISettings: %v", err)
	}

	got := NewBackend().CurrentGUISettings()
	if got.Theme != want.Theme || got.DownloadDir != want.DownloadDir ||
		got.DefaultExtension != want.DefaultExtension || got.PageCount != want.PageCount ||
		got.ProxyEnabled != want.ProxyEnabled || got.ProxyScheme != want.ProxyScheme ||
		got.ProxyAddr != want.ProxyAddr || got.ProxyPort != want.ProxyPort ||
		got.NotifyOnDownload != want.NotifyOnDownload {
		t.Errorf("GUISettings = %+v, want %+v", got, want)
	}

	// A successful login later must not clear the GUI block.
	cmd := &AppConfig{GUI: want}
	cmd.LastLoginEmail = "reader@example.com"
	if err := SaveAppConfig(cmd); err != nil {
		t.Fatalf("SaveAppConfig: %v", err)
	}
	after := NewBackend().CurrentGUISettings()
	if after.PageCount != 25 {
		t.Errorf("login-style SaveAppConfig dropped GUI settings: %+v", after)
	}
}

func TestBackendEmptySearchIsFree(t *testing.T) {
	t.Setenv(EnvConfigDir, t.TempDir())
	t.Setenv(EnvDomain, "https://z-lib.gd")
	t.Setenv(EnvProxy, "")

	b := NewBackend()
	page, err := b.Search(context.Background(), SearchOptions{Query: "  "})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if page == nil || page.Books == nil || len(page.Books) != 0 {
		t.Errorf("page = %#v", page)
	}
}

func TestBackendSetProxy(t *testing.T) {
	t.Setenv(EnvConfigDir, t.TempDir())
	t.Setenv(EnvDomain, "https://z-lib.gd")
	t.Setenv(EnvProxy, "")

	b := NewBackend()
	if got := b.client.Proxy(); got != "" {
		t.Fatalf("initial proxy = %q, want empty", got)
	}
	if err := b.SetProxy("socks5://127.0.0.1:7890"); err != nil {
		t.Fatalf("SetProxy: %v", err)
	}
	if got := b.client.Proxy(); got != "socks5://127.0.0.1:7890" {
		t.Fatalf("proxy after SetProxy = %q", got)
	}
	if err := b.SetProxy(""); err != nil {
		t.Fatalf("SetProxy(\"\"): %v", err)
	}
	if got := b.client.Proxy(); got != "" {
		t.Fatalf("proxy after clear = %q, want empty", got)
	}
}

// A malformed proxy is rejected during parse, before any dial, so the test
// stays network-free.
func TestBackendTestProxyRejectsBadProxy(t *testing.T) {
	t.Setenv(EnvConfigDir, t.TempDir())
	t.Setenv(EnvDomain, "https://z-lib.gd")
	t.Setenv(EnvProxy, "")

	b := NewBackend()
	_, err := b.TestProxy(context.Background(), "ftp://127.0.0.1:8080")
	if !errors.Is(err, ErrInvalidProxy) {
		t.Fatalf("TestProxy err = %v, want ErrInvalidProxy", err)
	}
}

// Doctor returns rows for every known mirror even when they all fail, and
// stamps the report with the time the probes finished. A canceled context
// fails every probe immediately (no network), which is enough to exercise
// the report assembly without holding the test open on real hosts.
func TestBackendDoctorStampsChecked(t *testing.T) {
	t.Setenv(EnvConfigDir, t.TempDir())
	b := NewBackend()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	report, err := b.Doctor(ctx)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if report.Checked.IsZero() {
		t.Error("report.Checked is the zero time")
	}
	if got := len(report.Domains); got != len(KnownDomains()) {
		t.Errorf("rows = %d, want %d", got, len(KnownDomains()))
	}
}
