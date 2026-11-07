package app

import (
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"zlib-gui/core"
)

type favoriteMutation struct {
	id    string
	saved bool
}

// favoriteSvc represents an account independent of any App instance.
type favoriteSvc struct {
	*core.Demo
	books      []core.Book
	signedOut  bool
	statusHook func(context.Context, core.Book) (bool, error)
	saveHook   func(context.Context, string, bool) error

	mu          sync.Mutex
	saved       map[string]bool
	mutations   []favoriteMutation
	pages       []int
	statusCalls int
}

func newFavoriteSvc(saved ...string) *favoriteSvc {
	s := &favoriteSvc{
		Demo: core.NewDemo(),
		books: []core.Book{
			{ID: "42", Hash: "h", Title: "第一本书"},
			{ID: "43", Hash: "other", Title: "第二本书"},
		},
		saved: map[string]bool{},
	}
	for _, id := range saved {
		s.saved[id] = true
	}
	return s
}

func (s *favoriteSvc) CurrentSession() *core.Session {
	if s.signedOut {
		return nil
	}
	return &core.Session{ID: "7", Email: "reader@example.com", Name: "reader"}
}

func (s *favoriteSvc) FavoriteStatus(ctx context.Context, book core.Book) (bool, error) {
	s.mu.Lock()
	s.statusCalls++
	saved := s.saved[book.ID]
	s.mu.Unlock()
	if s.statusHook != nil {
		return s.statusHook(ctx, book)
	}
	return saved, nil
}

func (s *favoriteSvc) SetFavorite(ctx context.Context, id string, saved bool) error {
	s.mu.Lock()
	s.mutations = append(s.mutations, favoriteMutation{id, saved})
	s.mu.Unlock()
	if s.saveHook != nil {
		if err := s.saveHook(ctx, id, saved); err != nil {
			return err
		}
	}
	s.mu.Lock()
	if saved {
		s.saved[id] = true
	} else {
		delete(s.saved, id)
	}
	s.mu.Unlock()
	return nil
}

func (s *favoriteSvc) Favorites(ctx context.Context, page, count int) (*core.BookPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pages = append(s.pages, page)
	books := []core.Book{}
	for _, book := range s.books {
		if s.saved[book.ID] {
			saved := true
			book.Saved = &saved
			books = append(books, book)
		}
	}
	start := min((page-1)*count, len(books))
	end := min(start+count, len(books))
	return &core.BookPage{
		Books: books[start:end], Page: page, Total: len(books),
		TotalPages: (len(books) + count - 1) / count,
	}, nil
}

func (s *favoriteSvc) changes() []favoriteMutation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]favoriteMutation(nil), s.mutations...)
}

type favoriteUI struct {
	a       *App
	tt      *ui.Tester
	updates chan func()
}

func newFavoriteUI(t *testing.T, svc *favoriteSvc) *favoriteUI {
	t.Helper()
	a := New(svc)
	// Start on a page without an automatic background load, so every update
	// below is explicitly driven by the test's event loop.
	a.path.Push("/settings")
	h := &favoriteUI{a: a, updates: make(chan func(), 32)}
	a.postUpdate = func(fn func()) { h.updates <- fn }
	h.tt = ui.NewTester(a.View, 1180, 760)
	t.Cleanup(a.resetFavorites)
	return h
}

func (h *favoriteUI) next(t *testing.T) {
	t.Helper()
	select {
	case fn := <-h.updates:
		fn()
		h.tt.Frame()
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not publish an update")
	}
}

func (h *favoriteUI) until(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for !done() {
		select {
		case fn := <-h.updates:
			fn()
			h.tt.Frame()
		case <-deadline.C:
			t.Fatal("UI state did not settle")
		}
	}
}

func (h *favoriteUI) open(t *testing.T, book core.Book) {
	t.Helper()
	h.a.showDetail(book)
	h.tt.Frame()
	h.until(t, func() bool {
		state := h.a.favorites[book.ID]
		return state != nil && !state.Loading
	})
}

func waitFavoriteSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
}

// An optional temporary artifact directory lets these same interaction tests
// supply rendered screenshots for visual review.
func favoriteScreenshot(t *testing.T, name string, tester *ui.Tester) {
	t.Helper()
	dir := os.Getenv("ZLIB_TEST_ARTIFACT_DIR")
	if dir == "" {
		return
	}
	file, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(file, tester.Image())
	closeErr := file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}

func TestFavoriteSyncAndAppReopen(t *testing.T) {
	svc := newFavoriteSvc()
	h := newFavoriteUI(t, svc)
	h.a.path.Push("/favorites")
	h.tt.Frame()
	h.until(t, func() bool { return h.a.library["favorites"].Loaded })
	h.open(t, svc.books[0])
	if err := h.tt.Click("收藏此书"); err != nil {
		t.Fatal(err)
	}
	h.until(t, func() bool {
		state := h.a.favorites["42"]
		list := h.a.library["favorites"]
		return state.Saved && !state.Saving && !list.Loading && len(list.Books) == 1
	})
	if !h.tt.HasText("已收藏到账户") || h.a.library["favorites"].Books[0].ID != "42" {
		t.Fatal("successful save did not update the account collection")
	}
	favoriteScreenshot(t, "favorite-saved.png", h.tt)
	if got := svc.changes(); !reflect.DeepEqual(got, []favoriteMutation{{"42", true}}) {
		t.Fatalf("mutations = %v", got)
	}

	// The supplied book has no favorite flag. A fresh app still finds the
	// persisted account favorite and offers the correct removal action.
	reopened := newFavoriteUI(t, svc)
	reopened.a.path.Push("/favorites")
	reopened.tt.Frame()
	reopened.until(t, func() bool { return reopened.a.library["favorites"].Loaded })
	reopened.open(t, svc.books[0])
	if !reopened.a.favorites["42"].Saved || !reopened.tt.HasText("已收藏到账户") {
		t.Fatal("reopened app did not read the saved account state")
	}
	if err := reopened.tt.Click("取消收藏此书"); err != nil {
		t.Fatal(err)
	}
	reopened.until(t, func() bool {
		state := reopened.a.favorites["42"]
		list := reopened.a.library["favorites"]
		return !state.Saved && !state.Saving && !list.Loading && len(list.Books) == 0
	})
	reopened.tt.Frame()
	reopened.tt.Frame()
	if got := svc.changes(); !reflect.DeepEqual(got, []favoriteMutation{{"42", true}, {"42", false}}) {
		t.Fatalf("mutations = %v", got)
	}
}

func TestFavoriteFailureAndDuplicateClick(t *testing.T) {
	svc := newFavoriteSvc()
	started, release := make(chan struct{}), make(chan struct{})
	var attempts atomic.Int64
	svc.saveHook = func(context.Context, string, bool) error {
		if attempts.Add(1) == 1 {
			close(started)
			<-release
			return errors.New("upstream rejected")
		}
		return nil
	}
	h := newFavoriteUI(t, svc)
	h.open(t, svc.books[0])
	if err := h.tt.Click("收藏此书"); err != nil {
		t.Fatal(err)
	}
	waitFavoriteSignal(t, started)
	// A second click or another build pass must not repeat the account write.
	_ = h.tt.Click("收藏此书")
	h.tt.Frame()
	h.tt.Frame()
	if len(svc.changes()) != 1 || h.a.favorites["42"].Saved {
		t.Fatal("pending save repeated or changed state before confirmation")
	}
	close(release)
	h.until(t, func() bool { return !h.a.favorites["42"].Saving })
	state := h.a.favorites["42"]
	if state.Saved || state.Err == "" || !h.tt.HasText("upstream rejected") {
		t.Fatalf("failed save state = %+v", state)
	}
	favoriteScreenshot(t, "favorite-error.png", h.tt)
	if err := h.tt.Click("收藏此书"); err != nil {
		t.Fatal(err)
	}
	h.until(t, func() bool { return h.a.favorites["42"].Saved && !h.a.favorites["42"].Saving })
	if h.a.favorites["42"].Err != "" || len(svc.changes()) != 2 {
		t.Fatal("explicit retry failed to recover")
	}
}

func TestFavoriteRequiresLogin(t *testing.T) {
	svc := newFavoriteSvc()
	svc.signedOut = true
	h := newFavoriteUI(t, svc)
	h.a.showDetail(svc.books[0])
	h.tt.Frame()
	if err := h.tt.Click("登录后收藏"); err != nil {
		t.Fatal(err)
	}
	if !h.a.login.Open || h.a.detail.Open || len(svc.changes()) != 0 || svc.statusCalls != 0 {
		t.Fatal("signed-out favorite action did not open login without an account request")
	}
}

func TestFavoriteStatusFailureCanRefresh(t *testing.T) {
	svc := newFavoriteSvc("42")
	var attempts atomic.Int64
	svc.statusHook = func(context.Context, core.Book) (bool, error) {
		if attempts.Add(1) == 1 {
			return false, core.ErrSessionExpired
		}
		return true, nil
	}
	h := newFavoriteUI(t, svc)
	// A cached book may carry the previous account's flag. A failed fresh
	// read must not make that flag authoritative for the current account.
	book := svc.books[0]
	saved := true
	book.Saved = &saved
	h.open(t, book)
	state := h.a.favorites["42"]
	if state.Known || state.Err == "" || !h.tt.HasText("会话已过期") {
		t.Fatalf("failed state read = %+v", state)
	}
	_ = h.tt.Click("收藏此书")
	if len(svc.changes()) != 0 {
		t.Fatal("unknown favorite state allowed a mutation")
	}
	if err := h.tt.Click("刷新收藏状态"); err != nil {
		t.Fatal(err)
	}
	h.until(t, func() bool { return h.a.favorites["42"].Known && !h.a.favorites["42"].Loading })
	if !h.a.favorites["42"].Saved || h.a.favorites["42"].Err != "" {
		t.Fatal("refresh did not recover the account state")
	}
}

func TestFavoriteStaleReadAfterSwitchingBooks(t *testing.T) {
	svc := newFavoriteSvc()
	started, release := make(chan struct{}), make(chan struct{})
	var oldContext context.Context
	svc.statusHook = func(ctx context.Context, book core.Book) (bool, error) {
		if book.ID == "42" {
			oldContext = ctx
			close(started)
			// Model a result that arrives even after cancellation.
			<-release
			return true, nil
		}
		return false, nil
	}
	h := newFavoriteUI(t, svc)
	h.a.showDetail(svc.books[0])
	h.tt.Frame()
	waitFavoriteSignal(t, started)
	h.open(t, svc.books[1])
	if oldContext.Err() == nil {
		t.Fatal("previous detail read was not canceled")
	}
	close(release)
	h.next(t)
	if h.a.detail.Book.ID != "43" || h.a.favorites["43"].Saved || h.a.favorites["42"].Known {
		t.Fatal("stale detail response overwrote a newer book")
	}
	h.tt.Frame()
	h.tt.Frame()
}

func TestFavoritePendingResultAfterAuthChange(t *testing.T) {
	svc := newFavoriteSvc()
	started, release := make(chan struct{}), make(chan struct{})
	var requestContext context.Context
	svc.saveHook = func(ctx context.Context, id string, saved bool) error {
		requestContext = ctx
		close(started)
		<-release
		return nil
	}
	h := newFavoriteUI(t, svc)
	h.open(t, svc.books[0])
	if err := h.tt.Click("收藏此书"); err != nil {
		t.Fatal(err)
	}
	waitFavoriteSignal(t, started)
	h.a.session = nil
	h.a.resetAfterAuthChange()
	h.tt.Frame()
	if requestContext.Err() == nil {
		t.Fatal("account change did not cancel its request")
	}
	close(release)
	h.next(t)
	if len(h.a.favorites) != 0 || h.a.detail.Open || h.a.detail.Book.Saved != nil {
		t.Fatal("previous account's completion restored favorite state")
	}
}

func TestFavoriteSaveSurvivesClosingDetail(t *testing.T) {
	svc := newFavoriteSvc()
	started, release := make(chan struct{}), make(chan struct{})
	var requestContext context.Context
	svc.saveHook = func(ctx context.Context, id string, saved bool) error {
		requestContext = ctx
		close(started)
		<-release
		return nil
	}
	h := newFavoriteUI(t, svc)
	h.open(t, svc.books[0])
	if err := h.tt.Click("收藏此书"); err != nil {
		t.Fatal(err)
	}
	waitFavoriteSignal(t, started)
	if err := h.tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	if requestContext.Err() != nil {
		t.Fatal("closing the dialog canceled an explicit account action")
	}
	h.open(t, svc.books[1])
	close(release)
	h.next(t)
	if !h.a.favorites["42"].Saved || h.a.detail.Book.ID != "43" || h.a.favorites["43"].Saved {
		t.Fatal("save completion changed the newly opened book")
	}
}

func TestFavoriteRemovalClampsLastPage(t *testing.T) {
	svc := newFavoriteSvc("42", "43")
	h := newFavoriteUI(t, svc)
	h.a.library["favorites"] = &libraryState{Count: 1}
	h.a.path.Push("/favorites")
	h.a.loadLibrary("favorites", 2)
	h.tt.Frame()
	h.until(t, func() bool { return h.a.library["favorites"].Loaded })
	h.open(t, h.a.library["favorites"].Books[0])
	if err := h.tt.Click("取消收藏此书"); err != nil {
		t.Fatal(err)
	}
	h.until(t, func() bool {
		list := h.a.library["favorites"]
		return !h.a.favorites["43"].Saving && !list.Loading &&
			list.Page == 1 && len(list.Books) == 1
	})
	list := h.a.library["favorites"]
	if list.Books[0].ID != "42" || list.TotalPages != 1 {
		t.Fatalf("collection after removal = %+v", list)
	}
	svc.mu.Lock()
	pages := append([]int(nil), svc.pages...)
	svc.mu.Unlock()
	if !reflect.DeepEqual(pages, []int{2, 2, 1}) {
		t.Fatalf("page refreshes = %v", pages)
	}
	h.tt.Frame()
	h.tt.Frame()
}
