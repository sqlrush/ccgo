//go:build darwin

package sandbox

import (
	"strings"
	"testing"
)

func TestBuildSeatbeltProfileDenyDefault(t *testing.T) {
	p := Policy{
		Enabled:    true,
		AllowWrite: []string{"/tmp/work"},
		DenyRead:   []string{"/etc/secret"},
	}
	profile := buildSeatbeltProfile(p, "/tmp/work")
	if !strings.HasPrefix(profile, "(version 1)") {
		t.Fatalf("profile must start with version: %q", profile[:20])
	}
	if !strings.Contains(profile, "(deny default)") {
		t.Fatal("profile must deny by default")
	}
	if !strings.Contains(profile, `(subpath "/tmp/work")`) {
		t.Fatalf("profile must allow writes under allowWrite: %s", profile)
	}
	if !strings.Contains(profile, `(subpath "/etc/secret")`) {
		t.Fatalf("profile must deny reads of denyRead paths: %s", profile)
	}
}

// TestBuildSeatbeltProfileAllowUnixSockets verifies SBX-49:
// AllowUnixSockets paths are emitted as (allow network-outbound (path "...")) rules.
func TestBuildSeatbeltProfileAllowUnixSockets(t *testing.T) {
	p := Policy{
		Enabled:          true,
		AllowUnixSockets: []string{"/var/run/docker.sock", "/run/containerd.sock"},
	}
	profile := buildSeatbeltProfile(p, "/tmp/work")
	for _, sock := range p.AllowUnixSockets {
		want := `(allow network-outbound (path "` + sock + `"))`
		if !strings.Contains(profile, want) {
			t.Fatalf("SBX-49: profile missing unix-socket rule for %q\nprofile:\n%s", sock, profile)
		}
	}
}

// TestBuildSeatbeltProfileNoUnixSockets verifies that when AllowUnixSockets is
// empty the profile contains no spurious (allow network-outbound (path ...)) rules.
func TestBuildSeatbeltProfileNoUnixSockets(t *testing.T) {
	p := Policy{Enabled: true}
	profile := buildSeatbeltProfile(p, "/tmp/work")
	if strings.Contains(profile, `(allow network-outbound (path`) {
		t.Fatalf("SBX-49: profile must not emit unix-socket rules when AllowUnixSockets is empty:\n%s", profile)
	}
}

func TestWrapDarwinUsesSandboxExec(t *testing.T) {
	name, args, err := Wrap("/bin/zsh", []string{"-c", "echo hi"}, Policy{Enabled: true})
	if err != nil {
		t.Fatalf("Wrap err: %v", err)
	}
	if name != "/usr/bin/sandbox-exec" {
		t.Fatalf("expected sandbox-exec wrapper, got %q", name)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "/bin/zsh") || !strings.Contains(joined, "echo hi") {
		t.Fatalf("wrapped args lost the original command: %v", args)
	}
}

// TestBuildSeatbeltProfileDomainPolicyComment verifies SBX-48:
// When AllowedDomains or DeniedDomains are set, the profile includes
// comment lines documenting the policy-layer intent.
// Per-domain enforcement requires a proxy layer (seatbelt has no DNS-level rules).
func TestBuildSeatbeltProfileDomainPolicyComment(t *testing.T) {
	p := Policy{
		Enabled:       true,
		AllowedDomains: []string{"api.github.com", "registry.npmjs.org"},
		DeniedDomains:  []string{"evil.com"},
	}
	profile := buildSeatbeltProfile(p, "/tmp/work")
	// The profile must document the configured domains via comments.
	if !strings.Contains(profile, "api.github.com") {
		t.Fatalf("SBX-48: profile must include allowed domain 'api.github.com'; got:\n%s", profile)
	}
	if !strings.Contains(profile, "evil.com") {
		t.Fatalf("SBX-48: profile must include denied domain 'evil.com'; got:\n%s", profile)
	}
}

// TestBuildSeatbeltProfileNoDomainCommentWhenEmpty verifies that no spurious
// domain comment is emitted when AllowedDomains and DeniedDomains are empty.
func TestBuildSeatbeltProfileNoDomainCommentWhenEmpty(t *testing.T) {
	p := Policy{Enabled: true}
	profile := buildSeatbeltProfile(p, "/tmp/work")
	if strings.Contains(profile, "allowed-domains:") || strings.Contains(profile, "denied-domains:") {
		t.Fatalf("SBX-48: profile must not emit domain comments when no domains configured:\n%s", profile)
	}
}

// TestBuildSeatbeltProfileWithProxyPort verifies SBX-48: when a proxy port is
// supplied, the seatbelt profile restricts direct network access to the proxy's
// loopback port only, forcing all traffic through the domain-filtering proxy.
func TestBuildSeatbeltProfileWithProxyPort(t *testing.T) {
	p := Policy{
		Enabled:        true,
		AllowedDomains: []string{"api.github.com"},
		DeniedDomains:  []string{"evil.com"},
	}
	const proxyPort = 54321
	profile := buildSeatbeltProfileWithProxy(p, "/tmp/work", proxyPort)

	// Must deny default network.
	if !strings.Contains(profile, "(deny network*)") {
		t.Errorf("SBX-48 proxy profile must contain '(deny network*)'; got:\n%s", profile)
	}
	// Must allow outbound only to the proxy port.
	wantRule := `(allow network-outbound (remote ip "localhost:54321"))`
	if !strings.Contains(profile, wantRule) {
		t.Errorf("SBX-48 proxy profile must allow only proxy port loopback:\nwant: %s\ngot:\n%s", wantRule, profile)
	}
	// Must NOT emit a blanket (allow network*).
	if strings.Contains(profile, "(allow network*)") {
		t.Errorf("SBX-48 proxy profile must not emit blanket '(allow network*)'; got:\n%s", profile)
	}
	// Must document the configured domains.
	if !strings.Contains(profile, "api.github.com") {
		t.Errorf("SBX-48: proxy profile must document allowed domain; got:\n%s", profile)
	}
}

// TestBuildSeatbeltProfileWithProxyPortZeroFallsBack verifies that proxyPort=0
// uses the normal AllowNetwork rule (no proxy-port injection).
func TestBuildSeatbeltProfileWithProxyPortZeroFallsBack(t *testing.T) {
	p := Policy{Enabled: true, AllowNetwork: true}
	profile := buildSeatbeltProfileWithProxy(p, "/tmp/work", 0)
	if !strings.Contains(profile, "(allow network*)") {
		t.Errorf("profile with AllowNetwork=true and proxyPort=0 must contain '(allow network*)'; got:\n%s", profile)
	}
}
