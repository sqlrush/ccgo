package remoteauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ClientMetadata is the RFC 7591 §2 client registration metadata.
// CC reference: services/mcp/auth.ts:1417-1437.
type ClientMetadata struct {
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
}

// RegisteredClient holds the fields returned by the registration endpoint.
type RegisteredClient struct {
	ClientID                string `json:"client_id"`
	ClientSecret            string `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64  `json:"client_id_issued_at,omitempty"`
	RegistrationAccessToken string `json:"registration_access_token,omitempty"`
}

// RegisterClient performs RFC 7591 Dynamic Client Registration by POSTing meta
// as JSON to registrationEndpoint and returning the parsed RegisteredClient.
//
// Validations:
//   - registrationEndpoint must be https (or loopback http) — isAbsoluteHTTPS.
//   - Response body is capped at maxBytes (io.LimitReader pattern).
//   - Non-2xx responses produce an error that includes the status code only
//     (no body echoed to avoid leaking sensitive detail).
//   - An empty client_id in the response is rejected.
func RegisterClient(ctx context.Context, hc *http.Client, registrationEndpoint string, meta ClientMetadata, maxBytes int64) (RegisteredClient, error) {
	if !isAbsoluteHTTPS(registrationEndpoint) {
		return RegisteredClient{}, fmt.Errorf("registrationEndpoint %q must be an absolute https (or loopback http) URL", registrationEndpoint)
	}
	if hc == nil {
		hc = http.DefaultClient
	}
	if maxBytes <= 0 {
		maxBytes = defaultMetadataMaxBytes
	}

	payload, err := json.Marshal(meta)
	if err != nil {
		return RegisteredClient{}, fmt.Errorf("remoteauth: marshal client metadata: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registrationEndpoint, bytes.NewReader(payload))
	if err != nil {
		return RegisteredClient{}, fmt.Errorf("remoteauth: build registration request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := hc.Do(req)
	if err != nil {
		return RegisteredClient{}, fmt.Errorf("remoteauth: registration request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return RegisteredClient{}, fmt.Errorf("remoteauth: read registration response: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return RegisteredClient{}, fmt.Errorf("remoteauth: registration response exceeds %d bytes", maxBytes)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Surface status code only — never echo body which may contain secrets.
		return RegisteredClient{}, fmt.Errorf("remoteauth: client registration failed with status %d", resp.StatusCode)
	}

	var rc RegisteredClient
	if err := json.Unmarshal(body, &rc); err != nil {
		return RegisteredClient{}, fmt.Errorf("remoteauth: decode registration response: %w", err)
	}
	if strings.TrimSpace(rc.ClientID) == "" {
		return RegisteredClient{}, fmt.Errorf("remoteauth: registration response missing client_id")
	}
	return rc, nil
}
