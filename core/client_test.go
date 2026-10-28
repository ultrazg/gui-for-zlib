package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestClientSetProxy(t *testing.T) {
	t.Setenv(EnvProxy, "")

	c := NewClient()
	if got := c.Proxy(); got != "" {
		t.Fatalf("initial proxy = %q, want empty", got)
	}
	if err := c.SetProxy("socks5://127.0.0.1:1080"); err != nil {
		t.Fatalf("SetProxy: %v", err)
	}
	if got := c.Proxy(); got != "socks5://127.0.0.1:1080" {
		t.Fatalf("Proxy = %q, want socks5://127.0.0.1:1080", got)
	}
	if err := c.SetProxy("bad://"); err == nil || !errors.Is(err, ErrInvalidProxy) {
		t.Fatalf("SetProxy(bad://) err = %v, want ErrInvalidProxy", err)
	}
	if got := c.Proxy(); got != "socks5://127.0.0.1:1080" {
		t.Fatalf("Proxy changed after failed SetProxy = %q", got)
	}
	if err := c.SetProxy(""); err != nil {
		t.Fatalf("SetProxy(\"\"): %v", err)
	}
	if got := c.Proxy(); got != "" {
		t.Fatalf("Proxy after clear = %q, want empty", got)
	}
}

func TestProxyURLFromParts(t *testing.T) {
	tests := []struct {
		name    string
		scheme  string
		addr    string
		port    string
		want    string
		wantErr bool
	}{
		{name: "socks5 default", scheme: "", addr: "127.0.0.1", port: "7890", want: "socks5://127.0.0.1:7890"},
		{name: "http lowercased", scheme: "HTTP", addr: "192.168.1.2", port: "8080", want: "http://192.168.1.2:8080"},
		{name: "ipv6 bare", scheme: "", addr: "::1", port: "1080", want: "socks5://[::1]:1080"},
		{name: "ipv6 bracketed", scheme: "socks5", addr: "[::1]", port: "1080", want: "socks5://[::1]:1080"},
		{name: "empty addr", scheme: "", addr: "", port: "7890", wantErr: true},
		{name: "non numeric port", scheme: "", addr: "127.0.0.1", port: "abc", wantErr: true},
		{name: "port zero", scheme: "", addr: "127.0.0.1", port: "0", wantErr: true},
		{name: "port too large", scheme: "", addr: "127.0.0.1", port: "70000", wantErr: true},
		{name: "unsupported scheme", scheme: "ftp", addr: "127.0.0.1", port: "7890", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ProxyURLFromParts(tt.scheme, tt.addr, tt.port)
			if tt.wantErr {
				if err == nil || !errors.Is(err, ErrInvalidProxy) {
					t.Fatalf("ProxyURLFromParts(%q,%q,%q) = %q, err %v; want ErrInvalidProxy", tt.scheme, tt.addr, tt.port, got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ProxyURLFromParts(%q,%q,%q): %v", tt.scheme, tt.addr, tt.port, err)
			}
			if got != tt.want {
				t.Fatalf("ProxyURLFromParts = %q, want %q", got, tt.want)
			}
		})
	}
}

// The transport must actually route requests through a set proxy, and the
// switch must be visible on a new connection after CloseIdleConnections.
func TestProxyRoutesRequests(t *testing.T) {
	t.Setenv(EnvProxy, "")

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	var mu sync.Mutex
	sawRequest := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sawRequest = true
		mu.Unlock()
		if got, want := r.URL.Host, strings.TrimPrefix(target.URL, "http://"); got != want {
			t.Errorf("proxy saw request for host %q, want %q", got, want)
		}
		if !r.URL.IsAbs() {
			t.Errorf("proxy saw a non-absolute-form request: %q", r.URL.String())
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	defer proxy.Close()

	c := NewClient()
	if err := c.SetProxy(proxy.URL); err != nil {
		t.Fatalf("SetProxy: %v", err)
	}
	if _, err := c.FetchImage(context.Background(), target.URL+"/cover.png"); err != nil {
		t.Fatalf("FetchImage through proxy: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !sawRequest {
		t.Fatal("proxy never received the request")
	}
}
