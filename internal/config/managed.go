package config

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"ccgo/internal/contracts"
)

const (
	macOSPreferenceDomain      = "com.anthropic.claudecode"
	windowsRegistryKeyPathHKLM = `HKLM\SOFTWARE\Policies\ClaudeCode`
	windowsRegistryKeyPathHKCU = `HKCU\SOFTWARE\Policies\ClaudeCode`
	windowsRegistryValueName   = "Settings"
	plutilPath                 = "/usr/bin/plutil"
	defaultPolicyReadTimeout   = 5 * time.Second
)

type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type ManagedPolicyOptions struct {
	GOOS       string
	Username   string
	ManagedDir string
	Timeout    time.Duration
	RunCommand CommandRunner
}

func LoadPolicySettings() (contracts.Settings, error) {
	return LoadPolicySettingsWithOptions(ManagedPolicyOptions{})
}

func LoadPolicySettingsWithOptions(options ManagedPolicyOptions) (contracts.Settings, error) {
	options = normalizeManagedPolicyOptions(options)
	if settings, ok, err := loadAdminManagedSettings(options); err != nil || ok {
		return settings, err
	}
	if settings, ok, err := loadManagedFileSettingsFromDir(options.ManagedDir); err != nil || ok {
		return settings, err
	}
	if settings, ok, err := loadUserManagedSettings(options); err != nil || ok {
		return settings, err
	}
	return contracts.Settings{}, nil
}

func normalizeManagedPolicyOptions(options ManagedPolicyOptions) ManagedPolicyOptions {
	if options.GOOS == "" {
		options.GOOS = runtime.GOOS
	}
	if options.ManagedDir == "" {
		options.ManagedDir = ManagedSettingsDir()
	}
	if options.Timeout <= 0 {
		options.Timeout = defaultPolicyReadTimeout
	}
	if options.RunCommand == nil {
		options.RunCommand = defaultCommandRunner
	}
	return options
}

func defaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func loadAdminManagedSettings(options ManagedPolicyOptions) (contracts.Settings, bool, error) {
	switch options.GOOS {
	case "darwin":
		for _, candidate := range macOSManagedPreferencePaths(options.Username) {
			stdout, err := runManagedPolicyCommand(options, plutilPath, "-convert", "json", "-o", "-", "--", candidate.path)
			if err != nil {
				continue
			}
			settings, ok, err := parseManagedCommandJSON(stdout, candidate.label)
			if err != nil || ok {
				return settings, ok, err
			}
		}
	case "windows":
		return loadWindowsRegistrySettings(options, windowsRegistryKeyPathHKLM)
	}
	return contracts.Settings{}, false, nil
}

func loadUserManagedSettings(options ManagedPolicyOptions) (contracts.Settings, bool, error) {
	if options.GOOS != "windows" {
		return contracts.Settings{}, false, nil
	}
	return loadWindowsRegistrySettings(options, windowsRegistryKeyPathHKCU)
}

func loadWindowsRegistrySettings(options ManagedPolicyOptions, key string) (contracts.Settings, bool, error) {
	stdout, err := runManagedPolicyCommand(options, "reg", "query", key, "/v", windowsRegistryValueName)
	if err != nil {
		return contracts.Settings{}, false, nil
	}
	value := parseRegQuerySettingsValue(string(stdout), windowsRegistryValueName)
	if value == "" {
		return contracts.Settings{}, false, nil
	}
	return parseManagedCommandJSON([]byte(value), "Registry: "+key+`\`+windowsRegistryValueName)
}

func runManagedPolicyCommand(options ManagedPolicyOptions, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()
	return options.RunCommand(ctx, name, args...)
}

type macOSManagedPreferencePath struct {
	path  string
	label string
}

func macOSManagedPreferencePaths(username string) []macOSManagedPreferencePath {
	if username == "" {
		if current, err := user.Current(); err == nil {
			username = current.Username
			if slash := strings.LastIndex(username, `\`); slash >= 0 {
				username = username[slash+1:]
			}
		}
	}
	var paths []macOSManagedPreferencePath
	if username != "" {
		paths = append(paths, macOSManagedPreferencePath{
			path:  filepath.Join(string(filepath.Separator), "Library", "Managed Preferences", username, macOSPreferenceDomain+".plist"),
			label: "per-user managed preferences",
		})
	}
	paths = append(paths, macOSManagedPreferencePath{
		path:  filepath.Join(string(filepath.Separator), "Library", "Managed Preferences", macOSPreferenceDomain+".plist"),
		label: "device-level managed preferences",
	})
	return paths
}

func loadManagedFileSettingsFromDir(dir string) (contracts.Settings, bool, error) {
	var settings []contracts.Settings
	if base, ok, err := loadManagedSettingsJSONFile(filepath.Join(dir, "managed-settings.json")); err != nil {
		return contracts.Settings{}, false, err
	} else if ok {
		settings = append(settings, base)
	}
	dropIns, err := managedDropInFiles(filepath.Join(dir, "managed-settings.d"))
	if err != nil {
		return contracts.Settings{}, false, err
	}
	for _, path := range dropIns {
		item, ok, err := loadManagedSettingsJSONFile(path)
		if err != nil {
			return contracts.Settings{}, false, err
		}
		if ok {
			settings = append(settings, item)
		}
	}
	if len(settings) == 0 {
		return contracts.Settings{}, false, nil
	}
	return MergeSettings(settings...), true, nil
}

func managedDropInFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		mode := entry.Type()
		if mode.IsRegular() || mode&os.ModeSymlink != 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(dir, name))
	}
	return paths, nil
}

func loadManagedSettingsJSONFile(path string) (contracts.Settings, bool, error) {
	data, err := readSettingsFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return contracts.Settings{}, false, nil
		}
		return contracts.Settings{}, false, err
	}
	settings, _, err := ParseSettingsJSON(data, path)
	if err != nil {
		return contracts.Settings{}, false, err
	}
	if !settingsHasContent(settings) {
		return contracts.Settings{}, false, nil
	}
	return settings, true, nil
}

func parseManagedCommandJSON(data []byte, source string) (contracts.Settings, bool, error) {
	if strings.TrimSpace(string(data)) == "" {
		return contracts.Settings{}, false, nil
	}
	settings, _, err := ParseSettingsJSON(data, source)
	if err != nil {
		return contracts.Settings{}, false, nil
	}
	if !settingsHasContent(settings) {
		return contracts.Settings{}, false, nil
	}
	return settings, true, nil
}

func parseRegQuerySettingsValue(stdout string, valueName string) string {
	escaped := regexp.QuoteMeta(valueName)
	re := regexp.MustCompile(`(?i)^\s+` + escaped + `\s+REG_(?:EXPAND_)?SZ\s+(.*)$`)
	for _, line := range strings.Split(strings.ReplaceAll(stdout, "\r\n", "\n"), "\n") {
		match := re.FindStringSubmatch(line)
		if len(match) == 2 {
			return strings.TrimRight(match[1], " \t")
		}
	}
	return ""
}

func settingsHasContent(settings contracts.Settings) bool {
	return !reflect.DeepEqual(settings, contracts.Settings{})
}
