package doctor

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNetworkCheckOmittedByDefault verifies that when NetworkCheckEndpoint is empty
// no network check is emitted.
// SUBCMD-DOCTOR-10: default off preserves network-free behaviour.
func TestNetworkCheckOmittedByDefault(t *testing.T) {
	report := Run(Input{Version: "1.0.0"})
	for _, c := range report.Checks {
		if c.Name == "Network connectivity" {
			t.Errorf("expected no network check by default, got: %+v", c)
		}
	}
}

// TestNetworkCheckOptIn_Reachable verifies that when the endpoint is reachable
// the check is StatusOK.
// SUBCMD-DOCTOR-10: opt-in network check with httptest server.
func TestNetworkCheckOptIn_Reachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	report := Run(Input{
		Version:              "1.0.0",
		NetworkCheckEndpoint: srv.URL,
		// Use DefaultNetworkCheckFn via nil — but to avoid real HTTP in tests inject a fake.
		NetworkCheckFn: func(url string) error {
			resp, err := http.Get(url) //nolint:noctx
			if err != nil {
				return err
			}
			_ = resp.Body.Close()
			return nil
		},
	})

	var found *Check
	for i := range report.Checks {
		if report.Checks[i].Name == "Network connectivity" {
			found = &report.Checks[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected Network connectivity check to be present")
	}
	if found.Status != StatusOK {
		t.Errorf("expected StatusOK, got %q: %s", found.Status, found.Detail)
	}
	if !strings.Contains(found.Detail, "reachable") {
		t.Errorf("expected 'reachable' in detail, got %q", found.Detail)
	}
}

// TestNetworkCheckOptIn_Unreachable verifies that when the probe returns an error
// the check is StatusWarn.
// SUBCMD-DOCTOR-10: unreachable endpoint → WARN not ERROR (non-fatal).
func TestNetworkCheckOptIn_Unreachable(t *testing.T) {
	report := Run(Input{
		Version:              "1.0.0",
		NetworkCheckEndpoint: "http://localhost:0",
		NetworkCheckFn: func(url string) error {
			return errors.New("connection refused")
		},
	})

	var found *Check
	for i := range report.Checks {
		if report.Checks[i].Name == "Network connectivity" {
			found = &report.Checks[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected Network connectivity check to be present")
	}
	if found.Status != StatusWarn {
		t.Errorf("expected StatusWarn for unreachable endpoint, got %q", found.Status)
	}
	if !strings.Contains(found.Detail, "cannot reach") {
		t.Errorf("expected 'cannot reach' in detail, got %q", found.Detail)
	}
}

// TestNetworkCheckOptIn_InjectedFn verifies that NetworkCheckFn is called with
// the configured endpoint URL.
// SUBCMD-DOCTOR-10: injection seam works correctly.
func TestNetworkCheckOptIn_InjectedFn(t *testing.T) {
	called := false
	calledURL := ""
	report := Run(Input{
		Version:              "1.0.0",
		NetworkCheckEndpoint: "https://example.com/health",
		NetworkCheckFn: func(url string) error {
			called = true
			calledURL = url
			return nil
		},
	})

	if !called {
		t.Error("expected NetworkCheckFn to be called")
	}
	if calledURL != "https://example.com/health" {
		t.Errorf("expected URL='https://example.com/health', got %q", calledURL)
	}
	var found bool
	for _, c := range report.Checks {
		if c.Name == "Network connectivity" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected Network connectivity check in report")
	}
}

// TestNetworkCheckOptIn_HttptestRoundtrip verifies the full flow with httptest.
// SUBCMD-DOCTOR-10.
func TestNetworkCheckOptIn_HttptestRoundtrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead && r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	check := networkConnectivityCheck(srv.URL, func(url string) error {
		resp, err := http.Head(url) //nolint:noctx
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		return nil
	})
	if check.Status != StatusOK {
		t.Errorf("expected StatusOK, got %q: %s", check.Status, check.Detail)
	}
}
