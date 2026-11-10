package app

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"zlib-gui/core"
)

func newTesterWith(t *testing.T, svc core.Service) (*App, *ui.Tester) {
	t.Helper()
	a := New(svc)
	return a, ui.NewTester(a.View, 1180, 760)
}

func newTester(t *testing.T) (*App, *ui.Tester) {
	t.Helper()
	return newTesterWith(t, core.NewDemo())
}

// savingSvc stands in for core.Backend's config.json round trip, recording
// what the GUI says it persisted.
type savingSvc struct {
	*core.Demo
	got core.GUISettings
}

func (s *savingSvc) CurrentGUISettings() core.GUISettings { return s.got }
func (s *savingSvc) SaveGUISettings(cfg core.GUISettings) error {
	s.got = cfg
	return nil
}

func TestLoginValidation(t *testing.T) {
	a, tt := newTester(t)

	if err := tt.Click("登录账户"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("登录"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("请输入有效的邮箱地址。") {
		t.Errorf("texts %q do not show the email validation", tt.Texts())
	}

	a.login.Email = "reader@example.com"
	if err := tt.Click("登录"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("密码至少 3 位。") {
		t.Errorf("texts %q do not show the password validation", tt.Texts())
	}
	if a.session != nil {
		t.Errorf("signed in without a password")
	}
}

// Logout is gated by a confirmation dialog. Headless tests stub it (nil
// means confirm); the real window shows a native message box.
func TestLogoutConfirmation(t *testing.T) {
	a, tt := newTester(t)
	a.session = &core.Session{Email: "reader@example.com", Name: "reader"}
	// State set behind the tester's back needs an explicit frame; input
	// events build their own.
	tt.Frame()

	if !tt.HasText("退出登录") {
		t.Fatalf("texts %q do not show the logout button", tt.Texts())
	}

	// Declining the dialog keeps the session.
	confirmed := 0
	a.confirmLogout = func() bool {
		confirmed++
		return false
	}
	if err := tt.Click("退出登录"); err != nil {
		t.Fatal(err)
	}
	if a.session == nil {
		t.Fatal("logged out although the confirmation was declined")
	}
	if confirmed != 1 {
		t.Errorf("confirmation asked %d times, want 1", confirmed)
	}

	// Confirming drops the session.
	a.confirmLogout = func() bool {
		confirmed++
		return true
	}
	if err := tt.Click("退出登录"); err != nil {
		t.Fatal(err)
	}
	if a.session != nil {
		t.Fatal("still signed in after a confirmed logout")
	}
	if confirmed != 2 {
		t.Errorf("confirmation asked %d times total, want 2", confirmed)
	}

	// Without an attached window the default (nil) proceeds right away.
	a.session = &core.Session{Email: "reader@example.com", Name: "reader"}
	a.confirmLogout = nil
	tt.Frame()
	if err := tt.Click("退出登录"); err != nil {
		t.Fatal(err)
	}
	if a.session != nil {
		t.Fatal("headless logout with nil confirmation kept the session")
	}
}

// The register affordance in the login modal opens the mirror's sign-up page.
func TestLoginRegister(t *testing.T) {
	a, tt := newTester(t)

	if err := tt.Click("登录账户"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("前往注册") {
		t.Fatalf("texts %q do not show the register link", tt.Texts())
	}
	if err := tt.Click("前往注册"); err != nil {
		t.Fatal(err)
	}
	urls := tt.OpenedURLs()
	if want := core.RegistrationURL(a.domain); len(urls) != 1 || urls[0] != want {
		t.Errorf("register click opened %q, want %q", urls, want)
	}
}

func TestSettingsPersist(t *testing.T) {
	svc := &savingSvc{Demo: core.NewDemo()}
	a, tt := newTesterWith(t, svc)

	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("下载完成后通知") {
		t.Errorf("texts %q do not show the download notification toggle", tt.Texts())
	}
	a.settings.Count = "25"
	a.settings.NotifyOnDownload = true
	if err := tt.Click("保存设置"); err != nil {
		t.Fatal(err)
	}
	if svc.got.PageCount != 25 {
		t.Errorf("saved page count = %d, want 25", svc.got.PageCount)
	}
	if svc.got.Theme != systemTheme {
		t.Errorf("saved theme = %q, want %q", svc.got.Theme, systemTheme)
	}
	if !svc.got.NotifyOnDownload {
		t.Error("saved NotifyOnDownload = false, want true")
	}

	// A second app instance started against the same backend restores the
	// saved page count and notification toggle.
	a2 := New(svc)
	if a2.settings.Count != "25" {
		t.Errorf("restored page count = %q, want 25", a2.settings.Count)
	}
	if !a2.settings.NotifyOnDownload {
		t.Error("restored NotifyOnDownload = false, want true")
	}
	_ = a
}

// The settings page's about section shows the app name and homepage
// regardless of how the binary was built. The version line is excluded
// deliberately: mygo dev/build inject it via ldflags, so it only appears in
// those builds, not in a plain test binary.
func TestSettingsAbout(t *testing.T) {
	_, tt := newTester(t)

	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("关于") {
		t.Errorf("texts %q do not show the about section", tt.Texts())
	}
	if !tt.HasText("GUI for zlib") {
		t.Errorf("texts %q do not show the app name", tt.Texts())
	}
	if !tt.HasText("GitHub") {
		t.Errorf("texts %q do not show the project homepage link", tt.Texts())
	}
	if !tt.HasText("heartleo/zlib") {
		t.Errorf("texts %q do not credit the upstream project", tt.Texts())
	}
	tt.Scroll(800, 600, 0, 1500)
	if err := tt.Click("heartleo/zlib"); err != nil {
		t.Fatal(err)
	}
	urls := tt.OpenedURLs()
	if len(urls) != 1 || urls[0] != core.UpstreamURL {
		t.Errorf("clicking heartleo/zlib opened %q, want %q", urls, core.UpstreamURL)
	}
	if !tt.HasText("检查更新") {
		t.Errorf("texts %q do not show the update check button", tt.Texts())
	}
}

// proxySvc wraps savingSvc with the runtime proxy capabilities the real
// backend offers, recording the last SetProxy value.
type proxySvc struct {
	*savingSvc
	setProxy string
}

func (s *proxySvc) SetProxy(p string) error {
	s.setProxy = p
	return nil
}

func (s *proxySvc) TestProxy(_ context.Context, _ string) (time.Duration, error) {
	return 15 * time.Millisecond, nil
}

// Enabling the proxy on the settings page and saving applies it to the
// backend immediately, persists it, and a fresh app restores and re-applies
// it at startup.
func TestSettingsProxyPersistAndApply(t *testing.T) {
	svc := &proxySvc{savingSvc: &savingSvc{Demo: core.NewDemo()}}
	a, tt := newTesterWith(t, svc)

	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	a.settings.ProxyEnabled = true
	a.settings.ProxyAddr = "127.0.0.1"
	a.settings.ProxyPort = "7890"
	tt.Frame()
	if err := tt.Click("保存设置"); err != nil {
		t.Fatal(err)
	}

	if svc.setProxy != "socks5://127.0.0.1:7890" {
		t.Errorf("SetProxy called with %q, want socks5://127.0.0.1:7890", svc.setProxy)
	}
	if !svc.got.ProxyEnabled || svc.got.ProxyScheme != "socks5" ||
		svc.got.ProxyAddr != "127.0.0.1" || svc.got.ProxyPort != "7890" {
		t.Errorf("persisted GUI settings = %+v", svc.got)
	}

	// A second app instance against the same backend restores the fields
	// and re-applies the proxy at startup.
	a2 := New(svc)
	if !a2.settings.ProxyEnabled || a2.settings.ProxyAddr != "127.0.0.1" ||
		a2.settings.ProxyPort != "7890" {
		t.Errorf("restored proxy settings = %+v", a2.settings)
	}
	if svc.setProxy != "socks5://127.0.0.1:7890" {
		t.Errorf("startup did not re-apply the saved proxy: %q", svc.setProxy)
	}
	_ = a
}

// Turning the switch off clears the applied proxy while keeping the fields
// for when the user re-enables it.
func TestSettingsProxyDisableClears(t *testing.T) {
	svc := &proxySvc{savingSvc: &savingSvc{Demo: core.NewDemo()}}
	a, tt := newTesterWith(t, svc)

	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	a.settings.ProxyEnabled = true
	a.settings.ProxyAddr = "127.0.0.1"
	a.settings.ProxyPort = "7890"
	tt.Frame()
	if err := tt.Click("保存设置"); err != nil {
		t.Fatal(err)
	}
	if svc.setProxy != "socks5://127.0.0.1:7890" {
		t.Fatalf("SetProxy before disable = %q", svc.setProxy)
	}

	a.settings.ProxyEnabled = false
	tt.Frame()
	if err := tt.Click("保存设置"); err != nil {
		t.Fatal(err)
	}
	if svc.setProxy != "" {
		t.Errorf("SetProxy after disable = %q, want cleared", svc.setProxy)
	}
	if svc.got.ProxyEnabled {
		t.Errorf("persisted ProxyEnabled after disable = true")
	}
}

// An invalid enabled proxy aborts the whole save, like the page-count
// validation: nothing is persisted and the backend proxy is untouched.
func TestSettingsProxyInvalidAbortsSave(t *testing.T) {
	svc := &proxySvc{savingSvc: &savingSvc{Demo: core.NewDemo()}}
	a, tt := newTesterWith(t, svc)

	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	a.settings.ProxyEnabled = true
	a.settings.ProxyAddr = "127.0.0.1"
	a.settings.ProxyPort = "abc"
	a.settings.Count = "25"
	tt.Frame()
	if err := tt.Click("保存设置"); err != nil {
		t.Fatal(err)
	}

	if svc.got.PageCount != 0 {
		t.Errorf("save was not aborted: persisted PageCount = %d", svc.got.PageCount)
	}
	if svc.setProxy != "" {
		t.Errorf("SetProxy touched by an invalid save: %q", svc.setProxy)
	}
	if a.toastMsg != "" && !strings.Contains(a.toastMsg, "代理设置无效") {
		t.Errorf("toast = %q, want 代理设置无效", a.toastMsg)
	}
}

// coverSvc serves one tiny PNG for every cover URL, counting how often it
// was asked.
type coverSvc struct {
	*core.Demo
	calls int
}

func (s *coverSvc) FetchImage(ctx context.Context, url string) ([]byte, error) {
	s.calls++
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// A book with a cover URL fetches and decodes it once; reopening the same
// book reuses the cached cover instead of refetching.
func TestDetailCoverLoads(t *testing.T) {
	svc := &coverSvc{Demo: core.NewDemo()}
	a, _ := newTesterWith(t, svc)

	b := core.Book{ID: "1", Hash: "h", Title: "示例书目", Cover: "https://example.com/c.jpg"}
	a.showDetail(b)
	if a.detail.coverDone {
		t.Fatal("cover marked done before the fetch settled")
	}

	deadline := time.Now().Add(3 * time.Second)
	for !a.detail.coverDone {
		if time.Now().After(deadline) {
			t.Fatal("cover did not load within 3s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if a.detail.cover == nil {
		t.Fatal("cover was not decoded")
	}
	if svc.calls != 1 {
		t.Errorf("fetch calls = %d, want 1", svc.calls)
	}

	a.showDetail(b)
	if svc.calls != 1 {
		t.Errorf("fetch calls after reopening = %d, want 1", svc.calls)
	}
}

// A book without a cover settles immediately and the modal shows the
// title-letter placeholder in the cover slot.
func TestDetailCoverPlaceholderWhenMissing(t *testing.T) {
	a, tt := newTester(t)

	a.showDetail(core.Book{ID: "9", Hash: "x", Title: "示例书目"})
	tt.Frame()
	if !a.detail.coverDone || a.detail.cover != nil {
		t.Errorf("cover state after empty-cover open = done %v, nil %v",
			a.detail.coverDone, a.detail.cover == nil)
	}
	if !tt.HasText("示") {
		t.Errorf("texts %q do not show the cover placeholder", tt.Texts())
	}
}

// A long description stays inside the modal: the action row (关闭/下载)
// keeps within the window instead of being pushed out by the text.
func TestDetailLongDescriptionStaysInWindow(t *testing.T) {
	a, tt := newTester(t)

	desc := strings.Repeat("这是一段用来测试简介溢出的长文字。", 120)
	a.showDetail(core.Book{ID: "9", Hash: "x", Title: "示例书目", Description: desc})
	tt.Frame()

	if !tt.HasText("这是一段用来测试简介溢出的长文字。") {
		t.Fatalf("texts %q do not show the description", tt.Texts())
	}
	const windowH = 760
	btn, ok := tt.Find("关闭")
	if !ok {
		t.Fatalf("texts %q do not show the close button", tt.Texts())
	}
	if btn.Y+btn.H > windowH {
		t.Errorf("close button bottom %v exceeds the window height %d", btn.Y+btn.H, windowH)
	}
}

// With window vibrancy on, the layout still renders: the sidebar drops its
// surface background and the content pane draws its own, and every page
// keeps working.
func TestVibrancyLayout(t *testing.T) {
	_, tt := newTester(t)
	tt.SetVibrancy(true)
	tt.Frame()

	if !tt.HasText("欢迎使用 GUI for zlib") {
		t.Errorf("texts %q do not show the home page under vibrancy", tt.Texts())
	}
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("下载目录") {
		t.Errorf("texts %q do not show the settings page under vibrancy", tt.Texts())
	}
}

func TestEmptyDownloads(t *testing.T) {
	_, tt := newTester(t)

	if err := tt.Click("下载"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("还没有下载任务") {
		t.Errorf("texts %q do not show the empty downloads page", tt.Texts())
	}
}

// The recommended and popular pages load their feeds asynchronously, like
// favorites and history, and settle into the same chapter book list.
func TestRecommendedAndPopular(t *testing.T) {
	a, tt := newTester(t)

	if err := tt.Click("推荐"); err != nil {
		t.Fatal(err)
	}
	waitLibrary(t, a, "recommended")
	tt.Frame()
	if !tt.HasText("为你推荐的书。") {
		t.Errorf("texts %q do not show the recommended page", tt.Texts())
	}
	if !tt.HasText("银河边缘的图书馆") {
		t.Errorf("texts %q do not list recommended books", tt.Texts())
	}

	if err := tt.Click("最热"); err != nil {
		t.Fatal(err)
	}
	waitLibrary(t, a, "popular")
	tt.Frame()
	if !tt.HasText("全站最热门的书。") {
		t.Errorf("texts %q do not show the popular page", tt.Texts())
	}
	if !tt.HasText("Go 并发模式实践") {
		t.Errorf("texts %q do not list popular books", tt.Texts())
	}
}

// waitLibrary blocks until the given feed page has finished loading. In
// tests the demo backends run on real time, so the load takes a moment.
func waitLibrary(t *testing.T, a *App, kind string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		ls := a.library[kind]
		if ls != nil && ls.Loaded {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("library %q did not load within 3s", kind)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
