package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func useTempConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(EnvConfigDir, dir)
	return dir
}

func TestSessionRoundTrip(t *testing.T) {
	useTempConfigDir(t)

	if _, err := LoadSession(); err == nil {
		t.Fatal("LoadSession succeeded without a file")
	}

	want := &SessionFile{
		Cookies: map[string]string{"remix_userid": "7", "remix_userkey": "k"},
		Domain:  "https://z-lib.gd",
		Mode:    "eapi",
		Name:    "书虫",
	}
	if err := SaveSession(want); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	got, err := LoadSession()
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got.Domain != want.Domain || got.Cookies["remix_userkey"] != "k" || got.Name != "书虫" {
		t.Errorf("session = %+v", got)
	}

	removed, err := DeleteSession()
	if err != nil || !removed {
		t.Fatalf("DeleteSession = %v, %v", removed, err)
	}
	if removed, err := DeleteSession(); err != nil || removed {
		t.Errorf("second DeleteSession = %v, %v", removed, err)
	}
}

func TestAppConfigRoundTrip(t *testing.T) {
	useTempConfigDir(t)

	cfg := LoadAppConfig()
	if cfg.LastLoginEmail != "" {
		t.Errorf("new config = %+v", cfg)
	}
	cfg.LastLoginEmail = "reader@example.com"
	cfg.GUI.Theme = "Gruvbox Dark"
	if err := SaveAppConfig(cfg); err != nil {
		t.Fatalf("SaveAppConfig: %v", err)
	}
	got := LoadAppConfig()
	if got.LastLoginEmail != "reader@example.com" || got.GUI.Theme != "Gruvbox Dark" {
		t.Errorf("config = %+v", got)
	}
}

func TestPersistDotEnvPreservesOtherLines(t *testing.T) {
	useTempConfigDir(t)
	path := DotEnvPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := "# keep me\nexport ZLIB_PROXY=\"http://proxy:8080\"\nZLIB_DOMAIN=\"https://old.example\"\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := PersistDotEnv(EnvDomain, "https://z-lib.gd"); err != nil {
		t.Fatalf("PersistDotEnv: %v", err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	for _, want := range []string{"# keep me", `ZLIB_PROXY="http://proxy:8080"`, `ZLIB_DOMAIN="https://z-lib.gd"`} {
		if !strings.Contains(text, want) {
			t.Errorf("dotenv lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "old.example") {
		t.Errorf("old value survived:\n%s", text)
	}
	if strings.Count(text, "ZLIB_DOMAIN=") != 1 {
		t.Errorf("ZLIB_DOMAIN duplicated:\n%s", text)
	}

	if err := PersistDotEnv(EnvDomain, ""); err != nil {
		t.Fatalf("PersistDotEnv(empty): %v", err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "ZLIB_DOMAIN") {
		t.Errorf("empty value did not remove the line:\n%s", data)
	}
	if !strings.Contains(string(data), "# keep me") {
		t.Errorf("comments were lost:\n%s", data)
	}
}

func TestParseDotEnvLine(t *testing.T) {
	cases := []struct {
		line string
		key  string
		val  string
		ok   bool
	}{
		{`ZLIB_DOMAIN="https://z-lib.gd"`, "ZLIB_DOMAIN", "https://z-lib.gd", true},
		{`export ZLIB_PROXY='socks5://p:1080'`, "ZLIB_PROXY", "socks5://p:1080", true},
		{`ZPWD="a\"b\$c"`, "ZPWD", `a"b$c`, true},
		{"# comment", "", "", false},
		{"not a pair", "", "", false},
	}
	for _, tc := range cases {
		key, val, ok := parseDotEnvLine(tc.line)
		if ok != tc.ok || key != tc.key || val != tc.val {
			t.Errorf("parseDotEnvLine(%q) = %q, %q, %v; want %q, %q, %v",
				tc.line, key, val, ok, tc.key, tc.val, tc.ok)
		}
	}
}
