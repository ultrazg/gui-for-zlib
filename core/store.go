package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Local state layout: ~/.config/zlib (or $ZLIB_CONFIG_DIR for tests and
// portable installs). Windows resolves the same way relative to the user's
// home directory.
func ConfigDir() string {
	if dir := strings.TrimSpace(os.Getenv(EnvConfigDir)); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".zlib"
	}
	return filepath.Join(home, ".config", "zlib")
}

func SessionPath() string   { return filepath.Join(ConfigDir(), "session.json") }
func AppConfigPath() string { return filepath.Join(ConfigDir(), "config.json") }
func DotEnvPath() string    { return filepath.Join(ConfigDir(), ".env") }

// SessionFile is session.json. Cookies are the only credential material;
// the password never reaches this file.
type SessionFile struct {
	Cookies map[string]string `json:"cookies"`
	Domain  string            `json:"domain"`
	Mode    string            `json:"mode"`
}

// LoadSession reads session.json. A missing file is reported as
// "not logged in" with guidance.
func LoadSession() (*SessionFile, error) {
	data, err := os.ReadFile(SessionPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: no saved session; sign in to continue", ErrNotLoggedIn)
		}
		return nil, err
	}
	var s SessionFile
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("session file is corrupt: %w", err)
	}
	if len(s.Cookies) == 0 {
		return nil, fmt.Errorf("%w: saved session has no cookies", ErrNotLoggedIn)
	}
	if s.Mode == "" {
		s.Mode = "eapi"
	}
	return &s, nil
}

// SaveSession writes session.json atomically with 0600 permissions.
func SaveSession(s *SessionFile) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(SessionPath(), data, 0o600)
}

// DeleteSession removes session.json. The bool reports whether a file was
// actually removed.
func DeleteSession() (bool, error) {
	err := os.Remove(SessionPath())
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// AppConfig is config.json: display settings only, never secrets. The GUI
// owns the GUI block; only LastLoginEmail lives at the top level so the
// login form, the CLI and the GUI share it.
type AppConfig struct {
	LastLoginEmail string      `json:"last_login_email,omitempty"`
	GUI            GUISettings `json:"gui,omitempty"`
}

// GUISettings is the slice of config.json the desktop UI persists and
// restores: theme, download directory, default format, page size, the
// network proxy and the download-complete notification toggle. It never
// carries credentials.
type GUISettings struct {
	Theme            string `json:"theme,omitempty"`
	DownloadDir      string `json:"download_dir,omitempty"`
	DefaultExtension string `json:"default_extension,omitempty"`
	PageCount        int    `json:"page_count,omitempty"`

	// 网络代理：开启后全部网络流量经该代理转发。
	ProxyEnabled bool   `json:"proxy_enabled,omitempty"`
	ProxyScheme  string `json:"proxy_scheme,omitempty"` // "socks5" | "http"
	ProxyAddr    string `json:"proxy_addr,omitempty"`
	ProxyPort    string `json:"proxy_port,omitempty"`

	// 下载任务完成时发送桌面通知。
	NotifyOnDownload bool `json:"notify_on_download,omitempty"`
}

func LoadAppConfig() *AppConfig {
	cfg := &AppConfig{}
	data, err := os.ReadFile(AppConfigPath())
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return &AppConfig{}
	}
	return cfg
}

func SaveAppConfig(cfg *AppConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(AppConfigPath(), data, 0o600)
}

// writeFileAtomic writes to a temp file in the same directory, syncs it and
// renames. A crash can never leave a half-written session or config.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".zlib-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	committed = true
	return nil
}

// LoadDotEnvFiles applies the working-directory .env and then the config
// .env. Real environment variables always win: a value that already exists
// is never overwritten.
func LoadDotEnvFiles() {
	for _, path := range []string{".env", DotEnvPath()} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		applyDotEnv(string(data))
	}
}

func applyDotEnv(text string) {
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := parseDotEnvLine(line)
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}
}

func parseDotEnvLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	key, value, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'') {
			quote := value[0]
			value = value[1 : len(value)-1]
			if quote == '"' {
				value = strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\$`, `$`, `\n`, "\n").Replace(value)
			}
		}
	}
	return key, value, true
}

var dotEnvLinePattern = regexp.MustCompile(`^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=`)

func escapeDotEnvValue(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`)
	return replacer.Replace(value)
}

// PersistDotEnv rewrites only the target key, preserving comments, other
// keys and line order. An empty value removes the line. The write is
// atomic so a GUI and a CLI can share the file.
func PersistDotEnv(key, value string) error {
	path := DotEnvPath()
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	text := strings.ReplaceAll(string(existing), "\r\n", "\n")
	lines := []string{}
	if text != "" {
		lines = strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	}

	replaced := false
	out := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		m := dotEnvLinePattern.FindStringSubmatch(line)
		if len(m) == 2 && m[1] == key {
			if value == "" {
				continue
			}
			if !replaced {
				out = append(out, fmt.Sprintf(`%s="%s"`, key, escapeDotEnvValue(value)))
				replaced = true
			}
			continue
		}
		out = append(out, line)
	}
	if value != "" && !replaced {
		out = append(out, fmt.Sprintf(`%s="%s"`, key, escapeDotEnvValue(value)))
	}

	payload := []byte(strings.Join(out, "\n"))
	if len(payload) > 0 {
		payload = append(payload, '\n')
	}
	return writeFileAtomic(path, payload, 0o600)
}
