//go:build linux

package sandbox

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestBuildSeccompNetworkFilterDenies(t *testing.T) {
	filter := buildSeccompNetworkFilter(false)
	if len(filter) == 0 {
		t.Fatal("expected a non-empty seccomp program when denying network")
	}
	// The program must reference the socket syscall and a deny return.
	var sawDeny bool
	for _, ins := range filter {
		if ins.K == (unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)) {
			sawDeny = true
		}
	}
	if !sawDeny {
		t.Fatal("network-deny filter must contain a SECCOMP_RET_ERRNO|EPERM action")
	}
}

func TestBuildSeccompNetworkFilterAllowsWhenPermitted(t *testing.T) {
	// allowNetwork=true => no restrictive program (nil/empty is acceptable).
	if f := buildSeccompNetworkFilter(true); len(f) != 0 {
		t.Fatalf("allowNetwork should yield no filter, got %d instructions", len(f))
	}
}

func TestWrapLinuxReexecsChild(t *testing.T) {
	name, args, err := wrap("/bin/sh", []string{"-c", "echo hi"}, Policy{Enabled: true})
	if err != nil {
		t.Fatalf("wrap err: %v", err)
	}
	if name == "/bin/sh" {
		t.Fatal("linux wrap must re-exec the ccgo binary, not the raw command")
	}
	if len(args) < 4 || args[0] != ChildSentinel {
		t.Fatalf("wrap must prefix the child sentinel: %v", args)
	}
}
