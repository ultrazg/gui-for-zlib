package core

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// allowedDomainSuffixes is the single source of truth for hosts this client
// will talk to (measured 2026-09-04). Keep it a var so tests can extend it;
// production code treats it as constant.
var allowedDomainSuffixes = []string{
	"1lib.sk",
	"article.sk",
	"articles.sk",
	"z-lib.fm",
	"z-lib.gd",
	"z-lib.gl",
	"z-lib.sk",
	"z-library.ec",
	"zlib.bz",
	"zliba.ru",
}

// AllowedDomainSuffixes returns a copy of the mirror suffix list.
func AllowedDomainSuffixes() []string {
	out := make([]string, len(allowedDomainSuffixes))
	copy(out, allowedDomainSuffixes)
	return out
}

// KnownDomains returns the full candidate list used by doctor.
func KnownDomains() []string {
	out := make([]string, 0, len(allowedDomainSuffixes))
	for _, suffix := range allowedDomainSuffixes {
		out = append(out, "https://"+suffix)
	}
	return out
}

// hostAllowed reports whether host equals a whitelisted suffix or is a
// subdomain of one.
func hostAllowed(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, suffix := range allowedDomainSuffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// ParseAllowedDomain validates a user-supplied mirror URL. Rules: HTTPS, no
// user info, no port, no query/fragment, path empty or "/", host in the
// suffix list. It returns the normalized origin (https://host).
func ParseAllowedDomain(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty domain")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid domain %q: %w", raw, err)
	}
	switch {
	case u.Scheme != "https":
		return nil, fmt.Errorf("domain %q must use https", raw)
	case u.User != nil:
		return nil, fmt.Errorf("domain %q must not contain user info", raw)
	case u.Port() != "":
		return nil, fmt.Errorf("domain %q must not contain a port", raw)
	case u.RawQuery != "" || u.Fragment != "":
		return nil, fmt.Errorf("domain %q must not contain query or fragment", raw)
	case u.Path != "" && u.Path != "/":
		return nil, fmt.Errorf("domain %q must be an origin without a path", raw)
	case u.Hostname() == "":
		return nil, fmt.Errorf("domain %q has no host", raw)
	}
	if !hostAllowed(u.Hostname()) {
		return nil, fmt.Errorf("domain %q is not in the mirror whitelist", raw)
	}
	return &url.URL{Scheme: "https", Host: strings.ToLower(u.Hostname())}, nil
}

// isAllowedURL reports whether requests to u may carry session credentials.
// Used by the redirect guard: off-list or downgraded (http) targets must not
// see remix headers or cookies.
func isAllowedURL(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && hostAllowed(u.Hostname())
}

// parseProxyURL validates $ZLIB_PROXY / --proxy. Accepted schemes:
// http, https, socks5, socks5h; no path, query or fragment. socks5h is
// mapped onto socks5, which already asks the proxy to resolve names.
func parseProxyURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%w: empty", ErrInvalidProxy)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidProxy, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	case "socks5", "socks5h":
		u.Scheme = "socks5"
	default:
		return nil, fmt.Errorf("%w: unsupported scheme %q", ErrInvalidProxy, u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%w: missing host", ErrInvalidProxy)
	}
	if u.Path != "" && u.Path != "/" {
		return nil, fmt.Errorf("%w: must not contain a path", ErrInvalidProxy)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("%w: must not contain query or fragment", ErrInvalidProxy)
	}
	u.Path = ""
	return u, nil
}

// ProxyURLFromParts assembles scheme://host:port from the settings-page
// fields and validates the result through parseProxyURL. scheme defaults to
// socks5; addr may be a bare IPv6 address (e.g. ::1).
func ProxyURLFromParts(scheme, addr, port string) (string, error) {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if scheme == "" {
		scheme = "socks5"
	}
	addr = strings.TrimSpace(addr)
	port = strings.TrimSpace(port)
	if addr == "" {
		return "", fmt.Errorf("%w: 地址不能为空", ErrInvalidProxy)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("%w: 端口需为 1–65535", ErrInvalidProxy)
	}
	// Accept "::1" or "[::1]".
	addr = strings.TrimPrefix(strings.TrimSuffix(addr, "]"), "[")
	parsed, err := parseProxyURL((&url.URL{Scheme: scheme, Host: net.JoinHostPort(addr, port)}).String())
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}
