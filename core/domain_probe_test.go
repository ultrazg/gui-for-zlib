package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeHealthyEAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != eapiProbePath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":1}`)
	}))
	defer srv.Close()

	result := ProbeDomain(context.Background(), srv.URL, DomainProbeOptions{Path: eapiProbePath})
	if result.State != DomainHealthy {
		t.Fatalf("state = %s (%s)", result.State, result.Err)
	}
	if result.FinalURL != srv.URL+eapiProbePath {
		t.Errorf("FinalURL = %q", result.FinalURL)
	}
}

func TestProbeRejectsHTMLOnEAPIPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html>not json</html>")
	}))
	defer srv.Close()

	result := ProbeDomain(context.Background(), srv.URL, DomainProbeOptions{Path: eapiProbePath})
	if result.State != DomainHTTPError {
		t.Fatalf("state = %s, want http_error", result.State)
	}
}

func TestProbeChallengePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<html><script>var hash="x"; var c_token="y";</script></html>`)
	}))
	defer srv.Close()

	result := ProbeDomain(context.Background(), srv.URL, DomainProbeOptions{Path: "/"})
	if result.State != DomainChallenged {
		t.Fatalf("state = %s, want challenged", result.State)
	}
}

func TestProbeDiamWallTitle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><title>DiamWall</title></html>")
	}))
	defer srv.Close()

	result := ProbeDomain(context.Background(), srv.URL, DomainProbeOptions{Path: "/"})
	if result.State != DomainDiamWallBlocked {
		t.Fatalf("state = %s, want diamwall_blocked", result.State)
	}
}

func TestProbeRedirectLoop(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+r.URL.Path, http.StatusFound)
	}))
	defer srv.Close()

	result := ProbeDomain(context.Background(), srv.URL, DomainProbeOptions{Path: "/"})
	if result.State != DomainRedirectLoop {
		t.Fatalf("state = %s (%s), want redirect_loop", result.State, result.Err)
	}
}

// TestProbeDiamWallCookieIsClearance verifies that a __diamwall cookie
// marks a request that got THROUGH, so a chain carrying it must not be
// reported as a plain redirect loop.
func TestProbeDiamWallCookieIsClearance(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "__diamwall", Value: "clearance"})
		http.Redirect(w, r, srv.URL+r.URL.Path, http.StatusFound)
	}))
	defer srv.Close()

	result := ProbeDomain(context.Background(), srv.URL, DomainProbeOptions{Path: "/"})
	if result.State != DomainDiamWallBlocked {
		t.Fatalf("state = %s (%s), want diamwall_blocked", result.State, result.Err)
	}
}

func TestProbeFollowsRedirector(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":1}`)
	}))
	defer final.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL+"/eapi/info/domains", http.StatusFound)
	}))
	defer redirector.Close()

	result := ProbeDomain(context.Background(), redirector.URL, DomainProbeOptions{Path: eapiProbePath})
	if result.State != DomainHealthy {
		t.Fatalf("state = %s (%s)", result.State, result.Err)
	}
	if result.FinalURL != final.URL+"/eapi/info/domains" {
		t.Errorf("FinalURL = %q, want the redirector's target", result.FinalURL)
	}
}

func TestProbeInvalidDomain(t *testing.T) {
	for _, raw := range []string{"", "not a url", "ftp://example.com", "https://"} {
		result := ProbeDomain(context.Background(), raw, DomainProbeOptions{Path: "/"})
		if result.State != DomainInvalid && result.State != DomainNetworkError {
			t.Errorf("ProbeDomain(%q) state = %s, want invalid/network error", raw, result.State)
		}
	}
	if result := ProbeDomain(context.Background(), "https://z-lib.gd", DomainProbeOptions{Path: "no-slash"}); result.State != DomainInvalid {
		t.Errorf("invalid path state = %s", result.State)
	}
}

func TestProbeDomainsPreservesOrder(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"success":1}`)
	}))
	defer ok.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer bad.Close()

	results := ProbeDomains(context.Background(), []string{ok.URL, bad.URL}, DomainProbeOptions{Path: eapiProbePath})
	if len(results) != 2 || results[0].Domain != ok.URL || results[1].Domain != bad.URL {
		t.Fatalf("order = %+v", results)
	}
	if results[0].State != DomainHealthy || results[1].State != DomainHTTPError {
		t.Errorf("states = %s, %s", results[0].State, results[1].State)
	}
}
