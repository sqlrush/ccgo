package integrations

import (
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
)

const (
	AdapterKindBrowser       = "browser"
	AdapterKindNativeHost    = "native_host"
	AdapterKindAudioCapture  = "audio_capture"
	AdapterKindScreenCapture = "screen_capture"
	AdapterKindInputControl  = "input_control"
)

type Adapter struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Available bool     `json:"available"`
	Command   []string `json:"command,omitempty"`
	Detail    string   `json:"detail,omitempty"`
}

type AdapterOptions struct {
	GOOS     string
	Env      func(string) string
	LookPath func(string) (string, error)
}

func DetectAdapters(name string, options AdapterOptions) []Adapter {
	name = normalizeRuntimeName(name)
	if name == "" {
		return nil
	}
	goos := strings.TrimSpace(options.GOOS)
	if goos == "" {
		goos = runtime.GOOS
	}
	env := options.Env
	if env == nil {
		env = os.Getenv
	}
	lookPath := options.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	var adapters []Adapter
	switch name {
	case "chrome":
		adapters = detectChromeAdapters(goos, lookPath)
	case "voice":
		adapters = detectVoiceAdapters(goos, lookPath)
	case "computer_use":
		adapters = detectComputerUseAdapters(goos, env, lookPath)
	}
	sort.SliceStable(adapters, func(i, j int) bool {
		if adapterPriority(adapters[i]) == adapterPriority(adapters[j]) {
			return adapters[i].Name < adapters[j].Name
		}
		return adapterPriority(adapters[i]) < adapterPriority(adapters[j])
	})
	return adapters
}

func CountAvailableAdapters(adapters []Adapter) int {
	count := 0
	for _, adapter := range adapters {
		if adapter.Available {
			count++
		}
	}
	return count
}

func detectChromeAdapters(goos string, lookPath func(string) (string, error)) []Adapter {
	adapters := []Adapter{{
		Name:      "native-host",
		Kind:      AdapterKindNativeHost,
		Available: false,
		Detail:    "Chrome native messaging host install/runtime is not configured",
	}}
	for _, name := range chromeCommandCandidates(goos) {
		if path, ok := adapterLookPath(lookPath, name); ok {
			adapters = append(adapters, Adapter{
				Name:      name,
				Kind:      AdapterKindBrowser,
				Available: true,
				Command:   []string{path},
				Detail:    "Chrome/Chromium browser command adapter",
			})
			return adapters
		}
	}
	adapters = append(adapters, Adapter{
		Name:      "browser",
		Kind:      AdapterKindBrowser,
		Available: false,
		Detail:    "Chrome/Chromium browser command not found in PATH",
	})
	return adapters
}

func detectVoiceAdapters(goos string, lookPath func(string) (string, error)) []Adapter {
	for _, candidate := range voiceCaptureCandidates(goos) {
		if path, ok := adapterLookPath(lookPath, candidate.name); ok {
			return []Adapter{{
				Name:      candidate.name,
				Kind:      AdapterKindAudioCapture,
				Available: true,
				Command:   append([]string{path}, candidate.args...),
				Detail:    candidate.detail,
			}}
		}
	}
	return []Adapter{{
		Name:      "audio-capture",
		Kind:      AdapterKindAudioCapture,
		Available: false,
		Detail:    "audio capture command not found in PATH",
	}}
}

func detectComputerUseAdapters(goos string, env func(string) string, lookPath func(string) (string, error)) []Adapter {
	var adapters []Adapter
	for _, candidate := range screenCaptureCandidates(goos, env) {
		if path, ok := adapterLookPath(lookPath, candidate.name); ok {
			adapters = append(adapters, Adapter{
				Name:      candidate.name,
				Kind:      AdapterKindScreenCapture,
				Available: true,
				Command:   append([]string{path}, candidate.args...),
				Detail:    candidate.detail,
			})
			break
		}
	}
	if len(adapters) == 0 {
		adapters = append(adapters, Adapter{
			Name:      "screen-capture",
			Kind:      AdapterKindScreenCapture,
			Available: false,
			Detail:    "screen capture command not found in PATH",
		})
	}
	for _, candidate := range inputControlCandidates(goos, env) {
		if path, ok := adapterLookPath(lookPath, candidate.name); ok {
			adapters = append(adapters, Adapter{
				Name:      candidate.name,
				Kind:      AdapterKindInputControl,
				Available: true,
				Command:   append([]string{path}, candidate.args...),
				Detail:    candidate.detail,
			})
			return adapters
		}
	}
	adapters = append(adapters, Adapter{
		Name:      "input-control",
		Kind:      AdapterKindInputControl,
		Available: false,
		Detail:    "input control command not found in PATH",
	})
	return adapters
}

type commandCandidate struct {
	name   string
	args   []string
	detail string
}

func chromeCommandCandidates(goos string) []string {
	switch goos {
	case "darwin":
		return []string{"google-chrome", "chromium", "chrome"}
	case "windows":
		return []string{"chrome.exe", "msedge.exe"}
	default:
		return []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome", "microsoft-edge"}
	}
}

func voiceCaptureCandidates(goos string) []commandCandidate {
	switch goos {
	case "darwin":
		return []commandCandidate{
			{name: "rec", args: []string{"-q", "-t", "wav", "-"}, detail: "SoX microphone capture adapter"},
			{name: "sox", args: []string{"-d", "-t", "wav", "-"}, detail: "SoX default input capture adapter"},
			{name: "ffmpeg", args: []string{"-f", "avfoundation", "-i", ":0", "-f", "wav", "-"}, detail: "FFmpeg avfoundation capture adapter"},
		}
	case "windows":
		return []commandCandidate{
			{name: "ffmpeg.exe", args: []string{"-f", "dshow", "-i", "audio=default", "-f", "wav", "-"}, detail: "FFmpeg DirectShow capture adapter"},
		}
	default:
		return []commandCandidate{
			{name: "pw-record", args: []string{"--target", "default", "-"}, detail: "PipeWire audio capture adapter"},
			{name: "parecord", args: []string{"--file-format=wav", "-"}, detail: "PulseAudio audio capture adapter"},
			{name: "arecord", args: []string{"-f", "cd", "-t", "wav", "-"}, detail: "ALSA audio capture adapter"},
			{name: "ffmpeg", args: []string{"-f", "pulse", "-i", "default", "-f", "wav", "-"}, detail: "FFmpeg PulseAudio capture adapter"},
		}
	}
}

func screenCaptureCandidates(goos string, env func(string) string) []commandCandidate {
	switch goos {
	case "darwin":
		return []commandCandidate{{name: "screencapture", args: []string{"-x", "-"}, detail: "macOS screen capture adapter"}}
	case "windows":
		return []commandCandidate{{name: "powershell.exe", args: []string{"-NoProfile", "-Command", "Add-Type -AssemblyName System.Windows.Forms,System.Drawing; [Windows.Forms.Screen]::PrimaryScreen.Bounds"}, detail: "Windows screen metadata adapter"}}
	default:
		if strings.TrimSpace(env("WAYLAND_DISPLAY")) != "" {
			return []commandCandidate{
				{name: "grim", args: []string{"-"}, detail: "Wayland screen capture adapter"},
				{name: "gnome-screenshot", args: []string{"-f", "-"}, detail: "GNOME screenshot adapter"},
			}
		}
		if strings.TrimSpace(env("DISPLAY")) == "" {
			return nil
		}
		return []commandCandidate{
			{name: "import", args: []string{"-window", "root", "png:-"}, detail: "ImageMagick X11 screen capture adapter"},
			{name: "gnome-screenshot", args: []string{"-f", "-"}, detail: "GNOME screenshot adapter"},
		}
	}
}

func inputControlCandidates(goos string, env func(string) string) []commandCandidate {
	switch goos {
	case "darwin":
		return []commandCandidate{{name: "osascript", detail: "macOS accessibility scripting adapter"}}
	case "windows":
		return []commandCandidate{{name: "powershell.exe", args: []string{"-NoProfile", "-Command"}, detail: "Windows PowerShell input control adapter"}}
	default:
		if strings.TrimSpace(env("WAYLAND_DISPLAY")) != "" {
			return []commandCandidate{{name: "ydotool", detail: "Wayland input control adapter"}}
		}
		if strings.TrimSpace(env("DISPLAY")) == "" {
			return nil
		}
		return []commandCandidate{{name: "xdotool", detail: "X11 input control adapter"}}
	}
}

func adapterLookPath(lookPath func(string) (string, error), name string) (string, bool) {
	path, err := lookPath(name)
	if err != nil || strings.TrimSpace(path) == "" {
		return "", false
	}
	return path, true
}

func adapterPriority(adapter Adapter) int {
	switch adapter.Kind {
	case AdapterKindBrowser:
		return 0
	case AdapterKindNativeHost:
		return 1
	case AdapterKindAudioCapture:
		return 2
	case AdapterKindScreenCapture:
		return 3
	case AdapterKindInputControl:
		return 4
	default:
		return 5
	}
}
