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
