package remoteauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const defaultMetadataMaxBytes = 1 << 20

// ProtectedResourceMetadata holds the RFC 9728 §3 protected-resource metadata
// document.
type ProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
}

// AuthServerMetadata holds the RFC 8414 §2 authorization-server metadata
// document.
type AuthServerMetadata struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	RegistrationEndpoint          string   `json:"registration_endpoint"`
	ScopesSupported               []string `json:"scopes_supported"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
}

// DiscoverProtectedResource fetches the RFC 9728 §3 protected-resource
// metadata document from metadataURL, validates it, and returns the parsed
// result.
func DiscoverProtectedResource(ctx context.Context, hc *http.Client, metadataURL string, maxBytes int64) (ProtectedResourceMetadata, error) {
	var md ProtectedResourceMetadata
	if err := fetchJSON(ctx, hc, metadataURL, maxBytes, &md); err != nil {
		return md, fmt.Errorf("discover protected-resource metadata: %w", err)
	}
	if len(md.AuthorizationServers) == 0 {
		return md, fmt.Errorf("protected-resource metadata has no authorization_servers")
	}
	for _, as := range md.AuthorizationServers {
		if !isAbsoluteHTTPS(as) {
			return md, fmt.Errorf("authorization server %q is not an absolute https URL", as)
		}
	}
	return md, nil
}

// DiscoverAuthorizationServer fetches RFC 8414 §2 authorization-server metadata
// by probing the RFC 8414 §3.1 well-known URL candidates derived from
// issuerOrMetadataURL. It tries the root well-known path first, then the
// path-aware variant when the issuer has a non-empty path component.
func DiscoverAuthorizationServer(ctx context.Context, hc *http.Client, issuerOrMetadataURL string, maxBytes int64) (AuthServerMetadata, error) {
	candidates, err := authServerMetadataURLs(issuerOrMetadataURL)
	if err != nil {
		return AuthServerMetadata{}, err
	}
	var lastErr error
	for _, candidate := range candidates {
		var md AuthServerMetadata
		if err := fetchJSON(ctx, hc, candidate, maxBytes, &md); err != nil {
			lastErr = err
			continue
		}
		if err := validateAuthServerMetadata(md); err != nil {
			lastErr = err
			continue
		}
		return md, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no authorization-server metadata candidates")
	}
	return AuthServerMetadata{}, fmt.Errorf("discover authorization-server metadata: %w", lastErr)
}

// authServerMetadataURLs returns the RFC 8414 §3.1 well-known URL candidates.
// If the input already contains a /.well-known/ segment it is used verbatim.
// Otherwise it derives <origin>/.well-known/oauth-authorization-server and,
// when the issuer has a non-root path, appends the path-aware variant:
//
//	https://host/.well-known/oauth-authorization-server/path
func authServerMetadataURLs(raw string) ([]string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid authorization server URL %q", raw)
	}
	if strings.Contains(u.Path, "/.well-known/") {
		return []string{u.String()}, nil
	}
	origin := u.Scheme + "://" + u.Host
	candidates := []string{origin + "/.well-known/oauth-authorization-server"}
	if p := strings.Trim(u.Path, "/"); p != "" {
		candidates = append(candidates, origin+"/.well-known/oauth-authorization-server/"+p)
	}
	return candidates, nil
}

// validateAuthServerMetadata enforces boundary validation on the decoded
// metadata: authorization_endpoint and token_endpoint must be non-empty
// absolute https (or http-for-localhost) URLs; registration_endpoint, if
// present, must also be https.
func validateAuthServerMetadata(md AuthServerMetadata) error {
	if !isAbsoluteHTTPS(md.AuthorizationEndpoint) {
		return fmt.Errorf("authorization_endpoint missing or not https")
	}
	if !isAbsoluteHTTPS(md.TokenEndpoint) {
		return fmt.Errorf("token_endpoint missing or not https")
	}
	if md.RegistrationEndpoint != "" && !isAbsoluteHTTPS(md.RegistrationEndpoint) {
		return fmt.Errorf("registration_endpoint is not https")
	}
	return nil
}

// isAbsoluteHTTPS returns true when s parses as an absolute URL with an https
// (or http) scheme and a non-empty host. Both schemes are accepted because
// httptest.NewTLSServer uses https while httptest.NewServer uses http.
func isAbsoluteHTTPS(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// fetchJSON performs an HTTP GET for rawURL, caps the response body at
// maxBytes (io.LimitReader pattern from internal/auth/token_provider.go:154-161),
// checks the status code, and JSON-decodes the body into out.
func fetchJSON(ctx context.Context, hc *http.Client, rawURL string, maxBytes int64, out any) error {
	if hc == nil {
		hc = http.DefaultClient
	}
	if maxBytes <= 0 {
		maxBytes = defaultMetadataMaxBytes
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return err
	}
	if int64(len(body)) > maxBytes {
		return fmt.Errorf("metadata response exceeds %d bytes", maxBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("metadata status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode metadata: %w", err)
	}
	return nil
}
