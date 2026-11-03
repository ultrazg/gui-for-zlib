package core

import (
	"os"
	"strings"
	"sync"
)

// Environment keys and the default mirror. Values are read at call time,
// never in init: the GUI loads .env files after the process starts, so a
// cached constant would miss them.
const (
	EnvDomain      = "ZLIB_DOMAIN"
	EnvProxy       = "ZLIB_PROXY"
	EnvRetries     = "ZLIB_DOWNLOAD_RETRIES"
	EnvDownloadDir = "ZLIB_DOWNLOAD_DIR"
	EnvTheme       = "ZLIB_THEME"
	EnvConfigDir   = "ZLIB_CONFIG_DIR"

	DefaultDomain = "https://z-lib.gd"
)

var (
	defaultDomainMu  sync.RWMutex
	defaultDomain    string
	defaultDomainSet bool
)

// SetDefaultDomain overrides the built-in default for this process. It is
// the highest-priority source in CurrentDefaultDomain.
func SetDefaultDomain(domain string) {
	defaultDomainMu.Lock()
	defaultDomain = normalizeDomain(domain)
	defaultDomainSet = domain != ""
	defaultDomainMu.Unlock()
}

// CurrentDefaultDomain resolves, at call time:
// SetDefaultDomain override > $ZLIB_DOMAIN > DefaultDomain.
func CurrentDefaultDomain() string {
	defaultDomainMu.RLock()
	override, set := defaultDomain, defaultDomainSet
	defaultDomainMu.RUnlock()
	if set {
		return override
	}
	if env := strings.TrimSpace(os.Getenv(EnvDomain)); env != "" {
		return normalizeDomain(env)
	}
	return DefaultDomain
}

// normalizeDomain only trims whitespace and a trailing slash, matching the
// upstream helper. Validation lives in ParseAllowedDomain.
func normalizeDomain(domain string) string {
	return strings.TrimRight(strings.TrimSpace(domain), "/")
}

// RegistrationURL returns the sign-up page on a mirror, falling back to the
// current default mirror when domain is empty.
func RegistrationURL(domain string) string {
	if domain == "" {
		domain = CurrentDefaultDomain()
	}
	return domain
}
