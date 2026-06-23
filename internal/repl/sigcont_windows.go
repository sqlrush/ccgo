//go:build windows

package repl

import "context"

// startSIGCONTListener is a no-op on Windows: SIGCONT does not exist on
// Windows. The REPL does not need a SIGCONT handler on that platform.
func startSIGCONTListener(_ context.Context, _ Terminal, _ chan<- resizeEvent) {}
