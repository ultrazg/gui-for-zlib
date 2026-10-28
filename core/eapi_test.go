package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// testClient points a real Client at an httptest server and installs a
// logged-in cookie snapshot. The allowlist is not applied here on purpose:
// the parser has its own test, and transport behaviour is what these tests
// exercise.
func testClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	t.Setenv(EnvProxy, "")
	t.Setenv(EnvRetries, "")
	c := NewClient()
	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	c.setDomainURL(u)
	c.SetCookies(map[string]string{"remix_userid": "7", "remix_userkey": "session-key"})
	return c
}

func jsonHandler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func TestLoginEAPI(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/eapi/user/login" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = r.ParseForm()
		gotForm = r.PostForm
		_, _ = io.WriteString(w, `{"success":1,"user":{"id":12345,"remix_userkey":"key-1"}}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	session, err := c.LoginEAPI(context.Background(), "reader@example.com", "secret")
	if err != nil {
		t.Fatalf("LoginEAPI: %v", err)
	}
	if gotForm.Get("email") != "reader@example.com" || gotForm.Get("password") != "secret" {
		t.Errorf("login form = %v", gotForm)
	}
	if session.ID != "12345" || session.Name != "reader" {
		t.Errorf("session = %+v", session)
	}
	cookies := c.Cookies()
	if cookies["remix_userid"] != "12345" || cookies["remix_userkey"] != "key-1" {
		t.Errorf("cookies = %v", cookies)
	}
	if !c.LoggedIn() {
		t.Error("client is not logged in after a successful login")
	}
}

func TestLoginEAPILegacyKeyField(t *testing.T) {
	srv := httptest.NewServer(jsonHandler(200, `{"success":1,"user":{"id":"42","remix_userkey":"","remix_user_key":"legacy"}}`))
	defer srv.Close()

	c := testClient(t, srv.URL)
	if _, err := c.LoginEAPI(context.Background(), "a@b.c", "pw"); err != nil {
		t.Fatalf("LoginEAPI: %v", err)
	}
	if got := c.Cookies()["remix_userkey"]; got != "legacy" {
		t.Errorf("remix_userkey = %q, want legacy", got)
	}
}

func TestLoginEAPIInvalidCredentials(t *testing.T) {
	srv := httptest.NewServer(jsonHandler(200, `{"success":0,"error":"Incorrect email or password"}`))
	defer srv.Close()

	c := testClient(t, srv.URL)
	c.Logout()
	_, err := c.LoginEAPI(context.Background(), "a@b.c", "bad")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
	if c.LoggedIn() {
		t.Error("client is logged in after a rejected login")
	}
}

func TestSearchMapping(t *testing.T) {
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
		_, _ = io.WriteString(w, `{
			"success": 1,
			"books": [{
				"id": 115066162,
				"hash": "e9f13b",
				"title": "Dune",
				"author": "Frank Herbert",
				"year": 1965,
				"publisher": "Chilton",
				"language": "english",
				"extension": "epub",
				"filesizeString": "2 MB",
				"description": "A desert planet.",
				"interestScore": "4.8",
				"pages": 412,
				"href": "/book/115066162/e9f13b",
				"cover": "https://covers.example/dune.jpg",
				"categories": [{"name": "Science Fiction"}, "Classics"],
				"isbn13": "9780593099322"
			}],
			"pagination": {"current": 2, "total_pages": 5}
		}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	page, err := c.searchEAPI(context.Background(), SearchOptions{
		Query: "dune", Extension: "EPUB", Page: 2, Count: 10,
	})
	if err != nil {
		t.Fatalf("searchEAPI: %v", err)
	}

	if form.Get("message") != "dune" || form.Get("page") != "2" || form.Get("limit") != "10" {
		t.Errorf("search form = %v", form)
	}
	if got := form["extensions[]"]; len(got) != 1 || got[0] != "epub" {
		t.Errorf("extensions[] = %v", got)
	}

	if page.Page != 2 || page.TotalPages != 5 || page.Total != 50 {
		t.Errorf("pagination = %+v", page)
	}
	if len(page.Books) != 1 {
		t.Fatalf("got %d books", len(page.Books))
	}
	b := page.Books[0]
	if b.ID != "115066162" || b.Hash != "e9f13b" || b.Title != "Dune" {
		t.Errorf("book identity = %+v", b)
	}
	if b.Author != "Frank Herbert" || b.Year != "1965" || b.Extension != "EPUB" {
		t.Errorf("book fields = %+v", b)
	}
	if b.Rating != 4.8 || b.Pages != 412 || b.Size != "2 MB" {
		t.Errorf("display fields = %+v", b)
	}
	if b.Categories != "Science Fiction, Classics" {
		t.Errorf("categories = %q", b.Categories)
	}
	if b.URL != srv.URL+"/book/115066162/e9f13b" {
		t.Errorf("URL = %q", b.URL)
	}
	wantDownload := srv.URL + "/eapi/book/115066162/e9f13b/file"
	if b.DownloadURL != wantDownload {
		t.Errorf("DownloadURL = %q, want %q", b.DownloadURL, wantDownload)
	}
	if b.ISBN13 != "9780593099322" || b.ISBN10 == "" {
		t.Errorf("isbn = %q / %q", b.ISBN10, b.ISBN13)
	}
}

func TestSearchPaginationFallback(t *testing.T) {
	srv := httptest.NewServer(jsonHandler(200, `{"success":1,"books":[]}`))
	defer srv.Close()

	c := testClient(t, srv.URL)
	page, err := c.searchEAPI(context.Background(), SearchOptions{Query: "x", Page: 3, Count: 10})
	if err != nil {
		t.Fatalf("searchEAPI: %v", err)
	}
	if page.Page != 3 {
		t.Errorf("page = %d, want the requested 3", page.Page)
	}
	if page.Books == nil || len(page.Books) != 0 {
		t.Errorf("books = %#v, want an empty non-nil slice", page.Books)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	c := NewClient()
	if _, err := c.searchEAPI(context.Background(), SearchOptions{Query: "  "}); !errors.Is(err, ErrEmptyQuery) {
		t.Fatalf("err = %v, want ErrEmptyQuery", err)
	}
}

func TestEAPIEnvelopePassedThroughOnHTTPError(t *testing.T) {
	srv := httptest.NewServer(jsonHandler(500, `{"success":0,"error":"daily limit reached"}`))
	defer srv.Close()

	c := testClient(t, srv.URL)
	_, err := c.searchEAPI(context.Background(), SearchOptions{Query: "x", Count: 10})
	if err == nil || !strings.Contains(err.Error(), "daily limit reached") {
		t.Fatalf("err = %v, want the server's original message", err)
	}

	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "<html>oops</html>")
	}))
	defer plain.Close()

	var statusErr *HTTPStatusError
	if _, err := testClient(t, plain.URL).searchEAPI(context.Background(), SearchOptions{Query: "x"}); !errors.As(err, &statusErr) {
		t.Fatalf("err = %v, want HTTPStatusError", err)
	}
}

func TestBotProtection(t *testing.T) {
	diamwall := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><title>DiamWall</title>checking your browser</html>")
	}))
	defer diamwall.Close()

	_, err := testClient(t, diamwall.URL).searchEAPI(context.Background(), SearchOptions{Query: "x"})
	if !errors.Is(err, ErrBotProtection) {
		t.Fatalf("err = %v, want ErrBotProtection", err)
	}

	// A book title containing the word must not be mistaken for a block:
	// JSON bodies are never scanned.
	srv := httptest.NewServer(jsonHandler(200, `{"success":1,"books":[{"id":1,"hash":"h","title":"diamwall"}]}`))
	defer srv.Close()
	if _, err := testClient(t, srv.URL).searchEAPI(context.Background(), SearchOptions{Query: "diamwall"}); err != nil {
		t.Fatalf("search with a diamwall book title failed: %v", err)
	}
}

func TestRedirectStripsCredentials(t *testing.T) {
	var seen http.Header
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		_, _ = io.WriteString(w, `{"success":1,"books":[]}`)
	}))
	defer final.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL+"/eapi/book/search", http.StatusFound)
	}))
	defer redirector.Close()

	c := testClient(t, redirector.URL)
	if _, err := c.searchEAPI(context.Background(), SearchOptions{Query: "x"}); err != nil {
		t.Fatalf("searchEAPI through redirect: %v", err)
	}
	if seen.Get("remix-userid") != "" || seen.Get("remix-userkey") != "" {
		t.Errorf("redirect forwarded remix headers: %v", seen)
	}
	// Cookies are also dropped from the copied header set. The jar is
	// separately scoped by host and Secure flag, so in production no
	// cookie is re-added for an off-whitelist target either.
	var req http.Request
	req.Header = http.Header{"Cookie": {"remix_userkey=secret"}}
	req.Header.Set("remix-userid", "7")
	stripEAPICredentials(&req)
	if req.Header.Get("Cookie") != "" || req.Header.Get("remix-userid") != "" {
		t.Errorf("stripEAPICredentials left credentials: %v", req.Header)
	}
}

func TestFetchBook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/eapi/book/115066162/e9f13b" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"success":1,"book":{"title":"Dune","author":"Frank Herbert"}}`)
	}))
	defer srv.Close()

	book, err := testClient(t, srv.URL).FetchBook(context.Background(), "115066162", "e9f13b")
	if err != nil {
		t.Fatalf("FetchBook: %v", err)
	}
	if book.ID != "115066162" || book.Hash != "e9f13b" {
		t.Errorf("fallback identity = %+v", book)
	}
}

func TestResolveFileURL(t *testing.T) {
	cdn := "https://cdn.example/book.epub"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/eapi/book/1/h/file" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"success":1,"file":{"downloadLink":"`+cdn+`"}}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	got, err := c.resolveEAPIFileURL(context.Background(), srv.URL+"/eapi/book/1/h/file")
	if err != nil {
		t.Fatalf("resolveEAPIFileURL: %v", err)
	}
	if got != cdn {
		t.Errorf("link = %q, want %q", got, cdn)
	}

	// Already-resolved CDN links pass straight through.
	if got, err := c.resolveEAPIFileURL(context.Background(), cdn); err != nil || got != cdn {
		t.Errorf("passthrough = %q, %v", got, err)
	}
}

func TestFetchProfile(t *testing.T) {
	srv := httptest.NewServer(jsonHandler(200, `{
		"success": 1,
		"user": {
			"id": "42", "name": "Reader", "email": "r@example.com",
			"downloads_today": 3, "downloads_limit": 10,
			"isPremium": 1, "confirmed": true,
			"personalDomains": ["d1.example", "d2.example"]
		}
	}`))
	defer srv.Close()

	profile, err := testClient(t, srv.URL).FetchProfile(context.Background())
	if err != nil {
		t.Fatalf("FetchProfile: %v", err)
	}
	if profile.DownloadsTotal != 10 || profile.Downloads != 3 || profile.DownloadsLeft != 7 {
		t.Errorf("quota = %+v", profile)
	}
	if !profile.QuotaKnown {
		t.Error("QuotaKnown is false although the limit field is present")
	}
	if profile.Premium == nil || !*profile.Premium || profile.Confirmed == nil || !*profile.Confirmed {
		t.Errorf("flags = %+v", profile)
	}
	if len(profile.PersonalDomains) != 2 {
		t.Errorf("personal domains = %v", profile.PersonalDomains)
	}
}

func TestFetchProfileWithoutQuota(t *testing.T) {
	srv := httptest.NewServer(jsonHandler(200, `{"success":1,"user":{"id":"42"}}`))
	defer srv.Close()

	profile, err := testClient(t, srv.URL).FetchProfile(context.Background())
	if err != nil {
		t.Fatalf("FetchProfile: %v", err)
	}
	if profile.QuotaKnown {
		t.Error("QuotaKnown is true without a limit field; zero is not 'exhausted'")
	}
}

func TestISBNValidation(t *testing.T) {
	cases := []struct {
		in10, in13, want10, want13 string
	}{
		{"059309932X", "", "059309932X", "9780593099322"},
		{"", "9780593099322", "059309932X", "9780593099322"},
		{"0593099320", "", "", ""}, // bad check digit -> dropped, not derived
		{"", "123", "", ""},
	}
	for _, tc := range cases {
		got10, got13 := normalizeISBN("", tc.in10, tc.in13)
		if got10 != tc.want10 || got13 != tc.want13 {
			t.Errorf("normalizeISBN(%q, %q) = %q, %q; want %q, %q", tc.in10, tc.in13, got10, got13, tc.want10, tc.want13)
		}
	}
}
