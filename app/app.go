package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"zlib-gui/core"
)

// App holds the window's state. Views read it on the main thread; jobs
// publish changes through Window.Update (see update).
type App struct {
	svc  core.Service
	win  *mygo.Window
	path *ui.Router

	themeName string
	domain    string
	session   *core.Session

	login       loginState
	search      searchState
	downloads   downloadState
	library     map[string]*libraryState
	profile     profileState
	doctor      doctorState
	settings    settingsState
	proxyTest   proxyTestState
	updateCheck updateCheckState

	detail detailState
	saved  map[string]bool

	toastMsg string
	// notify sends a desktop notification (download complete, ...). It stays
	// nil in tests, where Attach is never called, so completed jobs just
	// toast instead of touching the system.
	notify func(id, title, body string)
}

// New builds the app with the given backend. main passes core.NewBackend();
// tests can pass a double implementing core.Service.
func New(svc core.Service) *App {
	a := &App{
		svc:       svc,
		path:      ui.NewRouter("/home"),
		themeName: systemTheme,
		library:   map[string]*libraryState{},
		saved:     map[string]bool{},
	}
	a.settings.DownloadDir = defaultDownloadDir()
	a.settings.Extension = "全部"
	a.settings.Count = "10"
	a.search.Extension = "全部"
	a.search.Count = 10
	a.search.Selected = -1
	a.session = svc.CurrentSession()
	if d, ok := svc.(interface{ CurrentDomain() string }); ok {
		a.domain = d.CurrentDomain()
	}
	a.loadGUISettings()

	// Optional capability of the real backend: prefill the login email
	// from disk. Done once at construction, never during a view build.
	if l, ok := svc.(interface{ LastLoginEmail() string }); ok {
		a.login.Email = l.LastLoginEmail()
	}
	return a
}

// Attach connects the app to its window and starts the first search. It
// runs inside App.WhenReady, so the app is up and notifications can show.
func (a *App) Attach(win *mygo.Window) {
	a.win = win
	if a.notify == nil {
		a.notify = func(id, title, body string) {
			_ = mygo.NewNotification(mygo.NotificationOptions{ID: id, Title: title, Body: body}).Show()
		}
	}
	if !a.search.Started {
		a.startSearch(1)
	}
}

// OpenDownloads navigates to the downloads page. Notification clicks use it;
// it must be called on the main thread, which MyGo guarantees for them.
func (a *App) OpenDownloads() { a.path.Push("/downloads") }

// View builds one frame. MyGo calls it again after input and after
// Window.Update.
func (a *App) View(c *ui.Context) {
	a.applyTheme(c)
	t := c.Theme()

	// With a window material enabled, the material shows wherever the view
	// draws no background: make the root and the sidebar transparent and
	// let the content pane draw its own opaque background (see sidebar).
	vibrant := c.Vibrancy()
	if vibrant {
		c.Root().Background(ui.Transparent)
	}

	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		a.sidebar(c, vibrant)
		ui.Column(c).Grow(1).MinWidth(0).Background(t.Background).Children(func() {
			a.toolbar(c)
			a.path.View(c, func(r *ui.Route) {
				a.page(c, r)
			})
		})
	})

	a.loginModal(c)
	a.detailModal(c)
	a.updateModal(c)

	if a.toastMsg != "" {
		c.Toast(a.toastMsg)
		a.toastMsg = ""
	}
}

func (a *App) page(c *ui.Context, r *ui.Route) {
	switch {
	case r.Match("/search"):
		r.Title("搜索")
		a.searchPage(c)
	case r.Match("/recommended"):
		r.Title("推荐")
		a.libraryPage(c, "recommended")
	case r.Match("/popular"):
		r.Title("最热")
		a.libraryPage(c, "popular")
	case r.Match("/favorites"):
		r.Title("收藏")
		a.libraryPage(c, "favorites")
	case r.Match("/history"):
		r.Title("历史")
		a.libraryPage(c, "history")
	case r.Match("/downloads"):
		r.Title("下载")
		a.downloadsPage(c)
	case r.Match("/profile"):
		r.Title("账户")
		a.profilePage(c)
	case r.Match("/doctor"):
		r.Title("域名检测")
		a.doctorPage(c)
	case r.Match("/settings"):
		r.Title("设置")
		a.settingsPage(c)
	default:
		r.Title("首页")
		a.homePage(c)
	}
}

func (a *App) sidebar(c *ui.Context, vibrant bool) {
	t := c.Theme()
	// In an opaque window the sidebar carries a surface of its own; with a
	// material behind it the window shows it through instead.
	side := ui.Column(c).Width(212).PaddingY(14).Shrink(0)
	if !vibrant {
		side.Background(t.Surface)
	}
	side.Children(func() {
		if runtime.GOOS == "darwin" {
			ui.Text(c, "zlib").FontSize(22).Bold().Padding(36, 18, 0, 18)
		} else {
			ui.Text(c, "zlib").FontSize(22).Bold().Padding(0, 18)
		}
		ui.Text(c, "GUI for zlib").FontSize(12).TextColor(t.TextMuted).Padding(0, 18, 12)

		page := sidebarPageOf(a.path.Path())
		nav := ui.Sidebar(c, &page, func() {
			ui.SidebarItem(c, "/home", nil, "首页")
			ui.SidebarItem(c, "/search", nil, "搜索")
			ui.SidebarItem(c, "/recommended", nil, "推荐")
			ui.SidebarItem(c, "/popular", nil, "最热")
			ui.SidebarItem(c, "/favorites", nil, "收藏")
			ui.SidebarItem(c, "/history", nil, "历史")
			ui.SidebarItem(c, "/downloads", nil, "下载").Children(func() {
				// A badge counts the downloads still running in the
				// background; it disappears when none are left.
				if n := a.activeDownloads(); n > 0 {
					ui.Badge(c, strconv.Itoa(n))
				}
			})
			ui.SidebarSection(c, "账户与工具", nil, func() {
				ui.SidebarItem(c, "/profile", nil, "账户")
				ui.SidebarItem(c, "/doctor", nil, "域名检测")
				ui.SidebarItem(c, "/settings", nil, "设置")
			})
		}).Grow(1).Label("主导航")
		if vibrant {
			nav.Background(ui.Transparent)
		}
		if nav.Changed() {
			a.path.Push(page)
		}

		ui.Divider(c)
		ui.Column(c).Padding(10, 16).Gap(6).Children(func() {
			if a.session != nil {
				ui.Text(c, a.session.Name).Bold()
				ui.Text(c, a.session.Email).FontSize(12).TextColor(t.TextMuted).SingleLine()
				if ui.Button(c, "退出登录").Clicked() {
					a.session = nil
					a.resetAfterAuthChange()
					a.search.Books = nil
					a.search.Started = false
					a.search.Total = 0
					a.search.TotalPages = 0
					a.search.Selected = -1
					_ = a.svc.Logout()
					a.toast("已退出登录")
				}
				return
			}
			ui.Text(c, "未登录").FontSize(13).TextColor(t.TextMuted)
			if ui.Button(c, "登录账户").Clicked() {
				a.openLogin()
			}
		})
	})
}

func (a *App) toolbar(c *ui.Context) {
	// t := c.Theme()
	// The title bar is hidden (main.go), so the toolbar is the window's
	// drag handle: pressing and dragging its empty area (or the page
	// title) moves the window. The back/forward buttons stay clickable —
	// an interactive child keeps its own press.
	ui.Row(c).Padding(10, 18).Gap(8).AlignItems(ui.Center).Height(50).DragWindow().Children(func() {
		// ui.BackButton(c, a.path)
		// ui.ForwardButton(c, a.path)
		// ui.Text(c, a.pageTitle()).FontSize(14).Bold().Padding(0, 8)
		// ui.Spacer(c)
		// if a.session != nil {
		// 	ui.Badge(c, a.session.Email)
		// } else {
		// 	ui.Text(c, "未登录").FontSize(12).TextColor(t.TextMuted)
		// }
	})
	ui.Divider(c)
}

func (a *App) pageTitle() string {
	switch {
	case a.path.Path() == "/search":
		return "搜索"
	case a.path.Path() == "/recommended":
		return "推荐"
	case a.path.Path() == "/popular":
		return "最热"
	case a.path.Path() == "/favorites":
		return "收藏"
	case a.path.Path() == "/history":
		return "历史"
	case a.path.Path() == "/downloads":
		return "下载"
	case a.path.Path() == "/profile":
		return "账户"
	case a.path.Path() == "/doctor":
		return "域名检测"
	case a.path.Path() == "/settings":
		return "设置"
	default:
		return "首页"
	}
}

func sidebarPageOf(path string) string {
	switch path {
	case "/home", "/search", "/recommended", "/popular", "/favorites",
		"/history", "/downloads", "/profile", "/doctor", "/settings":
		return path
	}
	return "/home"
}

// update runs fn on the main thread and asks for a frame. Before the
// window exists (unit tests) it runs at once.
func (a *App) update(fn func()) {
	if a.win != nil {
		a.win.Update(fn)
		return
	}
	fn()
}

func (a *App) toast(msg string) { a.toastMsg = msg }

// loadGUISettings restores the display preferences the backend saved in
// config.json. The demo backend has none and keeps the defaults.
func (a *App) loadGUISettings() {
	g, ok := a.svc.(interface{ CurrentGUISettings() core.GUISettings })
	if !ok {
		return
	}
	cfg := g.CurrentGUISettings()

	// $ZLIB_THEME beats the saved value, and an unknown name is ignored so
	// the app keeps the default theme.
	if name := os.Getenv(core.EnvTheme); name != "" && a.knownTheme(name) {
		a.themeName = name
	} else if cfg.Theme != "" {
		a.themeName = cfg.Theme
	}

	if cfg.DownloadDir != "" {
		a.settings.DownloadDir = cfg.DownloadDir
	}
	if cfg.DefaultExtension != "" {
		a.settings.Extension = cfg.DefaultExtension
		a.search.Extension = cfg.DefaultExtension
	}
	if cfg.PageCount > 0 {
		a.settings.Count = strconv.Itoa(cfg.PageCount)
	}

	// 网络代理：scheme 默认 socks5 供下拉框显示，地址/端口留空。全新安装时
	// applySavedProxy 不会用空配置清除环境变量 ZLIB_PROXY 的行为。
	a.settings.ProxyEnabled = cfg.ProxyEnabled
	a.settings.ProxyScheme = cfg.ProxyScheme
	a.settings.ProxyAddr = cfg.ProxyAddr
	a.settings.ProxyPort = cfg.ProxyPort
	if a.settings.ProxyScheme == "" {
		a.settings.ProxyScheme = "socks5"
	}
	a.applySavedProxy()
}

func (a *App) knownTheme(name string) bool {
	if name == systemTheme {
		return true
	}
	_, ok := themePalettes[name]
	return ok
}

// applySavedProxy pushes config.json's proxy into the backend at startup. It
// runs before the window exists, so failures surface as a toast rendered on
// the first frame rather than a modal; a broken saved proxy falls back to
// direct rather than leaving the app wedged on a proxy that never worked.
// If the config never recorded a proxy (no address/port), the env-seeded
// $ZLIB_PROXY is left alone.
func (a *App) applySavedProxy() {
	setter, ok := a.svc.(interface{ SetProxy(string) error })
	if !ok {
		return
	}
	if !a.settings.ProxyEnabled {
		if a.settings.ProxyAddr != "" || a.settings.ProxyPort != "" {
			_ = setter.SetProxy("")
		}
		return
	}
	proxy, err := core.ProxyURLFromParts(a.settings.ProxyScheme, a.settings.ProxyAddr, a.settings.ProxyPort)
	if err != nil {
		a.toast("代理设置无效，已改为直连：" + friendlyError(err))
		_ = setter.SetProxy("")
		return
	}
	if err := setter.SetProxy(proxy); err != nil {
		a.toast("代理设置无效：" + friendlyError(err))
	}
}

// guiSettingsSnapshot reports the display preferences the GUI owns, for
// config.json persistence.
func (a *App) guiSettingsSnapshot() core.GUISettings {
	pageCount, _ := strconv.Atoi(a.settings.Count)
	return core.GUISettings{
		Theme:            a.themeName,
		DownloadDir:      a.settings.DownloadDir,
		DefaultExtension: a.settings.Extension,
		PageCount:        pageCount,
		ProxyEnabled:     a.settings.ProxyEnabled,
		ProxyScheme:      a.settings.ProxyScheme,
		ProxyAddr:        a.settings.ProxyAddr,
		ProxyPort:        a.settings.ProxyPort,
	}
}

// persistSettings writes the GUI's display preferences and reports whether
// the backend could save them (a demo backend stays in memory).
func (a *App) persistSettings() bool {
	s, ok := a.svc.(interface{ SaveGUISettings(core.GUISettings) error })
	if !ok {
		return false
	}
	if err := s.SaveGUISettings(a.guiSettingsSnapshot()); err != nil {
		a.toast("设置未能保存：" + friendlyError(err))
		return false
	}
	return true
}

func defaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "downloads"
	}
	return filepath.Join(home, "Downloads")
}
