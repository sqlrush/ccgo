//go:build linux

package sandbox

import "golang.org/x/sys/unix"

// buildSeccompNetworkFilter returns a classic BPF program that blocks the
// socket(2) syscall (and thus all new network sockets) by returning EPERM,
// while allowing everything else. Returns nil when network is permitted.
//
// Linux seccomp cannot filter by path (see sandboxTypes.ts:29), so filesystem
// confinement is handled by landlock; this covers only network egress.
//
// unix.SYS_SOCKET is the architecture-specific syscall constant provided by
// x/sys (e.g., 41 on amd64, 198 on arm64/riscv64/loong64, 281 on arm32,
// 326 on ppc64le, 359 on 386). Since landlock itself requires kernel ≥5.13,
// we target only modern kernels where direct socket(2) is always available.
func buildSeccompNetworkFilter(allowNetwork bool) []unix.SockFilter {
	if allowNetwork {
		return nil
	}

	deny := unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)

	return []unix.SockFilter{
		// Load syscall number: A = seccomp_data.nr (offset 0).
		bpfStmt(unix.BPF_LD|unix.BPF_W|unix.BPF_ABS, 0),
		// if A == SYS_SOCKET → deny, else fall through to allow.
		bpfJump(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K, uint32(unix.SYS_SOCKET), 0, 1),
		bpfStmt(unix.BPF_RET|unix.BPF_K, deny),
		bpfStmt(unix.BPF_RET|unix.BPF_K, unix.SECCOMP_RET_ALLOW),
	}
}

func bpfStmt(code uint16, k uint32) unix.SockFilter {
	return unix.SockFilter{Code: code, K: k}
}

func bpfJump(code uint16, k uint32, jt, jf uint8) unix.SockFilter {
	return unix.SockFilter{Code: code, Jt: jt, Jf: jf, K: k}
}
