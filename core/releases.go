package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	ProjectURL = "https://github.com/ultrazg/gui-for-zlib"
	// UpstreamURL is the Z-Library CLI project this GUI is built on; the
	// About page links to it.
	UpstreamURL      = "https://github.com/heartleo/zlib"
	latestReleaseURL = "https://api.github.com/repos/ultrazg/gui-for-zlib/releases/latest"
	releaseTimeout   = 15 * time.Second
	maxReleaseBytes  = 1 << 20
)

// Release contains the tag and Markdown notes of a GitHub release.
type Release struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
}

func (r Release) URL() string { return ProjectURL + "/releases/tag/" + url.PathEscape(r.TagName) }

// CheckForUpdate returns nil when the latest release is not newer. Only
// the proxy-aware transport is shared with the session client: this public
// GitHub request must not use Z-Library cookies or mirror redirect rules.
func (b *Backend) CheckForUpdate(ctx context.Context, current string) (*Release, error) {
	client := &http.Client{Transport: b.client.transport, Timeout: releaseTimeout}
	return checkForUpdate(ctx, client, latestReleaseURL, current)
}

func checkForUpdate(ctx context.Context, client *http.Client, endpoint, current string) (*Release, error) {
	version, err := parseReleaseVersion(current)
	if err != nil {
		return nil, fmt.Errorf("当前版本无效：%w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, releaseTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "gui-for-zlib/"+current)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 GitHub，请检查网络或代理设置：%w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, errors.New("GitHub 暂无可用的正式发布版本")
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, fmt.Errorf("GitHub 请求受限，请稍后重试（HTTP %d）", resp.StatusCode)
	default:
		return nil, fmt.Errorf("GitHub 返回异常状态（HTTP %d）", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReleaseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取更新信息失败：%w", err)
	}
	if len(body) > maxReleaseBytes {
		return nil, errors.New("GitHub 更新信息过大")
	}
	var result struct {
		Release
		Draft      bool `json:"draft"`
		Prerelease bool `json:"prerelease"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析 GitHub 更新信息失败：%w", err)
	}
	if result.Draft || result.Prerelease {
		return nil, errors.New("GitHub 未返回正式发布版本")
	}
	latest, err := parseReleaseVersion(result.TagName)
	if err != nil {
		return nil, fmt.Errorf("发布标签无效：%w", err)
	}
	if latest.compare(version) <= 0 {
		return nil, nil
	}
	return &result.Release, nil
}
