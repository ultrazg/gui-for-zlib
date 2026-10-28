package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client is the HTTP session layer. It owns the mirror domain, the session
// cookies and the redirect policy. Everything above it (EAPI, downloads)
// is built on eapiRequest / newDownloadRequest.
type Client struct {
	httpClient *http.Client
	transport  *http.Transport

	// jar is a stable wrapper whose backing store can be swapped on
	// SetDomain / Logout. Keeping the pointer stable means requests in
	// flight never race with the http.Client's Jar field.
	jar *sessionJar

	domainMu  sync.RWMutex
	domain    string
	domainURL *url.URL

	cookiesMu sync.RWMutex
	cookies   map[string]string
	loggedIn  bool

	// proxyMu guards proxyURL. Transport.Proxy is set once to proxyFor,
	// which reads the current URL under the read lock, so requests never
	// race with SetProxy on the Proxy field itself.
	proxyMu  sync.RWMutex
	proxyURL *url.URL // nil = 直连

	downloadRetries int
}

// sessionJar is an http.CookieJar with a replaceable inner jar.
type sessionJar struct {
	mu  sync.RWMutex
	jar *cookiejar.Jar
}

func newSessionJar() *sessionJar {
	jar, _ := cookiejar.New(nil)
	return &sessionJar{jar: jar}
}

func (j *sessionJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.jar.SetCookies(u, cookies)
}

func (j *sessionJar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.jar.Cookies(u)
}

func (j *sessionJar) reset() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.jar, _ = cookiejar.New(nil)
}

// clientOptions collects NewClient settings before the client exists.
type clientOptions struct {
	domain string
	proxy  string
}

// ClientOption configures NewClient.
type ClientOption func(*clientOptions)

// WithDomain overrides the mirror domain. Explicit options win over the
// environment, which is applied first.
func WithDomain(domain string) ClientOption {
	return func(o *clientOptions) { o.domain = domain }
}

// WithProxy sets an HTTP(S) or SOCKS5 proxy. Explicit options win over
// $ZLIB_PROXY.
func WithProxy(proxy string) ClientOption {
	return func(o *clientOptions) { o.proxy = proxy }
}

// NewClient builds a session. Precedence for both domain and proxy is:
// explicit option > environment > built-in default.
func NewClient(opts ...ClientOption) *Client {
	o := clientOptions{domain: CurrentDefaultDomain()}
	if envProxy := strings.TrimSpace(os.Getenv(EnvProxy)); envProxy != "" {
		o.proxy = envProxy
	}
	for _, fn := range opts {
		fn(&o)
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil

	jar := newSessionJar()
	c := &Client{
		transport:       tr,
		jar:             jar,
		cookies:         map[string]string{},
		downloadRetries: parseDownloadRetries(os.Getenv(EnvRetries)),
	}
	if o.proxy != "" {
		if proxyURL, err := parseProxyURL(o.proxy); err == nil {
			c.proxyURL = proxyURL
		}
	}
	// The Proxy field is wired once to a closure so SetProxy can swap the
	// URL behind it at runtime without touching the transport's field.
	tr.Proxy = c.proxyFor
	c.httpClient = &http.Client{
		Transport:     tr,
		Jar:           jar,
		Timeout:       defaultTimeout,
		CheckRedirect: c.checkRedirect,
	}
	_ = c.SetDomain(o.domain)
	return c
}

// proxyFor reports the current proxy for one new connection. It is assigned
// to Transport.Proxy exactly once, so SetProxy only swaps the URL behind it.
func (c *Client) proxyFor(*http.Request) (*url.URL, error) {
	c.proxyMu.RLock()
	defer c.proxyMu.RUnlock()
	return c.proxyURL, nil
}

// SetProxy switches the proxy at runtime. An empty value clears back to
// direct connections. A malformed proxy is rejected via parseProxyURL and
// leaves the current value untouched. CloseIdleConnections drops pooled
// keep-alive connections so the next dial honors the change; requests
// already in flight finish on their old connection.
func (c *Client) SetProxy(proxy string) error {
	proxy = strings.TrimSpace(proxy)
	var parsed *url.URL
	if proxy != "" {
		var err error
		if parsed, err = parseProxyURL(proxy); err != nil {
			return err
		}
	}
	c.proxyMu.Lock()
	c.proxyURL = parsed
	c.proxyMu.Unlock()
	c.transport.CloseIdleConnections()
	return nil
}

// Proxy returns the current proxy in canonical URL form, or "" when direct.
func (c *Client) Proxy() string {
	c.proxyMu.RLock()
	defer c.proxyMu.RUnlock()
	if c.proxyURL == nil {
		return ""
	}
	return c.proxyURL.String()
}

// parseDownloadRetries reads $ZLIB_DOWNLOAD_RETRIES at construction time.
// Invalid values fall back to 3; 0 disables retries.
func parseDownloadRetries(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultDownloadRetries
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return DefaultDownloadRetries
	}
	return n
}

// Domain returns the current mirror origin, e.g. https://z-lib.gd.
func (c *Client) Domain() string {
	domain, _ := c.domainSnapshot()
	return domain
}

// domainSnapshot returns the origin and its parsed form atomically.
func (c *Client) domainSnapshot() (string, *url.URL) {
	c.domainMu.RLock()
	defer c.domainMu.RUnlock()
	return c.domain, c.domainURL
}

// LoggedIn reports whether a session key is present.
func (c *Client) LoggedIn() bool {
	c.cookiesMu.RLock()
	defer c.cookiesMu.RUnlock()
	return c.loggedIn
}

// SetDomain switches the mirror and re-seeds the existing cookies into the
// new domain's jar. Order matters at login: SetDomain first, then
// SetCookies.
func (c *Client) SetDomain(domain string) error {
	parsed, err := ParseAllowedDomain(normalizeDomain(domain))
	if err != nil {
		return err
	}
	c.setDomainURL(parsed)
	return nil
}

// setDomainURL applies an already-validated origin.
func (c *Client) setDomainURL(u *url.URL) {
	c.domainMu.Lock()
	c.domain = u.Scheme + "://" + u.Host
	c.domainURL = &url.URL{Scheme: u.Scheme, Host: u.Host}
	c.domainMu.Unlock()
	c.reseedCookies()
}

func (c *Client) reseedCookies() {
	c.cookiesMu.Lock()
	snapshot := make(map[string]string, len(c.cookies))
	for k, v := range c.cookies {
		snapshot[k] = v
	}
	c.cookiesMu.Unlock()
	c.resetJar(snapshot)
}

func (c *Client) resetJar(cookies map[string]string) {
	c.jar.reset()
	_, origin := c.domainSnapshot()
	if len(cookies) == 0 || origin == nil {
		return
	}
	now := time.Now()
	secure := origin.Scheme == "https"
	list := make([]*http.Cookie, 0, len(cookies))
	for name, value := range cookies {
		if name == "" || value == "" {
			continue
		}
		list = append(list, &http.Cookie{
			Name:    name,
			Value:   value,
			Path:    "/",
			Domain:  origin.Hostname(),
			Secure:  secure,
			Expires: now.Add(365 * 24 * time.Hour),
		})
	}
	c.jar.SetCookies(origin, list)
}

// SetCookies stores the session snapshot and mirrors it into the HTTP jar.
// Passing an empty map signs the client out.
func (c *Client) SetCookies(cookies map[string]string) {
	c.cookiesMu.Lock()
	c.cookies = make(map[string]string, len(cookies))
	for k, v := range cookies {
		if k == "" || v == "" {
			continue
		}
		c.cookies[k] = v
	}
	c.loggedIn = c.cookies["remix_userid"] != "" && c.cookies["remix_userkey"] != ""
	snapshot := make(map[string]string, len(c.cookies))
	for k, v := range c.cookies {
		snapshot[k] = v
	}
	c.cookiesMu.Unlock()
	c.resetJar(snapshot)
}

// Cookies returns a copy of the session snapshot; safe for concurrent reads.
func (c *Client) Cookies() map[string]string {
	c.cookiesMu.RLock()
	defer c.cookiesMu.RUnlock()
	out := make(map[string]string, len(c.cookies))
	for k, v := range c.cookies {
		out[k] = v
	}
	return out
}

// Logout drops the snapshot and rebuilds an empty jar.
func (c *Client) Logout() {
	c.cookiesMu.Lock()
	c.cookies = map[string]string{}
	c.loggedIn = false
	c.cookiesMu.Unlock()
	c.resetJar(nil)
}

// eapiRequest performs one EAPI call. It returns the raw body so endpoint
// functions own JSON parsing. Non-2xx responses that carry an EAPI envelope
// are passed through: the server's error text ("daily limit reached") must
// reach the user unwrapped.
func (c *Client) eapiRequest(ctx context.Context, method, path string, form url.Values) ([]byte, error) {
	domain, _ := c.domainSnapshot()
	if domain == "" {
		return nil, fmt.Errorf("no domain configured")
	}
	endpoint := domain + path

	var (
		req *http.Request
		err error
	)
	switch method {
	case http.MethodGet:
		if len(form) > 0 {
			endpoint += "?" + form.Encode()
		}
		req, err = http.NewRequestWithContext(ctx, method, endpoint, nil)
	default:
		req, err = http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(form.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", defaultUserAgent)
	c.addCredentialHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, ErrRedirectLoop) {
			return nil, err
		}
		return nil, fmt.Errorf("request %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxEAPIResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", endpoint, err)
	}

	if isBotProtectionResponse(resp, body) {
		return nil, &BotProtectionError{StatusCode: resp.StatusCode, URL: endpoint}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if isEAPIEnvelope(body) {
			return body, nil
		}
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, URL: endpoint}
	}
	return body, nil
}

func (c *Client) addCredentialHeaders(req *http.Request) {
	cookies := c.Cookies()
	if id := cookies["remix_userid"]; id != "" {
		req.Header.Set("remix-userid", id)
	}
	if key := cookies["remix_userkey"]; key != "" {
		req.Header.Set("remix-userkey", key)
	}
}

// checkRedirect limits hops and drops credentials when a mirror redirects
// off the whitelist (or downgrades to http). net/http only strips Cookie on
// cross-host redirects; remix-* are custom headers and would otherwise leak.
func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return ErrRedirectLoop
	}
	if len(via) > 0 && !isAllowedURL(req.URL) {
		stripEAPICredentials(req)
	}
	return nil
}

func stripEAPICredentials(req *http.Request) {
	req.Header.Del("remix-userid")
	req.Header.Del("remix-userkey")
	req.Header.Del("Cookie")
}

var botTitlePattern = regexp.MustCompile(`(?is)<title[^>]*>[^<]*diamwall[^<]*</title>`)

// isBotProtectionResponse implements a deliberately narrow check: DiamWall
// statuses, or an HTML page whose <title> names DiamWall.
// A __diamwall clearance cookie is proof the request got THROUGH, never a
// block signal; and EAPI JSON bodies are never scanned, so a book title
// containing "diamwall" cannot trip this.
func isBotProtectionResponse(resp *http.Response, body []byte) bool {
	if resp == nil {
		return false
	}
	if resp.StatusCode == 513 || resp.StatusCode == 517 {
		return true
	}
	// Only HTML bodies are scanned. An empty Content-Type counts as
	// "not HTML" too: EAPI JSON must never be searched for the marker.
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "html") {
		return false
	}
	return botTitlePattern.Match(body)
}

// isChallengePage detects the old JS challenge layout. EAPI does not use
// it, but the domain probe still has to recognise it.
func isChallengePage(body []byte) bool {
	if len(body) >= 20*1024 {
		return false
	}
	return strings.Contains(string(body), "c_token=") && strings.Contains(string(body), "hash")
}

// isEAPIEnvelope reports whether body is a JSON object carrying the
// "success" field. Only such bodies may be passed through on non-2xx.
func isEAPIEnvelope(body []byte) bool {
	env, err := decodeEAPIObject(body)
	return err == nil && env != nil
}

// sendsCredentials is the download-side rule: cookies only travel to the
// same scheme and host as the mirror. CDN links are signed and need no
// account credentials.
func (c *Client) sendsCredentials(u *url.URL) bool {
	_, origin := c.domainSnapshot()
	if u == nil || origin == nil {
		return false
	}
	return u.Scheme == origin.Scheme && strings.EqualFold(u.Hostname(), origin.Hostname())
}

// newDownloadRequest builds a GET for a file URL. The request carries
// cookies only when the target is the mirror itself.
func (c *Client) newDownloadRequest(ctx context.Context, rawURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if domain, _ := c.domainSnapshot(); domain != "" {
		req.Header.Set("Referer", domain+"/")
	}
	if c.sendsCredentials(req.URL) {
		c.addCredentialHeaders(req)
	}
	return req, nil
}

// checkDownloadRedirect applies the 10-hop cap and strips credentials on
// every hop that leaves the mirror (including HTTPS -> HTTP).
func (c *Client) checkDownloadRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return ErrRedirectLoop
	}
	if !c.sendsCredentials(req.URL) {
		stripEAPICredentials(req)
	}
	return nil
}

// downloadClient returns a client with no total timeout (a large book can
// take as long as it needs) but the same transport and jar.
func (c *Client) downloadClient() *http.Client {
	return &http.Client{
		Transport:     c.transport,
		Jar:           c.jar,
		CheckRedirect: c.checkDownloadRedirect,
	}
}

// FetchImage downloads a cover image so the GUI can show it. It follows the
// session rules like a download: the configured proxy, the redirect budget,
// and credentials only toward the mirror itself. The response is size-capped
// so a stray URL cannot exhaust memory.
func (c *Client) FetchImage(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := c.newDownloadRequest(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "image/*")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch image %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCoverBytes))
	if err != nil {
		return nil, fmt.Errorf("read image %s: %w", rawURL, err)
	}
	if isBotProtectionResponse(resp, body) {
		return nil, &BotProtectionError{StatusCode: resp.StatusCode, URL: rawURL}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, URL: rawURL}
	}
	return body, nil
}

// absolutizeURL resolves a possibly-relative href against the mirror.
func (c *Client) absolutizeURL(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return href
	}
	if u.IsAbs() {
		return u.String()
	}
	domain, _ := c.domainSnapshot()
	base, err := url.Parse(domain)
	if err != nil {
		return href
	}
	return base.ResolveReference(u).String()
}
