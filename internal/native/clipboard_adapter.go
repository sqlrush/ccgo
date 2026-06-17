package native

import (
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
)

const (
	ClipboardAdapterKindSystem      = "system"
	ClipboardAdapterKindMultiplexer = "multiplexer"
	ClipboardAdapterKindTerminal    = "terminal"
)

type ClipboardAdapter struct {
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Available    bool     `json:"available"`
	WriteCommand []string `json:"write_command,omitempty"`
	ReadCommand  []string `json:"read_command,omitempty"`
	Detail       string   `json:"detail,omitempty"`
}

type ClipboardAdapterOptions struct {
	GOOS     string
	Env      func(string) string
	LookPath func(string) (string, error)
}

func DetectClipboardAdapters(options ClipboardAdapterOptions) []ClipboardAdapter {
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

	var adapters []ClipboardAdapter
	adapters = append(adapters, detectSystemClipboardAdapters(goos, env, lookPath)...)
	if strings.TrimSpace(env("TMUX")) != "" {
		if path, ok := clipboardLookPath(lookPath, "tmux"); ok {
			adapters = append(adapters, ClipboardAdapter{
				Name:         "tmux",
				Kind:         ClipboardAdapterKindMultiplexer,
				Available:    true,
				WriteCommand: []string{path, "load-buffer", "-"},
				ReadCommand:  []string{path, "save-buffer", "-"},
				Detail:       "tmux buffer adapter",
			})
		}
	}
	adapters = append(adapters, ClipboardAdapter{
		Name:      "osc52",
		Kind:      ClipboardAdapterKindTerminal,
		Available: true,
		Detail:    "OSC 52 terminal clipboard sequence",
	})
	sort.SliceStable(adapters, func(i, j int) bool {
		if adapterPriority(adapters[i]) == adapterPriority(adapters[j]) {
			return adapters[i].Name < adapters[j].Name
		}
		return adapterPriority(adapters[i]) < adapterPriority(adapters[j])
	})
	return adapters
}

func detectSystemClipboardAdapters(goos string, env func(string) string, lookPath func(string) (string, error)) []ClipboardAdapter {
	switch goos {
	case "darwin":
		write, writeOK := clipboardLookPath(lookPath, "pbcopy")
		read, readOK := clipboardLookPath(lookPath, "pbpaste")
		if writeOK {
			adapter := ClipboardAdapter{
				Name:         "pbcopy",
				Kind:         ClipboardAdapterKindSystem,
				Available:    true,
				WriteCommand: []string{write},
				Detail:       "macOS system clipboard adapter",
			}
			if readOK {
				adapter.ReadCommand = []string{read}
			}
			return []ClipboardAdapter{adapter}
		}
	case "linux":
		if strings.TrimSpace(env("WAYLAND_DISPLAY")) != "" {
			write, writeOK := clipboardLookPath(lookPath, "wl-copy")
			read, readOK := clipboardLookPath(lookPath, "wl-paste")
			if writeOK {
				adapter := ClipboardAdapter{
					Name:         "wl-copy",
					Kind:         ClipboardAdapterKindSystem,
					Available:    true,
					WriteCommand: []string{write},
					Detail:       "Wayland system clipboard adapter",
				}
				if readOK {
					adapter.ReadCommand = []string{read, "--no-newline"}
				}
				return []ClipboardAdapter{adapter}
			}
		}
		if strings.TrimSpace(env("DISPLAY")) != "" {
			if write, ok := clipboardLookPath(lookPath, "xclip"); ok {
				return []ClipboardAdapter{{
					Name:         "xclip",
					Kind:         ClipboardAdapterKindSystem,
					Available:    true,
					WriteCommand: []string{write, "-selection", "clipboard"},
					ReadCommand:  []string{write, "-selection", "clipboard", "-o"},
					Detail:       "X11 xclip clipboard adapter",
				}}
			}
			if write, ok := clipboardLookPath(lookPath, "xsel"); ok {
				return []ClipboardAdapter{{
					Name:         "xsel",
					Kind:         ClipboardAdapterKindSystem,
					Available:    true,
					WriteCommand: []string{write, "--clipboard", "--input"},
					ReadCommand:  []string{write, "--clipboard", "--output"},
					Detail:       "X11 xsel clipboard adapter",
				}}
			}
		}
	case "windows":
		if shell, ok := clipboardLookPath(lookPath, "powershell.exe"); ok {
			return []ClipboardAdapter{{
				Name:         "powershell",
				Kind:         ClipboardAdapterKindSystem,
				Available:    true,
				WriteCommand: []string{shell, "-NoProfile", "-Command", "Set-Clipboard"},
				ReadCommand:  []string{shell, "-NoProfile", "-Command", "Get-Clipboard -Raw"},
				Detail:       "Windows PowerShell clipboard adapter",
			}}
		}
		if shell, ok := clipboardLookPath(lookPath, "pwsh"); ok {
			return []ClipboardAdapter{{
				Name:         "pwsh",
				Kind:         ClipboardAdapterKindSystem,
				Available:    true,
				WriteCommand: []string{shell, "-NoProfile", "-Command", "Set-Clipboard"},
				ReadCommand:  []string{shell, "-NoProfile", "-Command", "Get-Clipboard -Raw"},
				Detail:       "PowerShell clipboard adapter",
			}}
		}
		if clip, ok := clipboardLookPath(lookPath, "clip.exe"); ok {
			return []ClipboardAdapter{{
				Name:         "clip.exe",
				Kind:         ClipboardAdapterKindSystem,
				Available:    true,
				WriteCommand: []string{clip},
				Detail:       "Windows clip.exe write-only clipboard adapter",
			}}
		}
	}
	return nil
}

func CountAvailableClipboardAdapters(adapters []ClipboardAdapter) int {
	count := 0
	for _, adapter := range adapters {
		if adapter.Available {
			count++
		}
	}
	return count
}

func ClipboardAdapterNames(adapters []ClipboardAdapter) []string {
	var names []string
	for _, adapter := range adapters {
		if !adapter.Available || strings.TrimSpace(adapter.Name) == "" {
			continue
		}
		names = append(names, adapter.Name)
	}
	sort.Strings(names)
	return names
}

func HasClipboardAdapterKind(adapters []ClipboardAdapter, kind string) bool {
	for _, adapter := range adapters {
		if adapter.Available && adapter.Kind == kind {
			return true
		}
	}
	return false
}

func clipboardAdapterNamesByKind(adapters []ClipboardAdapter, kind string) []string {
	var names []string
	for _, adapter := range adapters {
		if adapter.Available && adapter.Kind == kind && strings.TrimSpace(adapter.Name) != "" {
			names = append(names, adapter.Name)
		}
	}
	sort.Strings(names)
	return names
}

func clipboardCapabilityDetail(adapters []ClipboardAdapter, kind string, fallback string) string {
	names := clipboardAdapterNamesByKind(adapters, kind)
	if len(names) == 0 {
		return fallback
	}
	return strings.Join(names, ", ")
}

func clipboardLookPath(lookPath func(string) (string, error), name string) (string, bool) {
	path, err := lookPath(name)
	if err != nil || strings.TrimSpace(path) == "" {
		return "", false
	}
	return path, true
}

func adapterPriority(adapter ClipboardAdapter) int {
	switch adapter.Kind {
	case ClipboardAdapterKindSystem:
		return 0
	case ClipboardAdapterKindMultiplexer:
		return 1
	case ClipboardAdapterKindTerminal:
		return 2
	default:
		return 3
	}
}
