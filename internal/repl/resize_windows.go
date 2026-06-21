//go:build windows

package repl

import "context"

// startResizeListener is a no-op on Windows (resizing handled differently).
func startResizeListener(_ context.Context, _ Terminal, _ chan<- resizeEvent) {}
