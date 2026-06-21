//go:build linux

package sandbox

import (
	"fmt"
	"runtime"

	"golang.org/x/sys/unix"
)

// auditArchByGoarch maps runtime.GOARCH values to the AUDIT_ARCH_* constant
// that the Linux kernel embeds in seccomp_data.arch for that architecture.
// Only architectures that can run Go natively are listed. If the running arch
// is not present the filter-builder returns an error rather than silently
// omitting the arch check.
var auditArchByGoarch = map[string]uint32{
	"amd64":   unix.AUDIT_ARCH_X86_64,
	"arm64":   unix.AUDIT_ARCH_AARCH64,
	"arm":     unix.AUDIT_ARCH_ARM,
	"386":     0x40000003, // AUDIT_ARCH_I386
	"ppc64le": 0xc0000015, // AUDIT_ARCH_PPC64LE
	"s390x":   0x80000016, // AUDIT_ARCH_S390X
	"riscv64": 0xc00000f3, // AUDIT_ARCH_RISCV64
	"mips":    0x08000008, // AUDIT_ARCH_MIPS
	"mips64":  0x80000008, // AUDIT_ARCH_MIPS64
	"mipsle":  0x40000008, // AUDIT_ARCH_MIPSEL
	"mips64le": 0xc0000008, // AUDIT_ARCH_MIPSEL64
	"loong64": 0xc0000102, // AUDIT_ARCH_LOONGARCH64
}

// buildSeccompNetworkFilter returns a classic BPF program that:
//  1. Validates seccomp_data.arch (offset 4) against the expected AUDIT_ARCH
//     for this binary's architecture — a mismatch kills the process immediately
//     (closes the 32-bit compat bypass: a compat process reports different
//     syscall numbers, making nr-only filters meaningless).
//  2. Denies socket(2), connect(2), sendto(2), and sendmsg(2) by returning
//     EPERM — blocking both new-socket creation and use of inherited/open fds
//     for network egress.
//
// Returns nil when network is permitted (AllowNetwork=true); the caller must
// skip installation in that case.
//
// Linux seccomp cannot filter by path (see sandboxTypes.ts:29), so filesystem
// confinement is handled by landlock; this covers only network egress.
func buildSeccompNetworkFilter(allowNetwork bool) ([]unix.SockFilter, error) {
	if allowNetwork {
		return nil, nil
	}

	arch, ok := auditArchByGoarch[runtime.GOARCH]
	if !ok {
		return nil, fmt.Errorf("seccomp: unsupported GOARCH %q: no AUDIT_ARCH mapping", runtime.GOARCH)
	}

	kill := uint32(unix.SECCOMP_RET_KILL_PROCESS)
	deny := unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)

	// BPF program layout (instruction indices from 0):
	//
	//  0: A = seccomp_data.arch           ; load arch field (offset 4)
	//  1: if A == expectedArch → 3        ; arch OK → continue to nr check
	//  2: return KILL_PROCESS             ; wrong arch → kill immediately
	//  3: A = seccomp_data.nr             ; load syscall number (offset 0)
	//  4: if A == SYS_SOCKET → 8         ; match → deny
	//  5: if A == SYS_CONNECT → 7        ; match → deny
	//  6: if A == SYS_SENDTO → 6         ; match → deny
	//  7: if A == SYS_SENDMSG → 5        ; match → deny  (jf counts to next instr)
	//  8: return ALLOW
	//  9: return ERRNO|EPERM
	//
	// BPF_JEQ semantics: jt = jump offset on true, jf = offset on false.
	// "Offset" is relative to the NEXT instruction (i.e., 0 means fall-through).
	// The deny block is placed at the end; we compute jt from each jump to it.

	// Number of syscall-check instructions: 4 (socket, connect, sendto, sendmsg).
	// Layout after the arch prologue (instructions 3..):
	//   idx 3: load nr
	//   idx 4: je socket  → jt=?(to deny), jf=0
	//   idx 5: je connect → jt=?(to deny), jf=0
	//   idx 6: je sendto  → jt=?(to deny), jf=0
	//   idx 7: je sendmsg → jt=?(to deny), jf=0
	//   idx 8: return ALLOW
	//   idx 9: return DENY
	//
	// From idx 4: deny is at idx 9; next-after-4 is idx 5; distance = 9-5 = 4.
	// From idx 5: deny is at idx 9; next-after-5 is idx 6; distance = 9-6 = 3.
	// From idx 6: deny is at idx 9; next-after-6 is idx 7; distance = 9-7 = 2.
	// From idx 7: deny is at idx 9; next-after-7 is idx 8; distance = 9-8 = 1.

	return []unix.SockFilter{
		// --- Arch prologue (C1 fix) ---
		// 0: load seccomp_data.arch (offset 4, 32-bit word)
		bpfStmt(unix.BPF_LD|unix.BPF_W|unix.BPF_ABS, 4),
		// 1: if arch == expected → fall through (jf=0); else jump to kill (jt=1 → instr 3? no)
		//    jt=0 means fall-through (→ instr 2 which is kill) — WRONG.
		//    We want: arch matches → skip the kill → load nr.
		//    So: jt (true) = 1 to skip the kill; jf (false) = 0 to hit kill.
		bpfJump(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K, arch, 1, 0),
		// 2: arch mismatch → kill process
		bpfStmt(unix.BPF_RET|unix.BPF_K, kill),

		// --- Syscall-number checks (C2 fix) ---
		// 3: load seccomp_data.nr (offset 0, 32-bit word)
		bpfStmt(unix.BPF_LD|unix.BPF_W|unix.BPF_ABS, 0),
		// 4: if nr == SYS_SOCKET  → deny (jt=4), else continue (jf=0)
		bpfJump(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K, uint32(unix.SYS_SOCKET), 4, 0),
		// 5: if nr == SYS_CONNECT → deny (jt=3), else continue (jf=0)
		bpfJump(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K, uint32(unix.SYS_CONNECT), 3, 0),
		// 6: if nr == SYS_SENDTO  → deny (jt=2), else continue (jf=0)
		bpfJump(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K, uint32(unix.SYS_SENDTO), 2, 0),
		// 7: if nr == SYS_SENDMSG → deny (jt=1), else continue (jf=0)
		bpfJump(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K, uint32(unix.SYS_SENDMSG), 1, 0),
		// 8: return ALLOW
		bpfStmt(unix.BPF_RET|unix.BPF_K, unix.SECCOMP_RET_ALLOW),
		// 9: return ERRNO|EPERM (deny target)
		bpfStmt(unix.BPF_RET|unix.BPF_K, deny),
	}, nil
}

func bpfStmt(code uint16, k uint32) unix.SockFilter {
	return unix.SockFilter{Code: code, K: k}
}

func bpfJump(code uint16, k uint32, jt, jf uint8) unix.SockFilter {
	return unix.SockFilter{Code: code, Jt: jt, Jf: jf, K: k}
}
