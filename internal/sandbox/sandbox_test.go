package sandbox

import (
	"runtime"
	"testing"
)

func TestSupportedMatchesPlatform(t *testing.T) {
	got := Supported()
	want := runtime.GOOS == "darwin" || runtime.GOOS == "linux"
	if got != want {
		t.Fatalf("Supported() = %v want %v on %s", got, want, runtime.GOOS)
	}
}

func TestWrapUnsupportedPlatformErrors(t *testing.T) {
	if Supported() {
		t.Skip("platform supports sandbox; guard path tested only on unsupported OS")
	}
	// On an unsupported OS, Wrap with an enabled policy must error clearly
	// rather than silently running unconfined.
	_, _, err := Wrap("/bin/sh", []string{"-c", "echo hi"}, Policy{Enabled: true})
	if err == nil {
		t.Fatal("expected ErrUnsupported on unsupported platform")
	}
}
