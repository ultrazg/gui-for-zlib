package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReleaseVersionComparison(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v0.10.0", "0.9.0", 1},
		{"1.0.0", "v0.99.99", 1},
		{"v1.2.10", "1.2.9", 1},
		{"1.2.3", "v1.2.3", 0},
		{"1.2.3+build.9", "1.2.3+build.10", 0},
		{"v1.2.3", "1.2.3-rc.10", 1},
		{"1.2.3-rc.10", "1.2.3-rc.9", 1},
		{"1.2.3-alpha.1", "1.2.3-alpha", 1},
		{"1.2.3-beta", "1.2.3-alpha.9", 1},
		{"1.2.3-alpha.beta", "1.2.3-alpha.9", 1},
		{"1.2.3-1", "1.2.3-alpha", -1},
		{"1.2.3-rc.1", "1.2.4-alpha", -1},
		{"999999999999999999999.0.0", "99999999999999999999.0.0", 1},
	}
	for _, tc := range tests {
		t.Run(tc.a+"/"+tc.b, func(t *testing.T) {
			a, err := parseReleaseVersion(tc.a)
			if err != nil {
				t.Fatal(err)
			}
			b, err := parseReleaseVersion(tc.b)
			if err != nil {
				t.Fatal(err)
			}
			if got := a.compare(b); got != tc.want {
				t.Errorf("compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
			if got := b.compare(a); got != -tc.want {
				t.Errorf("reverse comparison = %d, want %d", got, -tc.want)
			}
		})
	}
	for _, invalid := range []string{"", "dev", "latest", "1.2", "1.2.3.4", "v01.2.3", "1.02.3", "1.2.3-01", "1.2.3-rc.01", "1.2.3+", "1.2.3-", "1.2.3-rc..1"} {
		if _, err := parseReleaseVersion(invalid); err == nil {
			t.Errorf("accepted invalid version %q", invalid)
		}
	}
}

func TestCheckForUpdate(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		current string
		wantTag string
		wantErr string
	}{
		{"newer", 200, `{"tag_name":"v0.10.0","name":"新版本","body":"## 更新\n- 修复下载"}`, "0.9.0", "v0.10.0", ""},
		{"same", 200, `{"tag_name":"v0.9.0"}`, "0.9.0", "", ""},
		{"older", 200, `{"tag_name":"v0.8.0"}`, "0.9.0", "", ""},
		{"stable over rc", 200, `{"tag_name":"v1.0.0"}`, "1.0.0-rc.1", "v1.0.0", ""},
		{"no notes", 200, `{"tag_name":"v1.0.0","body":null}`, "0.9.0", "v1.0.0", ""},
		{"bad tag", 200, `{"tag_name":"latest"}`, "0.9.0", "", "发布标签无效"},
		{"missing tag", 200, `{}`, "0.9.0", "", "发布标签无效"},
		{"bad json", 200, `<html>error</html>`, "0.9.0", "", "解析"},
		{"trailing json", 200, `{"tag_name":"v1.0.0"}{}`, "0.9.0", "", "解析"},
		{"draft", 200, `{"tag_name":"v1.0.0","draft":true}`, "0.9.0", "", "正式发布版本"},
		{"prerelease", 200, `{"tag_name":"v1.0.0-rc.1","prerelease":true}`, "0.9.0", "", "正式发布版本"},
		{"no release", 404, `{}`, "0.9.0", "", "暂无"},
		{"rate limit", 403, `{}`, "0.9.0", "", "请求受限"},
		{"too many requests", 429, `{}`, "0.9.0", "", "请求受限"},
		{"server error", 503, `{}`, "0.9.0", "", "HTTP 503"},
		{"too large", 200, strings.Repeat(" ", maxReleaseBytes+1), "0.9.0", "", "过大"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/repos/ultrazg/gui-for-zlib/releases/latest" {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("User-Agent") != "gui-for-zlib/"+tc.current {
					t.Errorf("request headers = %v", r.Header)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			release, err := checkForUpdate(context.Background(), srv.Client(), srv.URL+"/repos/ultrazg/gui-for-zlib/releases/latest", tc.current)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || release != nil {
					t.Fatalf("release = %+v, err = %v, want %q", release, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantTag == "" {
				if release != nil {
					t.Fatalf("unexpected update: %+v", release)
				}
				return
			}
			if release == nil || release.TagName != tc.wantTag || release.URL() != ProjectURL+"/releases/tag/"+tc.wantTag {
				t.Fatalf("release = %+v, want %s", release, tc.wantTag)
			}
			if tc.name == "newer" && (release.Name != "新版本" || release.Body != "## 更新\n- 修复下载") {
				t.Fatalf("release notes not preserved: %+v", release)
			}
		})
	}
}

func TestCheckForUpdateInvalidCurrentAndCancellation(t *testing.T) {
	// An invalid current version must fail before making any request.
	if _, err := checkForUpdate(context.Background(), nil, latestReleaseURL, "dev"); err == nil || !strings.Contains(err.Error(), "当前版本无效") {
		t.Fatalf("invalid current version: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := checkForUpdate(ctx, http.DefaultClient, latestReleaseURL, "0.1.0")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled check: %v", err)
	}
}
