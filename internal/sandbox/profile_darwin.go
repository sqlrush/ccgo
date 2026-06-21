//go:build darwin

package sandbox

import "strings"

// buildSeatbeltProfile renders a deny-by-default seatbelt profile that allows
// process/exec basics, read of the whole FS by default unless denied, and write
// only under AllowWrite. Mirrors the deny-default posture of CC's runtime.
func buildSeatbeltProfile(p Policy, cwd string) string {
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
	if p.AllowNetwork {
		b.WriteString("(allow network*)\n")
	} else {
		b.WriteString("(deny network*)\n")
		b.WriteString("(allow network* (local ip \"localhost:*\"))\n")
	}
	return b.String()
}

func escapeSB(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}
