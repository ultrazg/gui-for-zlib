package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ProbeState is the doctor row result.
type ProbeState string

const (
	DomainHealthy         ProbeState = "healthy"
	DomainChallenged      ProbeState = "challenged"
	DomainDiamWallBlocked ProbeState = "diamwall_blocked"
	DomainRedirectLoop    ProbeState = "redirect_loop"
	DomainHTTPError       ProbeState = "http_error"
	DomainNetworkError    ProbeState = "network_error"
	DomainInvalid         ProbeState = "invalid_domain"
)

const (
	domainProbeTimeout = 10 * time.Second
	maxDomainProbeHops = 3
	maxDomainProbeBody = 64 * 1024
	probeUserAgent     = defaultUserAgent
)

// DomainProbeOptions controls one probe.
type DomainProbeOptions struct {
	ProxyURL string
	Path     string
	Timeout  time.Duration
}

// DomainProbeResult is one doctor row plus the detail GUI/CLI can show.
type DomainProbeResult struct {
	Domain     string
	State      ProbeState
	StatusCode int
	FinalURL   string
	Latency    time.Duration
	Err        string
}

// ProbeDomain checks whether a candidate can serve EAPI, without any
// credentials. Redirects are followed by hand so the __diamwall cookie and
// the hop limit stay observable.
func ProbeDomain(ctx context.Context, domain string, opt DomainProbeOptions) DomainProbeResult {
	start := time.Now()
	result := DomainProbeResult{Domain: domain}

	path := strings.TrimSpace(opt.Path)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		return finishProbe(result, start, DomainInvalid, "probe path must start with /")
	}
	timeout := opt.Timeout
	if timeout <= 0 {
		timeout = domainProbeTimeout
	}

	current, err := url.Parse(strings.TrimSpace(domain))
	if err != nil || current.Host == "" || (current.Scheme != "http" && current.Scheme != "https") {
		return finishProbe(result, start, DomainInvalid, "cannot parse domain URL")
	}
	current.Path = path
	current.RawQuery = ""
	current.Fragment = ""

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if strings.TrimSpace(opt.ProxyURL) != "" {
		proxyURL, err := parseProxyURL(opt.ProxyURL)
		if err != nil {
			return finishProbe(result, start, DomainInvalid, err.Error())
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	seen := map[string]bool{}
	sawDiamWallCookie := false
	hops := 0

	for {
		key := current.String()
		if seen[key] {
			return blockedOrLoop(result, start, sawDiamWallCookie, hops, "redirect loop")
		}
		seen[key] = true
		result.FinalURL = key

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, key, nil)
		if err != nil {
			return finishProbe(result, start, DomainInvalid, err.Error())
		}
		req.Header.Set("User-Agent", probeUserAgent)
		if strings.HasPrefix(current.Path, "/eapi/") {
			req.Header.Set("Accept", "application/json")
		}

		resp, err := client.Do(req)
		if err != nil {
			return finishProbe(result, start, DomainNetworkError, err.Error())
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxDomainProbeBody))
		_ = resp.Body.Close()
		result.StatusCode = resp.StatusCode

		for _, cookie := range resp.Cookies() {
			if strings.EqualFold(cookie.Name, "__diamwall") {
				sawDiamWallCookie = true
			}
		}
		if isBotProtectionResponse(resp, body) {
			return finishProbe(result, start, DomainDiamWallBlocked, "DiamWall blocked the request")
		}
		if isChallengePage(body) {
			return finishProbe(result, start, DomainChallenged, "legacy JS challenge page")
		}

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			if strings.HasPrefix(current.Path, "/eapi/") && !isJSONObject(body) {
				return finishProbe(result, start, DomainHTTPError, "EAPI path returned non-JSON content")
			}
			return finishProbe(result, start, DomainHealthy, "")
		case resp.StatusCode >= 300 && resp.StatusCode < 400:
			location := strings.TrimSpace(resp.Header.Get("Location"))
			hops++
			if location == "" {
				return blockedOrLoop(result, start, sawDiamWallCookie, hops, "redirect without Location")
			}
			next, err := current.Parse(location)
			if err != nil {
				return finishProbe(result, start, DomainInvalid, "bad redirect target: "+location)
			}
			if hops >= maxDomainProbeHops {
				return blockedOrLoop(result, start, sawDiamWallCookie, hops, "too many redirects")
			}
			current = next
		default:
			return finishProbe(result, start, DomainHTTPError, fmt.Sprintf("HTTP %d", resp.StatusCode))
		}
	}
}

// blockedOrLoop treats a __diamwall cookie as a clearance pass. Only a
// redirect chain that carried it and never landed on a 2xx is reported as
// blocked, not as a loop.
func blockedOrLoop(result DomainProbeResult, start time.Time, sawCookie bool, hops int, detail string) DomainProbeResult {
	if sawCookie {
		return finishProbe(result, start, DomainDiamWallBlocked, "redirect chain never completed")
	}
	return finishProbe(result, start, DomainRedirectLoop, fmt.Sprintf("%s after %d hop(s)", detail, hops))
}

func finishProbe(result DomainProbeResult, start time.Time, state ProbeState, errText string) DomainProbeResult {
	result.State = state
	result.Err = errText
	result.Latency = time.Since(start)
	return result
}

func isJSONObject(body []byte) bool {
	var v any
	return json.Unmarshal(body, &v) == nil && v != nil
}

// ProbeDomains probes every candidate concurrently, preserving input order.
// One failing domain never affects the others.
func ProbeDomains(ctx context.Context, candidates []string, opt DomainProbeOptions) []DomainProbeResult {
	results := make([]DomainProbeResult, len(candidates))
	var wg sync.WaitGroup
	for i, domain := range candidates {
		wg.Add(1)
		go func(i int, domain string) {
			defer wg.Done()
			results[i] = ProbeDomain(ctx, domain, opt)
		}(i, domain)
	}
	wg.Wait()
	return results
}
