package app

import (
	"testing"
	"time"

	"zlib-gui/core"
)

func TestParseDeepLink(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantID  string
		wantKey bool
	}{
		{"book with id and hash", "zlib://book/1001/a1b2c3", "1001", true},
		{"host is case insensitive", "zlib://Book/1001/a1b2c3", "1001", true},
		{"hash may be long", "zlib://book/1001/0123456789abcdef", "1001", true},
		{"wrong scheme", "https://book/1001/a1b2c3", "", false},
		{"wrong host", "zlib://search/1001/a1b2c3", "", false},
		{"missing hash", "zlib://book/1001", "", false},
		{"empty id", "zlib://book//a1b2c3", "", false},
		{"trailing segment", "zlib://book/1001/a1b2c3/extra", "", false},
		{"not a url", "just some text", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, ok := parseDeepLink(tc.raw)
			if ok != tc.wantKey {
				t.Fatalf("parseDeepLink(%q) ok = %v, want %v", tc.raw, ok, tc.wantKey)
			}
			if !ok {
				return
			}
			if b.ID != tc.wantID {
				t.Errorf("parseDeepLink(%q) ID = %q, want %q", tc.raw, b.ID, tc.wantID)
			}
		})
	}
}

// A zlib://book/{id}/{hash} deep link opens the detail modal with the book
// it names. No window is attached in tests, so update runs synchronously.
func TestHandleDeepLinkOpensDetail(t *testing.T) {
	a := New(core.NewDemo())

	a.HandleDeepLink("zlib://book/1001/a1b2c3")

	if !a.detail.Open {
		t.Fatal("deep link did not open the detail modal")
	}
	if a.detail.Book.ID != "1001" || a.detail.Book.Hash != "a1b2c3" {
		t.Errorf("deep link book = %s:%s, want 1001:a1b2c3", a.detail.Book.ID, a.detail.Book.Hash)
	}
}

// A URL of the scheme we do not understand is ignored rather than opened as
// a book with an empty title.
func TestHandleDeepLinkIgnoresUnknown(t *testing.T) {
	a := New(core.NewDemo())

	a.HandleDeepLink("zlib://profile")
	a.HandleDeepLink("zlib://book/1001")

	if a.detail.Open {
		t.Fatal("unknown deep link opened the detail modal")
	}
}

// With no window attached (tests, tray-less runs before WhenReady), bringing
// the app to the front is a safe no-op.
func TestBringToFrontWithoutWindow(t *testing.T) {
	a := New(core.NewDemo())
	a.BringToFront() // must not panic
}

// A finished download fires the desktop notification: the notification ID
// carries "download-" and the book's id:hash so clicks route to the
// downloads page, and repeated downloads of the same book replace their
// notification instead of stacking.
func TestDownloadCompletionNotifies(t *testing.T) {
	a := New(core.NewDemo())
	a.session = &core.Session{Email: "reader@example.com", Name: "reader"}

	var gotID, gotTitle, gotBody string
	a.notify = func(id, title, body string) {
		gotID, gotTitle, gotBody = id, title, body
	}

	a.startDownload(core.Book{ID: "1001", Hash: "a1b2c3", Title: "示例书目"})

	deadline := time.Now().Add(5 * time.Second)
	for {
		if len(a.downloads.Items) > 0 && a.downloads.Items[0].Finished {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("download did not finish within 5s")
		}
		time.Sleep(25 * time.Millisecond)
	}

	if gotID != "download-1001:a1b2c3" {
		t.Errorf("notification id = %q, want download-1001:a1b2c3", gotID)
	}
	if gotTitle != "下载完成" {
		t.Errorf("notification title = %q, want 下载完成", gotTitle)
	}
	if gotBody != "示例书目" {
		t.Errorf("notification body = %q, want 示例书目", gotBody)
	}
}
