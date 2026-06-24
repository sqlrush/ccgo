// Package netproxy implements a local domain-filtering HTTP/HTTPS proxy for
// sandbox per-domain network enforcement (SBX-48).
//
// Architecture: the proxy listens on a loopback port and handles two kinds of
// requests:
//   - Plain HTTP (CONNECT-less): rewrite the request Host → forward to backend.
//   - CONNECT (used for HTTPS): check the target host; if allowed, tunnel the
//     TCP connection; if denied, return 403.
//
// Domain policy: AllowedDomains is an explicit allow-list; DeniedDomains takes
// precedence over AllowedDomains. The proxy is deny-by-default: a host not
// present in AllowedDomains (even if not in DeniedDomains) is blocked.
//
// Wildcard matching: "*.github.com" matches "api.github.com" but NOT
// "github.com" or "evil.api.github.com" (single-label wildcard only).
//
// Concurrency: the proxy is safe for concurrent requests; it uses no mutable
// shared state after construction.
//
// OS portability: the proxy itself (TCP listen + HTTP handling) is fully
// OS-agnostic. The "restrict direct network to loopback only" rule is emitted
// by the seatbelt profile generator (profile_darwin.go) when a FilteringProxy
// is active, and is a separate concern for Linux landlock/seccomp callers.
package netproxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Policy defines the per-domain filtering rules for the proxy.
// It is immutable after construction (no mutation after Start).
type Policy struct {
	// AllowedDomains is the explicit domain allow-list.
	// Supports exact match ("github.com") and single-label suffix wildcard
	// ("*.github.com"). An empty list means deny all (deny-by-default).
	AllowedDomains []string

	// DeniedDomains is the explicit deny-list. It takes precedence over
	// AllowedDomains: a host in both lists is always blocked.
	DeniedDomains []string
}

// FilteringProxy is a running local HTTP proxy that filters by domain.
// It must be started with Start and stopped with Close.
// All exported methods are safe for concurrent use.
type FilteringProxy struct {
	server   *http.Server
	listener net.Listener
	addr     string // "127.0.0.1:<port>" — set once before serving
	policy   Policy
	wg       sync.WaitGroup
}

// Start creates and starts a FilteringProxy on a random loopback port.
// It returns immediately once the server is accepting connections.
// The caller must call Close when done to release resources.
func Start(policy Policy) (*FilteringProxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("netproxy: listen: %w", err)
	}

	fp := &FilteringProxy{
		listener: ln,
		addr:     ln.Addr().String(),
		policy:   policy,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", fp.handleRequest)

	fp.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	fp.wg.Add(1)
	go func() {
		defer fp.wg.Done()
		// Serve returns ErrServerClosed on Clean shutdown — ignore it.
		_ = fp.server.Serve(ln)
	}()

	return fp, nil
}

// URL returns the proxy URL as "http://127.0.0.1:<port>".
// This is the value to set in HTTP_PROXY / HTTPS_PROXY / ALL_PROXY.
func (fp *FilteringProxy) URL() string {
	return "http://" + fp.addr
}

// Addr returns the "host:port" address the proxy is listening on.
func (fp *FilteringProxy) Addr() string {
	return fp.addr
}

// Close shuts down the proxy gracefully and waits for in-flight connections.
func (fp *FilteringProxy) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = fp.server.Shutdown(ctx)
	fp.wg.Wait()
}

// handleRequest is the single http.HandlerFunc. It dispatches:
//   - CONNECT requests → tunnel (for HTTPS)
//   - everything else → plain HTTP forward
func (fp *FilteringProxy) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		fp.handleConnect(w, r)
		return
	}
	fp.handleHTTP(w, r)
}

// handleConnect implements an HTTP CONNECT tunnel. For allowed hosts it
// establishes a raw TCP connection and bridges bytes. For denied hosts it
// returns 403.
func (fp *FilteringProxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	host := r.Host // "host:port" for CONNECT
	if !fp.isAllowed(host) {
		http.Error(w, "403 Forbidden: domain not in allowed list", http.StatusForbidden)
		return
	}

	// Hijack the connection to tunnel raw TCP.
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "proxy: hijack not supported", http.StatusInternalServerError)
		return
	}

	upstream, err := net.DialTimeout("tcp", host, 10*time.Second)
	if err != nil {
		http.Error(w, fmt.Sprintf("proxy: dial upstream: %v", err), http.StatusBadGateway)
		return
	}
	defer upstream.Close()

	// Signal CONNECT established.
	clientConn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	defer clientConn.Close()

	_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))

	// Bridge bytes bidirectionally.
	var bridgeWG sync.WaitGroup
	bridgeWG.Add(2)
	go func() {
		defer bridgeWG.Done()
		_, _ = io.Copy(upstream, clientConn)
		// Signal upstream we're done writing so it can stop reading.
		if tc, ok := upstream.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()
	go func() {
		defer bridgeWG.Done()
		_, _ = io.Copy(clientConn, upstream)
		if tc, ok := clientConn.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()
	bridgeWG.Wait()
}

// handleHTTP forwards a plain HTTP request to the upstream server.
func (fp *FilteringProxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if !fp.isAllowed(host) {
		http.Error(w, "403 Forbidden: domain not in allowed list", http.StatusForbidden)
		return
	}

	// Build outbound request (strip Proxy-Connection, adjust URL).
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, r.RequestURI, r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("proxy: build request: %v", err), http.StatusInternalServerError)
		return
	}
	// Copy headers, excluding hop-by-hop headers.
	copyHeaders(outReq.Header, r.Header)
	outReq.Header.Del("Proxy-Connection")

	resp, err := http.DefaultTransport.RoundTrip(outReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("proxy: upstream: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers + body.
	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// isAllowed implements the domain filtering logic:
//  1. Strip port from host if present.
//  2. If host matches any DeniedDomains pattern → DENY (deny takes precedence).
//  3. If AllowedDomains is empty → DENY (deny-by-default).
//  4. If host matches any AllowedDomains pattern → ALLOW.
//  5. Otherwise → DENY.
func (fp *FilteringProxy) isAllowed(host string) bool {
	bare := stripPort(host)

	// DeniedDomains takes precedence.
	for _, pat := range fp.policy.DeniedDomains {
		if DomainMatches(pat, bare) {
			return false
		}
	}

	// Deny-by-default: empty allowed list means nothing is allowed.
	if len(fp.policy.AllowedDomains) == 0 {
		return false
	}

	// Allow if any pattern matches.
	for _, pat := range fp.policy.AllowedDomains {
		if DomainMatches(pat, bare) {
			return true
		}
	}
	return false
}

// DomainMatches reports whether pattern matches host.
// Pattern forms:
//   - "github.com"   → exact match only
//   - "*.github.com" → matches "api.github.com" but not "github.com" or
//     "evil.api.github.com" (single-label wildcard)
//
// Both pattern and host may include a port suffix ("github.com:443") which
// is stripped before matching. This allows passing patterns like
// "127.0.0.1:12345" (as used in tests with httptest servers) as well as
// bare domain names.
func DomainMatches(pattern, host string) bool {
	pattern = stripPort(pattern)
	host = stripPort(host)
	if strings.HasPrefix(pattern, "*.") {
		// "*.github.com" → suffix must be ".github.com" and host must have
		// exactly one label before that suffix (no dots in the prefix label).
		suffix := pattern[1:] // ".github.com"
		if !strings.HasSuffix(host, suffix) {
			return false
		}
		prefix := host[:len(host)-len(suffix)] // "api" or "evil.api"
		return len(prefix) > 0 && !strings.Contains(prefix, ".")
	}
	return host == pattern
}

// stripPort removes the :port suffix from a host string if present.
func stripPort(host string) string {
	// Use net.SplitHostPort but fall back to the raw string on error.
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// copyHeaders copies all header values from src to dst.
func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// EnvForProxy returns the environment variables that should be injected into
// a sandboxed process to route its HTTP/HTTPS traffic through fp.
//
// Variables set (all pointing at fp.URL()):
//   - HTTP_PROXY
//   - HTTPS_PROXY
//   - ALL_PROXY (used by curl and other tools)
//
// NO_PROXY is set to exclude 127.0.0.1 so the sandboxed process can still
// reach the proxy itself without triggering a proxy loop.
func EnvForProxy(fp *FilteringProxy) []string {
	u := fp.URL()
	return []string{
		"HTTP_PROXY=" + u,
		"HTTPS_PROXY=" + u,
		"ALL_PROXY=" + u,
		"NO_PROXY=127.0.0.1,localhost,::1",
	}
}
