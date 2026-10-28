package core

import "testing"

func TestParseAllowedDomain(t *testing.T) {
	valid := map[string]string{
		"https://z-lib.gd":      "https://z-lib.gd",
		"https://z-lib.gd/":     "https://z-lib.gd",
		" https://z-lib.gd ":    "https://z-lib.gd",
		"https://api.z-lib.gd":  "https://api.z-lib.gd",
		"https://Z-LIB.GD":      "https://z-lib.gd",
		"https://z-library.ec/": "https://z-library.ec",
	}
	for raw, want := range valid {
		got, err := ParseAllowedDomain(raw)
		if err != nil {
			t.Fatalf("ParseAllowedDomain(%q): %v", raw, err)
		}
		if got.String() != want {
			t.Errorf("ParseAllowedDomain(%q) = %q, want %q", raw, got.String(), want)
		}
	}

	invalid := []string{
		"",
		"http://z-lib.gd",
		"https://evil.example",
		"https://z-lib.gd.evil.example",
		"https://z-lib.gd:8443",
		"https://user:pass@z-lib.gd",
		"https://z-lib.gd/path",
		"https://z-lib.gd/?q=1",
		"https://z-lib.gd/#frag",
	}
	for _, raw := range invalid {
		if _, err := ParseAllowedDomain(raw); err == nil {
			t.Errorf("ParseAllowedDomain(%q) accepted an invalid domain", raw)
		}
	}
}

func TestParseProxyURL(t *testing.T) {
	cases := map[string]string{
		"http://127.0.0.1:8080":        "http://127.0.0.1:8080",
		"https://proxy.example:443":    "https://proxy.example:443",
		"socks5://proxy.example:1080":  "socks5://proxy.example:1080",
		"socks5h://proxy.example:1080": "socks5://proxy.example:1080",
	}
	for raw, want := range cases {
		got, err := parseProxyURL(raw)
		if err != nil {
			t.Fatalf("parseProxyURL(%q): %v", raw, err)
		}
		if got.String() != want {
			t.Errorf("parseProxyURL(%q) = %q, want %q", raw, got.String(), want)
		}
	}
	for _, raw := range []string{"", "ftp://x:1", "http://host/path", "http://host?q=1"} {
		if _, err := parseProxyURL(raw); err == nil {
			t.Errorf("parseProxyURL(%q) accepted an invalid proxy", raw)
		}
	}
}

func TestKnownDomains(t *testing.T) {
	domains := KnownDomains()
	if len(domains) != len(allowedDomainSuffixes) {
		t.Fatalf("KnownDomains has %d entries, want %d", len(domains), len(allowedDomainSuffixes))
	}
	for _, d := range domains {
		if _, err := ParseAllowedDomain(d); err != nil {
			t.Errorf("known domain %q does not parse: %v", d, err)
		}
	}
}
