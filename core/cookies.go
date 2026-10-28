package core

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ParseNetscapeCookies accepts only root path cookies (the session key is
// root-scoped; importing deeper cookies would widen their reach), drops
// expired and Secure-mismatched entries, and requires the
// remix_userid/remix_userkey pair to be complete.
func ParseNetscapeCookies(data []byte, targetOrigin string, now time.Time) (map[string]string, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	// curl writes "# Netscape HTTP Cookie File"; some tools write
	// "# HTTP Cookie File". Both are Netscape records.
	if len(lines) == 0 || !strings.Contains(lines[0], "HTTP Cookie File") {
		return nil, fmt.Errorf("cookie file is not in Netscape format (missing \"# HTTP Cookie File\" header)")
	}

	parsed, err := url.Parse(strings.TrimSpace(targetOrigin))
	if err != nil || parsed.Hostname() == "" {
		return nil, fmt.Errorf("invalid target domain %q", targetOrigin)
	}
	targetHost := strings.ToLower(parsed.Hostname())
	targetSecure := parsed.Scheme == "https"

	out := map[string]string{}
	for _, line := range lines[1:] {
		line = strings.TrimSuffix(line, "\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		// HttpOnly cookies are marked with a pseudo-comment prefix that
		// belongs to the record, not to the file's comments.
		if strings.HasPrefix(line, "#HttpOnly_") {
			line = strings.TrimPrefix(line, "#HttpOnly_")
		} else if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 7 {
			// Tolerate foreign lines rather than failing the import, but
			// never guess at a malformed credential record.
			continue
		}
		domain, includeSub, cookiePath, secure, expires, name, value :=
			fields[0], fields[1], fields[2], fields[3], fields[4], fields[5], fields[6]

		cookieDomain := strings.ToLower(strings.TrimPrefix(domain, "."))
		if cookieDomain != targetHost && !(strings.EqualFold(includeSub, "TRUE") && strings.HasSuffix(targetHost, "."+cookieDomain)) {
			continue
		}
		if cookiePath != "" && cookiePath != "/" {
			continue
		}
		if strings.EqualFold(secure, "TRUE") && !targetSecure {
			continue
		}
		if exp, err := strconv.ParseInt(strings.TrimSpace(expires), 10, 64); err == nil {
			if exp > 0 && now.Unix() >= exp {
				continue
			}
		}
		if strings.TrimSpace(name) == "" {
			continue
		}
		out[strings.TrimSpace(name)] = value
	}

	if out["remix_userid"] == "" || out["remix_userkey"] == "" {
		return nil, fmt.Errorf("cookie file does not contain both remix_userid and remix_userkey for %s", targetHost)
	}
	return out, nil
}

// LoadNetscapeCookiesFile reads path and parses it for targetOrigin.
func LoadNetscapeCookiesFile(path, targetOrigin string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read cookie file: %w", err)
	}
	return ParseNetscapeCookies(data, targetOrigin, time.Now())
}

// ImportCookiesFile is the cookie-based login path used when EAPI password
// login is disabled upstream. An empty domain uses the current mirror.
func (b *Backend) ImportCookiesFile(path, domain string) (*Session, error) {
	b.loginMu.Lock()
	defer b.loginMu.Unlock()

	domain = strings.TrimSpace(domain)
	if domain == "" {
		domain = b.client.Domain()
	}
	parsed, err := ParseAllowedDomain(domain)
	if err != nil {
		return nil, err
	}
	cookies, err := LoadNetscapeCookiesFile(path, parsed.String())
	if err != nil {
		return nil, err
	}

	if err := b.client.SetDomain(parsed.String()); err != nil {
		return nil, err
	}
	b.client.SetCookies(cookies)

	saved := &SessionFile{
		Cookies: b.client.Cookies(),
		Domain:  b.client.Domain(),
		Mode:    "eapi",
	}
	if err := SaveSession(saved); err != nil {
		b.client.Logout()
		return nil, fmt.Errorf("imported cookies but could not save the session: %w", err)
	}
	_ = PersistDotEnv(EnvDomain, b.client.Domain())

	email := b.LastLoginEmail()
	name := localPart(email)
	if name == "" {
		name = "Cookie 会话"
	}
	session := &Session{Email: email, Name: name}
	b.sessionMu.Lock()
	b.session = session
	b.sessionMu.Unlock()
	return session, nil
}
