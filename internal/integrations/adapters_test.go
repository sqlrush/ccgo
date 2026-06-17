package integrations

import (
	"errors"
	"strings"
	"testing"
)

func TestDetectChromeAdapters(t *testing.T) {
	adapters := DetectAdapters("chrome", AdapterOptions{
		GOOS: "linux",
		LookPath: fakeAdapterLookPath(map[string]string{
			"google-chrome": "/usr/bin/google-chrome",
		}),
	})
	if len(adapters) != 2 {
		t.Fatalf("adapters = %#v", adapters)
	}
	if adapters[0].Name != "google-chrome" || adapters[0].Kind != AdapterKindBrowser || !adapters[0].Available {
		t.Fatalf("browser adapter = %#v", adapters[0])
	}
	if adapters[1].Name != "native-host" || adapters[1].Available {
		t.Fatalf("native host adapter = %#v", adapters[1])
	}
}

func TestDetectVoiceAdapters(t *testing.T) {
	adapters := DetectAdapters("voice", AdapterOptions{
		GOOS: "linux",
		LookPath: fakeAdapterLookPath(map[string]string{
			"pw-record": "/usr/bin/pw-record",
		}),
	})
	if len(adapters) != 1 || adapters[0].Name != "pw-record" || adapters[0].Kind != AdapterKindAudioCapture || !adapters[0].Available {
		t.Fatalf("voice adapter = %#v", adapters)
	}
	if got := adapters[0].Command; len(got) != 4 || got[0] != "/usr/bin/pw-record" || got[1] != "--target" {
		t.Fatalf("voice command = %#v", got)
	}
}

func TestDetectComputerUseAdapters(t *testing.T) {
	adapters := DetectAdapters("computer-use", AdapterOptions{
		GOOS: "linux",
		Env: func(key string) string {
			if key == "DISPLAY" {
				return ":0"
			}
			return ""
		},
		LookPath: fakeAdapterLookPath(map[string]string{
			"import":  "/usr/bin/import",
			"xdotool": "/usr/bin/xdotool",
		}),
	})
	if len(adapters) != 2 {
		t.Fatalf("adapters = %#v", adapters)
	}
	if adapters[0].Name != "import" || adapters[0].Kind != AdapterKindScreenCapture || !adapters[0].Available {
		t.Fatalf("screen adapter = %#v", adapters[0])
	}
	if adapters[1].Name != "xdotool" || adapters[1].Kind != AdapterKindInputControl || !adapters[1].Available {
		t.Fatalf("input adapter = %#v", adapters[1])
	}
	if CountAvailableAdapters(adapters) != 2 {
		t.Fatalf("available adapters = %d", CountAvailableAdapters(adapters))
	}
}

func TestDetectAdaptersMissingCommands(t *testing.T) {
	adapters := DetectAdapters("voice", AdapterOptions{
		GOOS:     "linux",
		LookPath: fakeAdapterLookPath(nil),
	})
	if len(adapters) != 1 || adapters[0].Available || adapters[0].Name != "audio-capture" {
		t.Fatalf("missing adapters = %#v", adapters)
	}
}

func fakeAdapterLookPath(paths map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if path := strings.TrimSpace(paths[name]); path != "" {
			return path, nil
		}
		return "", errors.New("not found")
	}
}
