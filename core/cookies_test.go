package core

import (
	"strings"
	"testing"
	"time"
)

const cookieHeader = "# HTTP Cookie File\n"

func TestParseNetscapeCookies(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := cookieHeader +
		// accepted: host match, root path
		"z-lib.gd\tTRUE\t/\tTRUE\t0\tremix_userid\t7\n" +
		// accepted: HttpOnly prefix
		"#HttpOnly_z-lib.gd\tTRUE\t/\tTRUE\t0\tremix_userkey\tkey\n" +
		// accepted: subdomain cookie applicable to the parent
		"z-lib.gd\tTRUE\t/\tTRUE\t0\tstyle\tdark\n" +
		// rejected: different domain
		"other.example\tTRUE\t/\tTRUE\t0\tremix_userkey\tstolen\n" +
		// rejected: path-scoped cookie
		"z-lib.gd\tTRUE\t/eapi\tTRUE\t0\tpathcookie\tx\n" +
		// rejected: expired
		"z-lib.gd\tTRUE\t/\tTRUE\t1000\told\tx\n" +
		// rejected: malformed record
		"z-lib.gd\tTRUE\t/\tTRUE\t0\tbroken\n"

	cookies, err := ParseNetscapeCookies([]byte(body), "https://z-lib.gd", now)
	if err != nil {
		t.Fatalf("ParseNetscapeCookies: %v", err)
	}
	if cookies["remix_userid"] != "7" || cookies["remix_userkey"] != "key" {
		t.Errorf("credentials = %v", cookies)
	}
	if cookies["style"] != "dark" {
		t.Errorf("host cookie missing: %v", cookies)
	}
	for _, banned := range []string{"pathcookie", "old", "broken"} {
		if _, ok := cookies[banned]; ok {
			t.Errorf("cookie %q should have been filtered out", banned)
		}
	}
}

func TestParseNetscapeCookiesSubdomainMatch(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := cookieHeader +
		"z-lib.gd\tTRUE\t/\tTRUE\t0\tremix_userid\t7\n" +
		"z-lib.gd\tTRUE\t/\tTRUE\t0\tremix_userkey\tkey\n"

	cookies, err := ParseNetscapeCookies([]byte(body), "https://api.z-lib.gd", now)
	if err != nil {
		t.Fatalf("subdomain import failed: %v", err)
	}
	if cookies["remix_userkey"] != "key" {
		t.Errorf("cookies = %v", cookies)
	}
}

func TestParseNetscapeCookiesRejectsIncompletePair(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := cookieHeader + "z-lib.gd\tTRUE\t/\tTRUE\t0\tremix_userid\t7\n"
	_, err := ParseNetscapeCookies([]byte(body), "https://z-lib.gd", now)
	if err == nil || !strings.Contains(err.Error(), "remix_userkey") {
		t.Fatalf("err = %v, want a missing-key error", err)
	}
}

func TestParseNetscapeCookiesRejectsForeignFormat(t *testing.T) {
	_, err := ParseNetscapeCookies([]byte("z-lib.gd\tTRUE\t/\tTRUE\t0\tremix_userid\t7\n"), "https://z-lib.gd", time.Now())
	if err == nil {
		t.Fatal("a file without the Netscape header was accepted")
	}
}
