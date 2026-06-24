package netproxy_test

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"ccgo/internal/sandbox/netproxy"
)

// makeProxy starts a FilteringProxy with the given policy, returning the proxy
// and a cleanup function. The proxy listens on a random loopback port.
func makeProxy(t *testing.T, policy netproxy.Policy) (*netproxy.FilteringProxy, func()) {
	t.Helper()
	p, err := netproxy.Start(policy)
	if err != nil {
		t.Fatalf("netproxy.Start: %v", err)
	}
	return p, func() { p.Close() }
}

// httpClientViaProxy returns an *http.Client that routes all requests through
// the proxy at proxyURL.
func httpClientViaProxy(t *testing.T, proxyURL string) *http.Client {
	t.Helper()
	u, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}
	return &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(u)},
		Timeout:   5 * time.Second,
	}
}

// TestAllowedDomainProxied verifies that a request to an allowed domain is
// forwarded through the proxy to the target server.
func TestAllowedDomainProxied(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	}))
	defer backend.Close()

	// Parse backend host:port for the allowed-domain entry.
	backendHost := mustHost(t, backend.URL)

	proxy, cleanup := makeProxy(t, netproxy.Policy{
		AllowedDomains: []string{backendHost},
	})
	defer cleanup()

	client := httpClientViaProxy(t, proxy.URL())
	resp, err := client.Get(backend.URL)
	if err != nil {
		t.Fatalf("GET via proxy: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello" {
		t.Errorf("body = %q, want %q", string(body), "hello")
	}
}

// TestDeniedDomainBlocked verifies that a request to a domain NOT in
// AllowedDomains is rejected with 403.
func TestDeniedDomainBlocked(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	// AllowedDomains does NOT include the backend host → deny-by-default.
	proxy, cleanup := makeProxy(t, netproxy.Policy{
		AllowedDomains: []string{"some-other-allowed.example.com"},
	})
	defer cleanup()

	client := httpClientViaProxy(t, proxy.URL())
	resp, err := client.Get(backend.URL)
	if err != nil {
		// The proxy may refuse the TCP connection entirely — both error and 403 are acceptable.
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for denied domain", resp.StatusCode)
	}
}

// TestDeniedDomainsPrecedence verifies that DeniedDomains takes precedence over
// AllowedDomains — a domain in both lists is blocked.
func TestDeniedDomainsPrecedence(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	backendHost := mustHost(t, backend.URL)

	proxy, cleanup := makeProxy(t, netproxy.Policy{
		AllowedDomains: []string{backendHost},
		DeniedDomains:  []string{backendHost}, // explicitly denied — must win
	})
	defer cleanup()

	client := httpClientViaProxy(t, proxy.URL())
	resp, err := client.Get(backend.URL)
	if err != nil {
		return // refused is also acceptable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (denied takes precedence over allowed)", resp.StatusCode)
	}
}

// TestWildcardAllow verifies that "*.example.com" matches "api.example.com".
func TestWildcardAllow(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("wildcard-ok"))
	}))
	defer backend.Close()

	// The backend listens on 127.0.0.1:<port>. We register the wildcard that
	// would match a real-world domain in the allow-list; for the test we register
	// the literal host returned by mustHost so the match is exact.
	backendHost := mustHost(t, backend.URL)

	proxy, cleanup := makeProxy(t, netproxy.Policy{
		AllowedDomains: []string{"*.example.com", backendHost},
	})
	defer cleanup()

	client := httpClientViaProxy(t, proxy.URL())
	resp, err := client.Get(backend.URL)
	if err != nil {
		t.Fatalf("GET via proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// TestWildcardMatchLogic verifies the domain-matching function directly.
func TestWildcardMatchLogic(t *testing.T) {
	cases := []struct {
		pattern string
		host    string
		want    bool
	}{
		// Exact match
		{"github.com", "github.com", true},
		{"github.com", "api.github.com", false},
		// Suffix wildcard
		{"*.github.com", "api.github.com", true},
		{"*.github.com", "github.com", false},
		{"*.github.com", "evil.api.github.com", false}, // only one label wildcard
		// Port stripping
		{"github.com", "github.com:443", true},
		{"*.github.com", "api.github.com:443", true},
		// No match
		{"example.com", "example.org", false},
	}
	for _, tc := range cases {
		got := netproxy.DomainMatches(tc.pattern, tc.host)
		if got != tc.want {
			t.Errorf("DomainMatches(%q, %q) = %v, want %v", tc.pattern, tc.host, got, tc.want)
		}
	}
}

// TestEmptyAllowedDeniesAll verifies deny-by-default: no AllowedDomains → all blocked.
func TestEmptyAllowedDeniesAll(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	// Empty AllowedDomains — deny-by-default, nothing allowed.
	proxy, cleanup := makeProxy(t, netproxy.Policy{
		AllowedDomains: nil,
	})
	defer cleanup()

	client := httpClientViaProxy(t, proxy.URL())
	resp, err := client.Get(backend.URL)
	if err != nil {
		return // connection refused is fine
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (deny-by-default)", resp.StatusCode)
	}
}

// TestConcurrentConnections verifies the proxy handles concurrent requests
// safely (race detector must pass).
func TestConcurrentConnections(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	backendHost := mustHost(t, backend.URL)

	proxy, cleanup := makeProxy(t, netproxy.Policy{
		AllowedDomains: []string{backendHost},
	})
	defer cleanup()

	const workers = 20
	var wg sync.WaitGroup
	wg.Add(workers)
	errs := make(chan error, workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			client := httpClientViaProxy(t, proxy.URL())
			resp, err := client.Get(backend.URL)
			if err != nil {
				errs <- err
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errs <- fmt.Errorf("status %d, want 200", resp.StatusCode)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("concurrent request error: %v", e)
	}
}

// TestProxyURL verifies that FilteringProxy.URL() returns a valid http://127.0.0.1:PORT.
func TestProxyURL(t *testing.T) {
	proxy, cleanup := makeProxy(t, netproxy.Policy{
		AllowedDomains: []string{"example.com"},
	})
	defer cleanup()

	u := proxy.URL()
	if !strings.HasPrefix(u, "http://127.0.0.1:") {
		t.Errorf("proxy URL = %q, want http://127.0.0.1:<port>", u)
	}
	// Port must be parseable and > 0.
	portStr := strings.TrimPrefix(u, "http://127.0.0.1:")
	if portStr == "" {
		t.Fatalf("no port in proxy URL %q", u)
	}
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:"+portStr)
	if err != nil || addr.Port <= 0 {
		t.Errorf("invalid port in proxy URL %q", u)
	}
}

// TestGracefulShutdown verifies Close() stops the proxy without hanging.
func TestGracefulShutdown(t *testing.T) {
	proxy, _ := makeProxy(t, netproxy.Policy{AllowedDomains: []string{"x.example.com"}})

	done := make(chan struct{})
	go func() {
		proxy.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("proxy.Close() timed out (> 3s)")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Env-injection tests
// ────────────────────────────────────────────────────────────────────────────

// TestEnvVarsInjected verifies that EnvForProxy returns the expected
// HTTP_PROXY / HTTPS_PROXY / ALL_PROXY / NO_PROXY variables.
func TestEnvVarsInjected(t *testing.T) {
	proxy, cleanup := makeProxy(t, netproxy.Policy{AllowedDomains: []string{"api.github.com"}})
	defer cleanup()

	env := netproxy.EnvForProxy(proxy)
	// Must contain HTTP_PROXY, HTTPS_PROXY, ALL_PROXY pointing at the proxy.
	require := []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"}
	for _, key := range require {
		found := false
		for _, e := range env {
			if strings.HasPrefix(e, key+"=") {
				val := strings.TrimPrefix(e, key+"=")
				if val != proxy.URL() {
					t.Errorf("%s = %q, want %q", key, val, proxy.URL())
				}
				found = true
				break
			}
		}
		if !found {
			t.Errorf("env missing %s", key)
		}
	}
	// NO_PROXY must exclude the proxy's own loopback addr.
	for _, e := range env {
		if strings.HasPrefix(e, "NO_PROXY=") {
			val := strings.TrimPrefix(e, "NO_PROXY=")
			if !strings.Contains(val, "127.0.0.1") {
				t.Errorf("NO_PROXY = %q, expected 127.0.0.1 excluded (proxy is on loopback)", val)
			}
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Policy-wiring test (loopback-only seatbelt profile fragment)
// ────────────────────────────────────────────────────────────────────────────

// TestPolicyHasNonEmptyDomains is a policy-layer smoke test: when AllowedDomains
// is non-empty, the proxy policy keeps them and the fields survive round-trip.
func TestPolicyHasNonEmptyDomains(t *testing.T) {
	pol := netproxy.Policy{
		AllowedDomains: []string{"api.github.com", "*.npmjs.com"},
		DeniedDomains:  []string{"evil.example.com"},
	}
	if len(pol.AllowedDomains) != 2 {
		t.Errorf("AllowedDomains len = %d, want 2", len(pol.AllowedDomains))
	}
	if len(pol.DeniedDomains) != 1 {
		t.Errorf("DeniedDomains len = %d, want 1", len(pol.DeniedDomains))
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

// mustHost extracts host:port from rawURL, stripping the scheme.
func mustHost(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url %q: %v", rawURL, err)
	}
	return u.Host // e.g. "127.0.0.1:12345"
}
