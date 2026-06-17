package native

import (
	"errors"
	"strings"
	"testing"
)

func TestDetectClipboardAdaptersDarwinSystemAndTmux(t *testing.T) {
	adapters := DetectClipboardAdapters(ClipboardAdapterOptions{
		GOOS: "darwin",
		Env: func(key string) string {
			if key == "TMUX" {
				return "/tmp/tmux.sock"
			}
			return ""
		},
		LookPath: fakeClipboardLookPath(map[string]string{
			"pbcopy":  "/usr/bin/pbcopy",
			"pbpaste": "/usr/bin/pbpaste",
			"tmux":    "/usr/bin/tmux",
		}),
	})
	if len(adapters) != 3 {
		t.Fatalf("adapters = %#v", adapters)
	}
	if adapters[0].Name != "pbcopy" || adapters[0].Kind != ClipboardAdapterKindSystem || !adapters[0].Available {
		t.Fatalf("system adapter = %#v", adapters[0])
	}
	if got := adapters[0].WriteCommand; len(got) != 1 || got[0] != "/usr/bin/pbcopy" {
		t.Fatalf("system write command = %#v", got)
	}
	if got := adapters[0].ReadCommand; len(got) != 1 || got[0] != "/usr/bin/pbpaste" {
		t.Fatalf("system read command = %#v", got)
	}
	if adapters[1].Name != "tmux" || adapters[1].Kind != ClipboardAdapterKindMultiplexer {
		t.Fatalf("tmux adapter = %#v", adapters[1])
	}
	if adapters[2].Name != "osc52" || adapters[2].Kind != ClipboardAdapterKindTerminal {
		t.Fatalf("terminal adapter = %#v", adapters[2])
	}
	if CountAvailableClipboardAdapters(adapters) != 3 {
		t.Fatalf("available adapters = %d", CountAvailableClipboardAdapters(adapters))
	}
}

func TestDetectClipboardAdaptersLinuxWaylandAndX11(t *testing.T) {
	wayland := DetectClipboardAdapters(ClipboardAdapterOptions{
		GOOS: "linux",
		Env: func(key string) string {
			if key == "WAYLAND_DISPLAY" {
				return "wayland-0"
			}
			return ""
		},
		LookPath: fakeClipboardLookPath(map[string]string{
			"wl-copy":  "/usr/bin/wl-copy",
			"wl-paste": "/usr/bin/wl-paste",
		}),
	})
	if wayland[0].Name != "wl-copy" || wayland[0].Kind != ClipboardAdapterKindSystem {
		t.Fatalf("wayland adapter = %#v", wayland)
	}
	if got := wayland[0].ReadCommand; len(got) != 2 || got[0] != "/usr/bin/wl-paste" || got[1] != "--no-newline" {
		t.Fatalf("wayland read command = %#v", got)
	}

	x11 := DetectClipboardAdapters(ClipboardAdapterOptions{
		GOOS: "linux",
		Env: func(key string) string {
			if key == "DISPLAY" {
				return ":0"
			}
			return ""
		},
		LookPath: fakeClipboardLookPath(map[string]string{
			"xclip": "/usr/bin/xclip",
		}),
	})
	if x11[0].Name != "xclip" || x11[0].Kind != ClipboardAdapterKindSystem {
		t.Fatalf("x11 adapter = %#v", x11)
	}
	if got := x11[0].WriteCommand; len(got) != 3 || got[1] != "-selection" || got[2] != "clipboard" {
		t.Fatalf("x11 write command = %#v", got)
	}
}

func TestDetectClipboardAdaptersAlwaysIncludesOSC52(t *testing.T) {
	adapters := DetectClipboardAdapters(ClipboardAdapterOptions{
		GOOS:     "plan9",
		Env:      func(string) string { return "" },
		LookPath: fakeClipboardLookPath(nil),
	})
	if len(adapters) != 1 || adapters[0].Name != "osc52" || !adapters[0].Available {
		t.Fatalf("fallback adapters = %#v", adapters)
	}
	if HasClipboardAdapterKind(adapters, ClipboardAdapterKindSystem) {
		t.Fatalf("unexpected system adapter = %#v", adapters)
	}
}

func fakeClipboardLookPath(paths map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if path := strings.TrimSpace(paths[name]); path != "" {
			return path, nil
		}
		return "", errors.New("not found")
	}
}
