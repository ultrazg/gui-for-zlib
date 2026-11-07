package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestFavoriteAccountRoundTrip(t *testing.T) {
	var mu sync.Mutex
	saved := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("remix-userid") != "7" ||
			r.Header.Get("remix-userkey") != "session-key" {
			t.Errorf("favorite request lacks the expected method or credentials")
		}
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/eapi/user/book/42/save", "/eapi/user/book/42/unsave":
			if r.URL.RawQuery != "" || !strings.Contains(r.Header.Get("Cache-Control"), "no-store") {
				t.Error("account mutation must be uncached and use only the numeric path ID")
			}
			saved = strings.HasSuffix(r.URL.Path, "/save")
			fmt.Fprint(w, `{"success":1}`)
		case "/eapi/book/42/h":
			fmt.Fprintf(w, `{"success":1,"book":{"id":42,"hash":"h","_isUserSavedBook":%t}}`, saved)
		case pathSaved:
			books := "[]"
			if saved {
				books = `[{"id":42,"hash":"h","title":"Account favorite"}]`
			}
			fmt.Fprintf(w, `{"success":1,"books":%s,"pagination":{"current":1,"total_pages":1}}`, books)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx := context.Background()
	book := Book{ID: "42", Hash: "h"}
	backend := &Backend{client: testClient(t, server.URL)}
	if got, err := backend.FavoriteStatus(ctx, book); err != nil || got {
		t.Fatalf("initial favorite = %v, %v", got, err)
	}
	if err := backend.SetFavorite(ctx, book.ID, true); err != nil {
		t.Fatal(err)
	}
	page, err := backend.Favorites(ctx, 1, 10)
	if err != nil || len(page.Books) != 1 || page.Books[0].Saved == nil || !*page.Books[0].Saved {
		t.Fatalf("saved collection = %+v, %v", page, err)
	}
	// A fresh backend must recover the state from the account, not local memory.
	reopened := &Backend{client: testClient(t, server.URL)}
	if got, err := reopened.FavoriteStatus(ctx, book); err != nil || !got {
		t.Fatalf("reopened favorite = %v, %v", got, err)
	}
	if err := reopened.SetFavorite(ctx, book.ID, false); err != nil {
		t.Fatal(err)
	}
	page, err = backend.Favorites(ctx, 1, 10)
	if err != nil || len(page.Books) != 0 {
		t.Fatalf("unsaved collection = %+v, %v", page, err)
	}
}

func TestFavoriteStatusUsesServerFlag(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{{"true", true}, {"false", false}, {"1", true}, {"0", false}, {`"1"`, true}, {`"0"`, false}} {
		t.Run(tc.value, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path != "/eapi/book/42/h" {
					t.Errorf("unexpected fallback request %s", r.URL.Path)
				}
				fmt.Fprintf(w, `{"success":1,"book":{"id":42,"hash":"h","_isUserSavedBook":%s}}`, tc.value)
			}))
			defer server.Close()
			got, err := testClient(t, server.URL).FavoriteStatus(context.Background(), Book{ID: "42", Hash: "h"})
			if err != nil || got != tc.want || requests.Load() != 1 {
				t.Fatalf("state = %v, %v, requests = %d", got, err, requests.Load())
			}
		})
	}
}

func TestFavoriteStatusFallsBackToAllPages(t *testing.T) {
	for _, tc := range []struct {
		id   string
		want bool
	}{{"42", true}, {"99", false}} {
		t.Run(tc.id, func(t *testing.T) {
			var pages atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/eapi/book/") {
					fmt.Fprintf(w, `{"success":1,"book":{"id":%q,"hash":"h"}}`, tc.id)
					return
				}
				if r.URL.Path != pathSaved || r.URL.Query().Get("limit") != "50" {
					t.Errorf("unexpected saved-books request %s", r.URL.String())
				}
				pages.Add(1)
				page := r.URL.Query().Get("page")
				id := 7
				if page == "2" {
					id = 42
				}
				fmt.Fprintf(w, `{"success":1,"books":[{"id":%d,"hash":"h"}],"pagination":{"current":%s,"total_pages":2}}`, id, page)
			}))
			defer server.Close()
			got, err := testClient(t, server.URL).FavoriteStatus(context.Background(), Book{ID: tc.id, Hash: "h"})
			if err != nil || got != tc.want || pages.Load() != 2 {
				t.Fatalf("state = %v, %v, pages = %d", got, err, pages.Load())
			}
		})
	}
}

func TestFavoriteStatusRejectsIncompleteCollection(t *testing.T) {
	server := httptest.NewServer(jsonHandler(http.StatusOK, `{"success":1,"books":[{"id":7,"hash":"h"}]}`))
	defer server.Close()
	_, err := testClient(t, server.URL).FavoriteStatus(context.Background(), Book{ID: "42"})
	if !errors.Is(err, ErrFavoriteStateUnknown) {
		t.Fatalf("incomplete collection error = %v", err)
	}
}

func TestSetFavoriteRejectsInvalidIDAndAnonymous(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"success":1}`)
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	for _, id := range []string{"", "42:hash", "../42", "42oops", "-1"} {
		if err := client.SetFavorite(context.Background(), id, true); !errors.Is(err, ErrNoID) {
			t.Errorf("ID %q: error = %v", id, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.SetFavorite(ctx, "42", true); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled mutation = %v", err)
	}
	client.Logout()
	backend := &Backend{client: client}
	if err := backend.SetFavorite(context.Background(), "42", true); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("anonymous mutation = %v", err)
	}
	if _, err := backend.FavoriteStatus(context.Background(), Book{ID: "42", Hash: "h"}); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("anonymous state read = %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid or anonymous operations sent %d requests", requests.Load())
	}
}

func TestSetFavoriteReportsFailureWithoutRetry(t *testing.T) {
	for _, tc := range []struct {
		name, body, message string
		status              int
		expired             bool
	}{
		{"rejected", `{"success":0,"error":"Favorite rejected"}`, "Favorite rejected", 400, false},
		{"expired", `{"success":0,"error":"Not authorized"}`, "Not authorized", 401, true},
		{"missing-success", `{}`, "missing success", 200, false},
		{"malformed", `not json`, "invalid EAPI", 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			err := testClient(t, server.URL).SetFavorite(context.Background(), "42", true)
			if err == nil || !strings.Contains(err.Error(), tc.message) || requests.Load() != 1 {
				t.Fatalf("error = %v, requests = %d", err, requests.Load())
			}
			if errors.Is(err, ErrSessionExpired) != tc.expired {
				t.Errorf("session classification = %v", err)
			}
		})
	}
}

func TestSetFavoriteDoesNotReplayLostResponse(t *testing.T) {
	var mutations atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/eapi/book/42/h" {
			fmt.Fprint(w, `{"success":1,"book":{"id":42,"hash":"h","_isUserSavedBook":false}}`)
			return
		}
		mutations.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	if _, err := client.FavoriteStatus(context.Background(), Book{ID: "42", Hash: "h"}); err != nil {
		t.Fatal(err)
	}
	if err := client.SetFavorite(context.Background(), "42", true); err == nil {
		t.Fatal("lost mutation response reported success")
	}
	if mutations.Load() != 1 {
		t.Fatalf("mutation replayed %d times", mutations.Load())
	}
}

func TestDemoFavoriteStateBelongsToAccount(t *testing.T) {
	demo := NewDemo()
	ctx := context.Background()
	book := Book{ID: "1002", Hash: "d4e5f6"}
	if _, err := demo.Login(ctx, "reader@example.com", "fixture"); err != nil {
		t.Fatal(err)
	}
	if err := demo.SetFavorite(ctx, book.ID, true); err != nil {
		t.Fatal(err)
	}
	demo.Logout()
	demo.Login(ctx, "another@example.com", "fixture")
	if got, err := demo.FavoriteStatus(ctx, book); err != nil || got {
		t.Fatalf("another account inherited favorite = %v, %v", got, err)
	}
	demo.Logout()
	demo.Login(ctx, "reader@example.com", "fixture")
	if got, err := demo.FavoriteStatus(ctx, book); err != nil || !got {
		t.Fatalf("original account lost favorite = %v, %v", got, err)
	}
}
