//go:build linux

package sandbox

import (
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

func TestBuildSeccompNetworkFilterDenies(t *testing.T) {
	filter, err := buildSeccompNetworkFilter(false)
	if err != nil {
		t.Fatalf("buildSeccompNetworkFilter returned unexpected error: %v", err)
	}
	if len(filter) == 0 {
		t.Fatal("expected a non-empty seccomp program when denying network")
	}
	// The program must contain a SECCOMP_RET_ERRNO|EPERM deny action.
	deny := unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)
	var sawDeny bool
	for _, ins := range filter {
		if ins.K == deny {
			sawDeny = true
		}
	}
	if !sawDeny {
		t.Fatal("network-deny filter must contain a SECCOMP_RET_ERRNO|EPERM action")
	}
}

func TestBuildSeccompNetworkFilterAllowsWhenPermitted(t *testing.T) {
	// allowNetwork=true => no restrictive program (nil/empty is acceptable).
	filter, err := buildSeccompNetworkFilter(true)
	if err != nil {
		t.Fatalf("buildSeccompNetworkFilter(allow) returned unexpected error: %v", err)
	}
	if len(filter) != 0 {
		t.Fatalf("allowNetwork should yield no filter, got %d instructions", len(filter))
	}
}

// TestBuildSeccompFilterArchCheck verifies that the BPF program contains an
// AUDIT_ARCH prologue: instruction loading offset 4 (seccomp_data.arch) and
// a comparison against the expected AUDIT_ARCH constant for this binary's
// architecture, with KILL_PROCESS as the mismatch action (C1 fix).
func TestBuildSeccompFilterArchCheck(t *testing.T) {
	filter, err := buildSeccompNetworkFilter(false)
	if err != nil {
		t.Fatalf("buildSeccompNetworkFilter: %v", err)
	}

	expectedArch, ok := auditArchByGoarch[runtime.GOARCH]
	if !ok {
		t.Skipf("no AUDIT_ARCH mapping for GOARCH=%s; skipping arch-check assertion", runtime.GOARCH)
	}

	// There must be a BPF_LD|BPF_W|BPF_ABS instruction loading offset 4.
	var sawArchLoad bool
	for _, ins := range filter {
		if ins.Code == uint16(unix.BPF_LD|unix.BPF_W|unix.BPF_ABS) && ins.K == 4 {
			sawArchLoad = true
			break
		}
	}
	if !sawArchLoad {
		t.Fatal("BPF program must load seccomp_data.arch (BPF_LD|BPF_W|BPF_ABS offset 4)")
	}

	// There must be a JEQ instruction comparing against the expected AUDIT_ARCH.
	var sawArchJEQ bool
	for _, ins := range filter {
		if ins.Code == uint16(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K) && ins.K == expectedArch {
			sawArchJEQ = true
			break
		}
	}
	if !sawArchJEQ {
		t.Fatalf("BPF program must compare arch against AUDIT_ARCH 0x%x for GOARCH=%s",
			expectedArch, runtime.GOARCH)
	}

	// There must be a KILL_PROCESS return for arch mismatch.
	var sawKill bool
	for _, ins := range filter {
		if ins.Code == uint16(unix.BPF_RET|unix.BPF_K) && ins.K == uint32(unix.SECCOMP_RET_KILL_PROCESS) {
			sawKill = true
			break
		}
	}
	if !sawKill {
		t.Fatal("BPF program must contain SECCOMP_RET_KILL_PROCESS for arch mismatch")
	}
}

// TestBuildSeccompFilterBlocksConnectSendto verifies that the BPF program
// blocks connect(2), sendto(2), and sendmsg(2) in addition to socket(2),
// preventing use of inherited/open sockets for network egress (C2 fix).
func TestBuildSeccompFilterBlocksConnectSendto(t *testing.T) {
	filter, err := buildSeccompNetworkFilter(false)
	if err != nil {
		t.Fatalf("buildSeccompNetworkFilter: %v", err)
	}

	deniedSyscalls := map[string]uint32{
		"SYS_SOCKET":  uint32(unix.SYS_SOCKET),
		"SYS_CONNECT": uint32(unix.SYS_CONNECT),
		"SYS_SENDTO":  uint32(unix.SYS_SENDTO),
		"SYS_SENDMSG": uint32(unix.SYS_SENDMSG),
	}

	for name, nr := range deniedSyscalls {
		var found bool
		for _, ins := range filter {
			if ins.Code == uint16(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K) && ins.K == nr {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("BPF program must include a JEQ check for %s (nr=%d)", name, nr)
		}
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
