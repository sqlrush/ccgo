package netproxy_test

import (
	"strings"
	"testing"

	"ccgo/internal/sandbox/netproxy"
)

// TestNeedsProxy verifies the NeedsProxy gate.
func TestNeedsProxy(t *testing.T) {
	cases := []struct {
		allowed []string
		denied  []string
		want    bool
	}{
		{nil, nil, false},
		{[]string{}, []string{}, false},
		{[]string{"github.com"}, nil, true},
		{nil, []string{"evil.com"}, true},
		{[]string{"github.com"}, []string{"evil.com"}, true},
	}
	for _, tc := range cases {
		got := netproxy.NeedsProxy(tc.allowed, tc.denied)
		if got != tc.want {
			t.Errorf("NeedsProxy(%v, %v) = %v, want %v", tc.allowed, tc.denied, got, tc.want)
		}
	}
}

// TestStartForSandbox verifies the helper starts a proxy and returns env vars.
func TestStartForSandbox(t *testing.T) {
	fp, envVars, stop, err := netproxy.StartForSandbox(
		[]string{"api.github.com"},
		[]string{"evil.example.com"},
	)
	if err != nil {
		t.Fatalf("StartForSandbox: %v", err)
	}
	defer stop()

	if fp == nil {
		t.Fatal("FilteringProxy is nil")
	}

	// Proxy URL must be loopback.
	proxyURL := fp.URL()
	if !strings.HasPrefix(proxyURL, "http://127.0.0.1:") {
		t.Errorf("proxy URL = %q, want http://127.0.0.1:<port>", proxyURL)
	}

	// Env vars must include HTTP_PROXY, HTTPS_PROXY, ALL_PROXY pointing at proxy.
	required := []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"}
	for _, key := range required {
		found := false
		for _, e := range envVars {
			if strings.HasPrefix(e, key+"=") {
				val := strings.TrimPrefix(e, key+"=")
				if val != proxyURL {
					t.Errorf("env %s = %q, want %q", key, val, proxyURL)
				}
				found = true
				break
			}
		}
		if !found {
			t.Errorf("env missing %s", key)
		}
	}
}

// TestStartForSandboxNilWhenNoRules verifies that StartForSandbox on empty
// domain lists still works (returns a proxy in this case — policy decides
// whether to call it; NeedsProxy should be used as the gate first).
// This test just ensures the function doesn't panic on valid empty input.
func TestDomainMatchesPortStripping(t *testing.T) {
	// Patterns with ports should match hosts with or without ports.
	cases := []struct {
		pattern string
		host    string
		want    bool
	}{
		// Pattern with port, host without
		{"127.0.0.1:8080", "127.0.0.1", true},
		// Pattern without port, host with port
		{"github.com", "github.com:443", true},
		// Wildcard with host+port
		{"*.github.com", "api.github.com:443", true},
		// Non-match with ports
		{"github.com", "evil.com:443", false},
	}
	for _, tc := range cases {
		got := netproxy.DomainMatches(tc.pattern, tc.host)
		if got != tc.want {
			t.Errorf("DomainMatches(%q, %q) = %v, want %v", tc.pattern, tc.host, got, tc.want)
		}
	}
}
