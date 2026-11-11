package core

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Backend is the real Service implementation: the HTTP, EAPI, download and
// domain-policy layers assembled behind the same interface the GUI already
// uses.
type Backend struct {
	client *Client

	preferredDomain string
	downloadDir     string
	appCfg          *AppConfig

	sessionMu sync.RWMutex
	session   *Session

	loginMu sync.Mutex
}

type backendOptions struct {
	domain      string
	proxy       string
	downloadDir string
}

// BackendOption configures NewBackend.
type BackendOption func(*backendOptions)

// WithBackendDomain pins the mirror domain (highest precedence).
func WithBackendDomain(domain string) BackendOption {
	return func(o *backendOptions) { o.domain = domain }
}

// WithBackendProxy pins the proxy URL.
func WithBackendProxy(proxy string) BackendOption {
	return func(o *backendOptions) { o.proxy = proxy }
}

// WithDownloadDir sets the default download directory.
func WithDownloadDir(dir string) BackendOption {
	return func(o *backendOptions) { o.downloadDir = dir }
}

// NewBackend loads .env files, restores a saved session and returns a
// ready-to-use Service. It never contacts the network.
func NewBackend(opts ...BackendOption) *Backend {
	LoadDotEnvFiles()

	o := backendOptions{}
	for _, fn := range opts {
		fn(&o)
	}

	clientOpts := make([]ClientOption, 0, 2)
	if o.domain != "" {
		clientOpts = append(clientOpts, WithDomain(o.domain))
	}
	if o.proxy != "" {
		clientOpts = append(clientOpts, WithProxy(o.proxy))
	}

	b := &Backend{
		client:          NewClient(clientOpts...),
		preferredDomain: o.domain,
		downloadDir:     o.downloadDir,
		appCfg:          LoadAppConfig(),
	}
	b.restoreSession()
	return b
}

// restoreSession applies session.json without failing construction: a
// corrupt or absent file simply means signed out.
func (b *Backend) restoreSession() {
	saved, err := LoadSession()
	if err != nil {
		return
	}
	domain := strings.TrimSpace(os.Getenv(EnvDomain))
	if domain == "" {
		domain = saved.Domain
	}
	if domain == "" {
		domain = b.client.Domain()
	}
	if err := b.client.SetDomain(domain); err != nil {
		return
	}
	b.client.SetCookies(saved.Cookies)
	if !b.client.LoggedIn() {
		return
	}

	email := b.appCfg.LastLoginEmail
	name := saved.Name
	if name == "" {
		// Sessions written before Name was persisted, and cookie imports,
		// carry no display name; the email's local part is the best guess.
		name = localPart(email)
		if name == "" {
			name = "已登录"
		}
	}
	b.sessionMu.Lock()
	b.session = &Session{Email: email, Name: name}
	b.sessionMu.Unlock()
}

// CurrentSession returns the restored or freshly created session.
func (b *Backend) CurrentSession() *Session {
	b.sessionMu.RLock()
	defer b.sessionMu.RUnlock()
	if b.session == nil {
		return nil
	}
	copy := *b.session
	return &copy
}

// UpdateSessionName remembers the account's display name once the profile
// reports it. It repairs sessions saved before the name was persisted, so a
// restart does not fall back to the email's local part.
func (b *Backend) UpdateSessionName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	b.loginMu.Lock()
	defer b.loginMu.Unlock()

	b.sessionMu.Lock()
	if b.session == nil {
		b.sessionMu.Unlock()
		return nil
	}
	changed := b.session.Name != name
	b.session.Name = name
	b.sessionMu.Unlock()
	if !changed {
		return nil
	}

	saved, err := LoadSession()
	if err != nil {
		// Signed out in the meantime; nothing to update.
		return nil
	}
	saved.Name = name
	return SaveSession(saved)
}

// LastLoginEmail is used by the login form's prefill.
func (b *Backend) LastLoginEmail() string {
	b.sessionMu.RLock()
	defer b.sessionMu.RUnlock()
	if b.appCfg == nil {
		return ""
	}
	return b.appCfg.LastLoginEmail
}

// CurrentGUISettings reports the display preferences saved in config.json,
// so the GUI can restore theme, download directory and page size.
func (b *Backend) CurrentGUISettings() GUISettings {
	b.sessionMu.RLock()
	defer b.sessionMu.RUnlock()
	if b.appCfg == nil {
		return GUISettings{}
	}
	return b.appCfg.GUI
}

// SaveGUISettings writes the display preferences the GUI edits. It only
// replaces the GUI block: login state the backend owns is never touched.
func (b *Backend) SaveGUISettings(s GUISettings) error {
	b.sessionMu.Lock()
	defer b.sessionMu.Unlock()
	if b.appCfg == nil {
		b.appCfg = &AppConfig{}
	}
	b.appCfg.GUI = s
	return SaveAppConfig(b.appCfg)
}

// CurrentDomain reports the mirror currently in use.
func (b *Backend) CurrentDomain() string { return b.client.Domain() }

// UseDomain switches the preferred mirror and persists it to the config
// .env so the CLI and the next GUI run agree. The doctor page calls this
// after a successful probe.
func (b *Backend) UseDomain(domain string) error {
	b.loginMu.Lock()
	defer b.loginMu.Unlock()

	parsed, err := ParseAllowedDomain(domain)
	if err != nil {
		return err
	}
	if err := b.client.SetDomain(parsed.String()); err != nil {
		return err
	}
	b.preferredDomain = parsed.String()
	_ = PersistDotEnv(EnvDomain, parsed.String())
	return nil
}

// SetProxy switches the network proxy at runtime; empty clears to direct.
// Doctor and the login preflight read the same value via client.Proxy.
func (b *Backend) SetProxy(proxy string) error { return b.client.SetProxy(proxy) }

// Login probes the mirror, authenticates and persists the session. The
// password is sent once and never stored.
func (b *Backend) Login(ctx context.Context, email, password string) (*Session, error) {
	b.loginMu.Lock()
	defer b.loginMu.Unlock()

	email = strings.TrimSpace(email)
	password = strings.TrimSpace(password)
	switch {
	case email == "":
		return nil, fmt.Errorf("%w: email is required", ErrLoginFailed)
	case password == "":
		return nil, fmt.Errorf("%w: password is required", ErrLoginFailed)
	}

	domain, err := b.resolveLoginDomain(ctx)
	if err != nil {
		return nil, err
	}
	if err := b.client.SetDomain(domain); err != nil {
		return nil, err
	}

	session, err := b.client.LoginEAPI(ctx, email, password)
	if err != nil {
		return nil, err
	}

	saved := &SessionFile{
		Cookies: b.client.Cookies(),
		Domain:  b.client.Domain(),
		Mode:    "eapi",
		Name:    session.Name,
	}
	if err := SaveSession(saved); err != nil {
		// Do not leave the client half-signed-in with cookies the GUI
		// will never believe in.
		b.client.Logout()
		return nil, fmt.Errorf("signed in but could not save the session: %w", err)
	}

	// Best-effort conveniences; a failure here must not undo the login.
	b.sessionMu.Lock()
	b.appCfg.LastLoginEmail = email
	b.session = session
	cfgCopy := *b.appCfg
	b.sessionMu.Unlock()
	_ = SaveAppConfig(&cfgCopy)
	_ = PersistDotEnv(EnvDomain, b.client.Domain())
	return session, nil
}

// resolveLoginDomain runs the login preflight: probe the candidate, follow
// a pure redirector to its final host, then re-validate that host against
// the whitelist.
func (b *Backend) resolveLoginDomain(ctx context.Context) (string, error) {
	candidate := b.preferredDomain
	if candidate == "" {
		candidate = CurrentDefaultDomain()
	}
	parsed, err := ParseAllowedDomain(candidate)
	if err != nil {
		return "", err
	}

	probe := ProbeDomain(ctx, parsed.String(), DomainProbeOptions{
		Path:     eapiProbePath,
		ProxyURL: b.client.Proxy(),
	})
	if probe.State != DomainHealthy {
		detail := probe.Err
		if detail == "" {
			detail = string(probe.State)
		}
		return "", fmt.Errorf("%w: %s (%s); run the domain check or set %s", ErrNoDomain, parsed.Host, detail, EnvDomain)
	}

	final, err := url.Parse(probe.FinalURL)
	if err == nil && final.Hostname() != "" && !strings.EqualFold(final.Hostname(), parsed.Hostname()) {
		origin := &url.URL{Scheme: final.Scheme, Host: final.Host}
		validated, err := ParseAllowedDomain(origin.String())
		if err != nil {
			return "", fmt.Errorf("%w: redirect left the whitelist: %v", ErrNoDomain, err)
		}
		return validated.String(), nil
	}
	return parsed.String(), nil
}

// Logout removes session.json and clears the in-memory cookies.
func (b *Backend) Logout() error {
	b.sessionMu.Lock()
	b.session = nil
	b.sessionMu.Unlock()
	b.client.Logout()
	_, err := DeleteSession()
	return err
}

func (b *Backend) requireClient() (*Client, error) {
	if !b.client.LoggedIn() {
		return nil, ErrNotLoggedIn
	}
	return b.client, nil
}

// Search maps the GUI's single-select filters onto the wire slices.
func (b *Backend) Search(ctx context.Context, opt SearchOptions) (*BookPage, error) {
	if strings.TrimSpace(opt.Query) == "" {
		// The GUI fires an empty search when it opens; an empty page is
		// friendlier than an error and avoids a pointless request.
		return &BookPage{Books: []Book{}, Page: 1}, nil
	}
	client, err := b.requireClient()
	if err != nil {
		return nil, err
	}
	return client.searchEAPI(ctx, opt)
}

func (b *Backend) Favorites(ctx context.Context, page, count int) (*BookPage, error) {
	client, err := b.requireClient()
	if err != nil {
		return nil, err
	}
	return client.SavedBooks(ctx, page, count)
}

func (b *Backend) FavoriteStatus(ctx context.Context, book Book) (bool, error) {
	client, err := b.requireClient()
	if err != nil {
		return false, err
	}
	return client.FavoriteStatus(ctx, book)
}

func (b *Backend) SetFavorite(ctx context.Context, id string, saved bool) error {
	client, err := b.requireClient()
	if err != nil {
		return err
	}
	return client.SetFavorite(ctx, id, saved)
}

func (b *Backend) History(ctx context.Context, page, count int) (*BookPage, error) {
	client, err := b.requireClient()
	if err != nil {
		return nil, err
	}
	return client.DownloadHistory(ctx, page, count)
}

// FetchBook refreshes a list item's metadata through the session client.
func (b *Backend) FetchBook(ctx context.Context, id, hash string) (*Book, error) {
	client, err := b.requireClient()
	if err != nil {
		return nil, err
	}
	return client.FetchBook(ctx, id, hash)
}

func (b *Backend) Recommended(ctx context.Context, page, count int) (*BookPage, error) {
	client, err := b.requireClient()
	if err != nil {
		return nil, err
	}
	return client.Recommended(ctx, page, count)
}

func (b *Backend) Popular(ctx context.Context, page, count int) (*BookPage, error) {
	client, err := b.requireClient()
	if err != nil {
		return nil, err
	}
	return client.MostPopular(ctx, page, count)
}

func (b *Backend) Profile(ctx context.Context) (*Profile, error) {
	client, err := b.requireClient()
	if err != nil {
		return nil, err
	}
	profile, err := client.FetchProfile(ctx)
	if err != nil {
		return nil, err
	}
	if profile.Email == "" {
		if s := b.CurrentSession(); s != nil {
			profile.Email = s.Email
		}
	}
	if profile.Name == "" {
		profile.Name = localPart(profile.Email)
	}
	return profile, nil
}

// Doctor probes every whitelisted mirror concurrently. Rows are returned
// even when all fail; the GUI shows the reason per row.
func (b *Backend) Doctor(ctx context.Context) (*DoctorReport, error) {
	results := ProbeDomains(ctx, KnownDomains(), DomainProbeOptions{
		Path:     eapiProbePath,
		ProxyURL: b.client.Proxy(),
	})

	report := &DoctorReport{
		Checked: time.Now(),
		Domains: make([]DomainStatus, 0, len(results)),
	}
	for _, r := range results {
		ok := r.State == DomainHealthy
		if ok {
			if u, err := url.Parse(r.FinalURL); err != nil || !isAllowedURL(u) {
				ok = false
			}
		}
		errText := r.Err
		if !ok && errText == "" {
			errText = string(r.State)
		}
		if ok {
			errText = ""
		}
		report.Domains = append(report.Domains, DomainStatus{
			Domain:  r.Domain,
			OK:      ok,
			Latency: r.Latency,
			Error:   errText,
		})
	}
	return report, nil
}

// proxyTestURL is where a 测试连接 probe goes: reaching it proves the proxy
// can get out to the internet, independent of the current mirror's health.
const proxyTestURL = "https://www.google.com"

// proxyTestTimeout bounds one 测试连接 probe; a little shorter than the
// doctor's default so the button answers promptly.
const proxyTestTimeout = 8 * time.Second

// TestProxy verifies that a proxy can reach the internet by fetching
// proxyTestURL, reusing the domain probe. It returns the healthy latency, or
// an error classified so the GUI can show a friendly message: a malformed
// proxy is ErrInvalidProxy; any other failure carries the probe's reason.
func (b *Backend) TestProxy(ctx context.Context, proxy string) (time.Duration, error) {
	res := ProbeDomain(ctx, proxyTestURL, DomainProbeOptions{
		Path:     "/",
		ProxyURL: proxy,
		Timeout:  proxyTestTimeout,
	})
	if res.State == DomainHealthy {
		return res.Latency, nil
	}
	if res.State == DomainInvalid {
		return 0, fmt.Errorf("%w: %s", ErrInvalidProxy, res.Err)
	}
	detail := res.Err
	if detail == "" {
		detail = string(res.State)
	}
	if res.StatusCode != 0 {
		detail = fmt.Sprintf("%s（HTTP %d）", detail, res.StatusCode)
	}
	return 0, fmt.Errorf("%s", detail)
}

// Download resolves the book's file link and writes it atomically.
func (b *Backend) Download(ctx context.Context, book Book, dir string, progress func(done, total int64)) (string, error) {
	client, err := b.requireClient()
	if err != nil {
		return "", err
	}

	downloadURL := strings.TrimSpace(book.DownloadURL)
	if downloadURL == "" && book.ID != "" && book.Hash != "" {
		downloadURL = client.Domain() + "/eapi/book/" + url.PathEscape(book.ID) + "/" + url.PathEscape(book.Hash) + "/file"
	}
	if downloadURL == "" {
		return "", ErrNoID
	}

	targetDir := strings.TrimSpace(dir)
	if targetDir == "" {
		targetDir = strings.TrimSpace(os.Getenv(EnvDownloadDir))
	}
	if targetDir == "" {
		targetDir = b.downloadDir
	}
	if targetDir == "" {
		targetDir = "."
	}
	if abs, err := filepath.Abs(targetDir); err == nil {
		targetDir = abs
	}

	result, err := client.DownloadBook(ctx, downloadURL, targetDir, filenameFromBook(book), progress)
	if err != nil {
		return "", err
	}
	return result.FilePath, nil
}

// filenameFromBook derives a fallback download name from a book's metadata,
// "Title.EXT", used when the server response carries no usable name: the
// EAPI file link ends in /redirection, and with no Content-Disposition the
// URL basename would name the file "redirection" with no extension. It
// returns "" when the book has no title or extension, so the server-derived
// name stands.
func filenameFromBook(book Book) string {
	title := strings.TrimSpace(book.Title)
	ext := strings.ToLower(strings.TrimSpace(book.Extension))
	ext = strings.TrimPrefix(ext, ".")
	if title == "" || ext == "" {
		return ""
	}
	return title + "." + ext
}

// FetchImage downloads a book cover through the session client, so the
// mirror, proxy and redirect rules apply even while signed out.
func (b *Backend) FetchImage(ctx context.Context, rawURL string) ([]byte, error) {
	if b.client == nil {
		return nil, ErrNotLoggedIn
	}
	return b.client.FetchImage(ctx, rawURL)
}
