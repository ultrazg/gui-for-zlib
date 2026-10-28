package app

import (
	"net/url"
	"strings"

	"zlib-gui/core"
)

// parseDeepLink turns a URL of the app's scheme into the book it names.
// Only "zlib://book/{id}/{hash}" is understood; anything malformed or
// unknown reports false so it is ignored instead of surfacing an empty
// detail. A Book's id:hash is enough for the backend to download.
func parseDeepLink(rawURL string) (core.Book, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(u.Scheme, "zlib") {
		return core.Book{}, false
	}
	if !strings.EqualFold(u.Host, "book") {
		return core.Book{}, false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return core.Book{}, false
	}
	return core.Book{ID: parts[0], Hash: parts[1]}, true
}

// HandleDeepLink opens the book a zlib:// URL names. MyGo calls it on the
// main thread: for the URL the app was launched with, and for URLs the
// single-instance lock forwards from a later instance.
func (a *App) HandleDeepLink(rawURL string) {
	b, ok := parseDeepLink(rawURL)
	if !ok {
		return
	}
	a.update(func() {
		a.showDetail(b)
		a.toast("已打开书籍详情")
	})
}

// BringToFront shows and focuses the main window when a second instance
// starts (the single-instance lock forwards its arguments to us). Called on
// the main thread.
func (a *App) BringToFront() {
	if a.win == nil {
		return
	}
	a.win.Show()
	if a.win.IsMinimized() {
		a.win.Restore()
	}
	a.win.Focus()
}
