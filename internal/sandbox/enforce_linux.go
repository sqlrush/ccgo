//go:build linux

package sandbox

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"unsafe"

	"github.com/landlock-lsm/go-landlock/landlock"
	"golang.org/x/sys/unix"
)

// wrap re-execs the ccgo binary as a confined child that applies landlock +
// seccomp to itself, then exec's the real command. The parent stays unconfined.
//
// Re-exec strategy (CC: Shell.ts:386-388): applying landlock to a new process
// requires setting the ruleset on the process itself — the only way to confine
// a child before it begins executing its payload is to re-exec into a known
// entrypoint that sets up confinement, then exec's the real target.
func wrap(name string, args []string, p Policy) (string, []string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", nil, fmt.Errorf("sandbox: locate self: %w", err)
	}
	encoded, err := encodePolicy(p)
	if err != nil {
		return "", nil, fmt.Errorf("sandbox: encode policy: %w", err)
	}
	// Layout: [ChildSentinel, encodedPolicy, "--", name, args...]
	childArgs := make([]string, 0, 3+1+len(args))
	childArgs = append(childArgs, ChildSentinel, encoded, "--", name)
	childArgs = append(childArgs, args...)
	return self, childArgs, nil
}

// RunChild is the entrypoint dispatched from cmd/claude/main.go when
// os.Args carries the child sentinel. It applies confinement then exec's
// the wrapped command, replacing the process image on success.
//
// args layout received from wrap: [encodedPolicy, "--", name, cmdArgs...]
func RunChild(args []string) error {
	if len(args) < 3 || args[1] != "--" {
		return fmt.Errorf("sandbox child: malformed args (want: policy -- cmd [args...])")
	}
	p, err := decodePolicy(args[0])
	if err != nil {
		return fmt.Errorf("sandbox child: decode policy: %w", err)
	}
	cmd := args[2:]
	if err := ApplyLandlockSeccomp(p); err != nil {
		return err
	}
	return unix.Exec(cmd[0], cmd, os.Environ())
}

// ApplyLandlockSeccomp confines the current process:
//   - Landlock restricts filesystem access (CC: sandboxTypes.ts:29 —
//     seccomp cannot filter by path, so FS confinement requires landlock).
//   - PR_SET_NO_NEW_PRIVS prevents privilege escalation after confinement.
//   - A seccomp BPF program blocks socket(2) when AllowNetwork is false.
//
// Kernel version requirement: landlock needs ≥5.13. BestEffort() degrades
// gracefully on older kernels (no FS confinement) unless FailIfUnavailable
// is set, in which case an error is returned.
func ApplyLandlockSeccomp(p Policy) error {
	if err := applyLandlock(p); err != nil {
		return fmt.Errorf("sandbox: landlock: %w", err)
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("sandbox: no_new_privs: %w", err)
	}
	filter, err := buildSeccompNetworkFilter(p.AllowNetwork)
	if err != nil {
		return fmt.Errorf("sandbox: seccomp filter: %w", err)
	}
	if len(filter) > 0 {
		if err := installSeccomp(filter); err != nil {
			return fmt.Errorf("sandbox: seccomp: %w", err)
		}
	}
	return nil
}

// applyLandlock sets up a landlock ruleset from Policy.
//
// Baseline: read-only access everywhere (/). Writable: cwd + AllowWrite
// paths. NOTE: DenyRead / DenyWrite paths are NOT YET implemented — landlock
// operates as a deny-by-default allowlist once activated, but per-path
// exclusion from the allow rules requires additional work and is tracked as
// future work. Currently these fields are silently ignored.
//
// Graceful degradation: BestEffort() negotiates the highest available ABI
// version. On kernels that don't support landlock at all (< 5.13), this
// is a no-op instead of an error — unless FailIfUnavailable is set, in
// which case we switch to the strict (non-BestEffort) variant.
func applyLandlock(p Policy) error {
	cwd, _ := os.Getwd()

	var rules []landlock.Rule
	// Baseline: read everything under root; this also allows /proc, /sys, /dev.
	rules = append(rules, landlock.RODirs("/"))
	// Allow writes in the current working directory.
	if cwd != "" {
		rules = append(rules, landlock.RWDirs(cwd))
	}
	// Additional explicit write grants from policy.
	for _, w := range p.AllowWrite {
		rules = append(rules, landlock.RWDirs(w))
	}
	// Explicit extra read grants (paths already covered by the root RODirs,
	// but listed here for clarity / future per-path tightening).
	for _, r := range p.AllowRead {
		rules = append(rules, landlock.RODirs(r))
	}

	cfg := landlock.V5.BestEffort()
	if p.FailIfUnavailable {
		// Strict mode: error if the kernel does not support landlock V5
		// or any earlier version.
		cfg = landlock.V5
	}
	return cfg.RestrictPaths(rules...)
}

// installSeccomp loads a classic BPF seccomp filter into the kernel.
// PR_SET_NO_NEW_PRIVS must already be set before calling this.
func installSeccomp(filter []unix.SockFilter) error {
	prog := unix.SockFprog{
		Len:    uint16(len(filter)),
		Filter: &filter[0],
	}
	_, _, errno := unix.Syscall(unix.SYS_PRCTL,
		uintptr(unix.PR_SET_SECCOMP),
		uintptr(unix.SECCOMP_MODE_FILTER),
		uintptr(unsafe.Pointer(&prog)),
	)
	if errno != 0 {
		return errno
	}
	return nil
}

func encodePolicy(p Policy) (string, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func decodePolicy(s string) (Policy, error) {
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return Policy{}, err
	}
	var p Policy
	if err := json.Unmarshal(data, &p); err != nil {
		return Policy{}, err
	}
	return p, nil
}
