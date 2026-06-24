//go:build darwin

package sandbox

import (
	"fmt"
	"strings"
)

// buildSeatbeltProfile renders a deny-by-default seatbelt profile that allows
// process/exec basics, read of the whole FS by default unless denied, and write
// only under AllowWrite. Mirrors the deny-default posture of CC's runtime.
//
// SBX-48: when per-domain rules are present AND a proxy port is provided, the
// profile restricts direct outbound network to only the proxy's loopback port.
// This forces HTTP/HTTPS traffic through the domain-filtering proxy; the proxy
// itself enforces AllowedDomains/DeniedDomains. proxyPort=0 means no proxy is
// active and the normal AllowNetwork rule applies.
func buildSeatbeltProfile(p Policy, cwd string) string {
	return buildSeatbeltProfileWithProxy(p, cwd, 0)
}

// buildSeatbeltProfileWithProxy is the full implementation of profile generation.
// proxyPort is the port of the domain-filtering proxy on 127.0.0.1; use 0 when
// no proxy is active.
func buildSeatbeltProfileWithProxy(p Policy, cwd string, proxyPort int) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")
	b.WriteString("(allow process-exec)\n")
	b.WriteString("(allow process-fork)\n")
	b.WriteString("(allow signal (target self))\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow file-read*)\n") // read-all baseline; tighten via deny below
	for _, path := range p.DenyRead {
		b.WriteString(`(deny file-read* (subpath "` + escapeSB(path) + `"))` + "\n")
	}
	if cwd != "" {
		b.WriteString(`(allow file-write* (subpath "` + escapeSB(cwd) + `"))` + "\n")
	}
	for _, path := range p.AllowWrite {
		b.WriteString(`(allow file-write* (subpath "` + escapeSB(path) + `"))` + "\n")
	}
	for _, path := range p.DenyWrite {
		b.WriteString(`(deny file-write* (subpath "` + escapeSB(path) + `"))` + "\n")
	}
	b.WriteString(`(allow file-write* (subpath "/dev"))` + "\n")
	b.WriteString(`(allow file-write* (subpath "/private/tmp"))` + "\n")

	// Network rules — three cases:
	// 1. AllowNetwork=true and no domain proxy → unrestricted network.
	// 2. Per-domain proxy active (proxyPort > 0) → allow only loopback to proxy
	//    port; process MUST go through the filtering proxy for all HTTP/HTTPS.
	// 3. AllowNetwork=false and no proxy → deny all network, localhost exception.
	if p.AllowNetwork && proxyPort == 0 {
		b.WriteString("(allow network*)\n")
	} else if proxyPort > 0 {
		// SBX-48: Restrict direct network to the domain-filtering proxy port only.
		// The sandboxed process sends HTTP/HTTPS through the proxy (via HTTP_PROXY /
		// HTTPS_PROXY / ALL_PROXY env vars); the proxy enforces AllowedDomains /
		// DeniedDomains.  Direct connections to other loopback ports or external
		// hosts are denied, forcing all traffic through the filter.
		b.WriteString("(deny network*)\n")
		b.WriteString(fmt.Sprintf(
			"(allow network-outbound (remote ip \"localhost:%d\"))\n", proxyPort,
		))
		b.WriteString(fmt.Sprintf(
			"(allow network-outbound (remote ip \"127.0.0.1:%d\"))\n", proxyPort,
		))
		// SBX-48 comment: domain policy is enforced by the proxy; kernel seatbelt
		// cannot express DNS-level rules so we restrict to proxy port only.
		if len(p.AllowedDomains) > 0 {
			b.WriteString("; allowed-domains (enforced by proxy): " + strings.Join(p.AllowedDomains, ",") + "\n")
		}
		if len(p.DeniedDomains) > 0 {
			b.WriteString("; denied-domains (enforced by proxy): " + strings.Join(p.DeniedDomains, ",") + "\n")
		}
	} else {
		b.WriteString("(deny network*)\n")
		b.WriteString("(allow network* (local ip \"localhost:*\"))\n")
		// SBX-48: when no proxy is active but domains are configured, emit them as
		// comments so operators can inspect intent without consulting settings.
		if len(p.AllowedDomains) > 0 {
			b.WriteString("; allowed-domains: " + strings.Join(p.AllowedDomains, ",") + "\n")
		}
		if len(p.DeniedDomains) > 0 {
			b.WriteString("; denied-domains: " + strings.Join(p.DeniedDomains, ",") + "\n")
		}
	}

	// SBX-49: AllowUnixSockets — emit per-path outbound rules.
	// macOS seatbelt supports (allow network-outbound (path "/path/to.sock")).
	for _, sock := range p.AllowUnixSockets {
		b.WriteString(`(allow network-outbound (path "` + escapeSB(sock) + `"))` + "\n")
	}
	return b.String()
}

func escapeSB(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}
