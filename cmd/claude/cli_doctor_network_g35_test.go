package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDoctorCommand_NetworkCheckEndpointWired verifies that when --check-network
// is passed, the production doctor command path probes the given URL.
// SUBCMD-DOCTOR-10: prod call site adds NetworkCheckEndpoint to doctor.Input.
func TestDoctorCommand_NetworkCheckEndpointWired(t *testing.T) {
	// Start a real HTTP test server to serve as the network check endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var out bytes.Buffer
	var errOut bytes.Buffer

	// Run doctor with --check-network pointing to the test server.
	rc := runDoctorCommand([]string{"--check-network", srv.URL}, "/tmp", &out, &errOut)
	output := out.String()

	// Non-error exit code.
	if rc == 1 {
		t.Fatalf("unexpected exit code 1; output=%q err=%q", output, errOut.String())
	}
	// The network check result should appear in the doctor output.
	if !strings.Contains(output, "Network connectivity") {
		t.Errorf("expected 'Network connectivity' check in output, got:\n%s", output)
	}
	// The endpoint should be reachable.
	if !strings.Contains(output, "reachable") {
		t.Errorf("expected 'reachable' in output, got:\n%s", output)
	}
}

// TestDoctorCommand_NetworkCheckOmittedByDefault verifies that without
// --check-network, no network check is emitted.
// SUBCMD-DOCTOR-10: default is network-free.
func TestDoctorCommand_NetworkCheckOmittedByDefault(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	_ = runDoctorCommand(nil, "/tmp", &out, &errOut)
	output := out.String()
	if strings.Contains(output, "Network connectivity") {
		t.Errorf("expected no 'Network connectivity' check without --check-network, got:\n%s", output)
	}
}
