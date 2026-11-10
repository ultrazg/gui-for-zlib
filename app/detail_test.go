package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"zlib-gui/core"
)

type detailUI struct {
	a       *App
	tt      *ui.Tester
	updates chan func()
}

func newDetailUI(t *testing.T, svc core.Service) *detailUI {
	t.Helper()
	a := New(svc)
	a.path.Push("/settings")
	h := &detailUI{a: a, updates: make(chan func(), 32)}
	a.postUpdate = func(fn func()) { h.updates <- fn }
	h.tt = ui.NewTester(a.View, 1180, 760)
	t.Cleanup(func() {
		if a.detail.coverCancel != nil {
			a.detail.coverCancel()
		}
		a.resetFavorites()
	})
	return h
}

func (h *detailUI) next(t *testing.T) {
	t.Helper()
	select {
	case fn := <-h.updates:
		fn()
		h.tt.Frame()
	case <-time.After(3 * time.Second):
		t.Fatal("cover worker did not publish an update")
	}
}

func (h *detailUI) settleCover(t *testing.T) {
	t.Helper()
	for !h.a.detail.coverDone {
		h.next(t)
	}
}

type detailSvc struct {
	*core.Demo
	history    []core.Book
	bookHook   func(context.Context, string, string) (*core.Book, error)
	imageHook  func(context.Context, string) ([]byte, error)
	bookCalls  atomic.Int32
	imageCalls atomic.Int32
}

func (s *detailSvc) History(ctx context.Context, page, count int) (*core.BookPage, error) {
	return &core.BookPage{Books: s.history, Page: 1, TotalPages: 1}, nil
}

func (s *detailSvc) FetchBook(ctx context.Context, id, hash string) (*core.Book, error) {
	s.bookCalls.Add(1)
	if s.bookHook == nil {
		return nil, core.ErrUnsupported
	}
	return s.bookHook(ctx, id, hash)
}

func (s *detailSvc) FetchImage(ctx context.Context, url string) ([]byte, error) {
	s.imageCalls.Add(1)
	return s.imageHook(ctx, url)
}

func detailPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 16, G: 177, B: 211, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestHistoryDetailCoverLoads(t *testing.T) {
	for _, source := range []string{"missing", "expired", "invalid image"} {
		t.Run(source, func(t *testing.T) {
			data := detailPNG(t)
			book := core.Book{ID: "42", Hash: "h", Title: "下载过的书", Date: "2026-11-10"}
			if source != "missing" {
				book.Cover = "https://example.com/old.jpg"
			}
			const resolved = "https://example.com/current.png"
			svc := &detailSvc{Demo: core.NewDemo(), history: []core.Book{book}}
			svc.bookHook = func(_ context.Context, id, hash string) (*core.Book, error) {
				if id != book.ID || hash != book.Hash {
					t.Errorf("detail identity = %s:%s, want %s", id, hash, book.Key())
				}
				return &core.Book{ID: id, Hash: hash, Cover: resolved}, nil
			}
			svc.imageHook = func(_ context.Context, url string) ([]byte, error) {
				if url == resolved {
					return data, nil
				}
				if source == "invalid image" {
					return []byte("<html>expired image</html>"), nil
				}
				return nil, errors.New("expired cover")
			}
			h := newDetailUI(t, svc)
			if err := h.tt.Click("历史"); err != nil {
				t.Fatal(err)
			}
			h.next(t)
			if err := h.tt.Click(book.Title); err != nil {
				t.Fatal(err)
			}
			h.tt.Key(0, ui.KeyEnter)
			if !h.a.detail.Open {
				t.Fatal("history book did not open its detail modal")
			}
			h.settleCover(t)
			if h.a.detail.cover == nil {
				t.Fatal("history detail did not load the cover from the book endpoint")
			}
			if h.a.detail.Book.Date != book.Date {
				t.Fatal("cover lookup discarded the download date")
			}
			if got := svc.bookCalls.Load(); got != 1 {
				t.Errorf("detail calls = %d, want 1", got)
			}
			wantImages := int32(1)
			if source != "missing" {
				wantImages++
			}
			if got := svc.imageCalls.Load(); got != wantImages {
				t.Errorf("image calls = %d, want %d", got, wantImages)
			}

			// Check the rendered pixels as well as the decoded bitmap state.
			img := h.tt.Image()
			pixels := 0
			for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
				for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
					r, g, b, _ := img.At(x, y).RGBA()
					if r == 16*257 && g == 177*257 && b == 211*257 {
						pixels++
					}
				}
			}
			if pixels < 100 {
				t.Fatalf("cover bitmap was not rendered: %d matching pixels", pixels)
			}

			h.a.detail.Open = false
			h.tt.Frame()
			h.a.showDetail(book)
			h.tt.Frame()
			if h.a.detail.cover == nil || svc.bookCalls.Load() != 1 || svc.imageCalls.Load() != wantImages {
				t.Fatal("reopening the history book did not reuse its successful cover")
			}
		})
	}
}

func TestDetailCoverReloadsWhenSourceChanges(t *testing.T) {
	svc := &coverSvc{Demo: core.NewDemo()}
	h := newDetailUI(t, svc)
	book := core.Book{ID: "42", Hash: "h", Title: "示例书目"}
	h.a.showDetail(book)
	book.Cover = "https://example.com/first.png"
	h.a.showDetail(book)
	h.settleCover(t)
	if h.a.detail.cover == nil {
		t.Fatal("an earlier missing cover prevented a later URL from loading")
	}
	book.Cover = "https://example.com/second.png"
	h.a.showDetail(book)
	h.settleCover(t)
	if svc.calls != 2 {
		t.Fatalf("changed cover URL fetched %d images, want 2", svc.calls)
	}
}

func TestDetailCoverRetryAfterFailure(t *testing.T) {
	data := detailPNG(t)
	svc := &detailSvc{Demo: core.NewDemo()}
	svc.imageHook = func(_ context.Context, _ string) ([]byte, error) {
		if svc.imageCalls.Load() == 1 {
			return nil, errors.New("temporary image failure")
		}
		return data, nil
	}
	h := newDetailUI(t, svc)
	book := core.Book{ID: "42", Hash: "h", Title: "示例书目", Cover: "https://example.com/cover.png"}
	h.a.showDetail(book)
	h.settleCover(t)
	if h.a.detail.cover != nil {
		t.Fatal("failed request unexpectedly produced a cover")
	}
	h.a.showDetail(book)
	h.settleCover(t)
	if h.a.detail.cover == nil || svc.imageCalls.Load() != 2 {
		t.Fatal("reopening the book did not retry the failed cover")
	}
}

type blockedCoverSvc struct {
	*core.Demo
	started chan struct{}
	release chan struct{}
	data    []byte
}

func (s *blockedCoverSvc) FetchImage(_ context.Context, _ string) ([]byte, error) {
	close(s.started)
	<-s.release
	return s.data, nil
}

func TestDetailCoverStaleResultAfterSourceRemoved(t *testing.T) {
	svc := &blockedCoverSvc{
		Demo: core.NewDemo(), started: make(chan struct{}), release: make(chan struct{}), data: detailPNG(t),
	}
	t.Cleanup(func() { close(svc.release) })
	h := newDetailUI(t, svc)
	book := core.Book{ID: "42", Hash: "h", Title: "示例书目", Cover: "https://example.com/cover.png"}
	h.a.showDetail(book)
	select {
	case <-svc.started:
	case <-time.After(3 * time.Second):
		t.Fatal("cover request did not start")
	}
	book.Cover = ""
	h.a.showDetail(book)
	// Complete the obsolete fetch even though its source is no longer present.
	svc.release <- struct{}{}
	h.next(t)
	if h.a.detail.cover != nil || !h.a.detail.coverDone {
		t.Fatal("an obsolete image replaced the current placeholder")
	}
}
