package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	macOSPreferenceDomain         = "com.anthropic.claudecode"
	windowsRegistryKeyPathHKLM    = `HKLM\SOFTWARE\Policies\ClaudeCode`
	windowsRegistryKeyPathHKCU    = `HKCU\SOFTWARE\Policies\ClaudeCode`
	windowsRegistryValueName      = "Settings"
	plutilPath                    = "/usr/bin/plutil"
	defaultPolicyReadTimeout      = 5 * time.Second
	defaultRemotePolicyLimit      = int64(1 << 20)
	remoteManagedSettingsURLEnv   = "CLAUDE_CODE_REMOTE_MANAGED_SETTINGS_URL"
	remoteManagedSettingsTokenEnv = "CLAUDE_CODE_REMOTE_MANAGED_SETTINGS_TOKEN"
)

type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type ManagedPolicyOptions struct {
	GOOS                   string
	Username               string
	ManagedDir             string
	Timeout                time.Duration
	RunCommand             CommandRunner
	RemoteURL              string
	RemoteAuthToken        string
	HTTPClient             *http.Client
	MaxRemoteResponseBytes int64
}

func LoadPolicySettings() (contracts.Settings, error) {
	return LoadPolicySettingsWithOptions(ManagedPolicyOptions{})
}

func RemoteManagedSettingsConfigured() bool {
	return strings.TrimSpace(os.Getenv(remoteManagedSettingsURLEnv)) != ""
}

func LoadPolicySettingsWithOptions(options ManagedPolicyOptions) (contracts.Settings, error) {
	options = normalizeManagedPolicyOptions(options)
	if settings, ok, err := loadAdminManagedSettings(options); err != nil || ok {
		return settings, err
	}
	if settings, ok, err := loadManagedFileSettingsFromDir(options.ManagedDir); err != nil || ok {
		return settings, err
	}
	if settings, ok, err := loadRemoteManagedSettings(options); err != nil || ok {
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
	if strings.TrimSpace(options.RemoteURL) == "" {
		options.RemoteURL = strings.TrimSpace(os.Getenv(remoteManagedSettingsURLEnv))
	}
	if strings.TrimSpace(options.RemoteAuthToken) == "" {
		options.RemoteAuthToken = strings.TrimSpace(os.Getenv(remoteManagedSettingsTokenEnv))
	}
	if options.HTTPClient == nil {
		options.HTTPClient = http.DefaultClient
	}
	if options.MaxRemoteResponseBytes <= 0 {
		options.MaxRemoteResponseBytes = defaultRemotePolicyLimit
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

func loadRemoteManagedSettings(options ManagedPolicyOptions) (contracts.Settings, bool, error) {
	rawURL := strings.TrimSpace(options.RemoteURL)
	if rawURL == "" {
		return contracts.Settings{}, false, nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return contracts.Settings{}, false, fmt.Errorf("invalid remote managed settings URL %q", rawURL)
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return contracts.Settings{}, false, fmt.Errorf("invalid remote managed settings URL scheme %q", parsed.Scheme)
	}

	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return contracts.Settings{}, false, err
	}
	req.Header.Set("accept", "application/json")
	if token := strings.TrimSpace(options.RemoteAuthToken); token != "" {
		req.Header.Set("authorization", "Bearer "+token)
	}

	resp, err := options.HTTPClient.Do(req)
	if err != nil {
		return contracts.Settings{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return contracts.Settings{}, false, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, options.MaxRemoteResponseBytes+1))
	if err != nil {
		return contracts.Settings{}, false, err
	}
	if int64(len(body)) > options.MaxRemoteResponseBytes {
		return contracts.Settings{}, false, fmt.Errorf("remote managed settings response exceeds %d bytes", options.MaxRemoteResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return contracts.Settings{}, false, fmt.Errorf("remote managed settings status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	payload, err := remoteManagedSettingsPayload(body)
	if err != nil {
		return contracts.Settings{}, false, err
	}
	settings, _, err := ParseSettingsJSON(payload, remoteManagedSettingsSourceLabel(parsed))
	if err != nil {
		return contracts.Settings{}, false, err
	}
	if !settingsHasContent(settings) {
		return contracts.Settings{}, false, nil
	}
	return settings, true, nil
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

func remoteManagedSettingsPayload(data []byte) ([]byte, error) {
	if strings.TrimSpace(string(data)) == "" {
		return data, nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	for _, key := range []string{"settings", "policy", "managedSettings", "managed_settings"} {
		if nested, ok := raw[key].(map[string]any); ok {
			return json.Marshal(nested)
		}
	}
	if dataValue, ok := raw["data"].(map[string]any); ok {
		for _, key := range []string{"settings", "policy", "managedSettings", "managed_settings"} {
			if nested, ok := dataValue[key].(map[string]any); ok {
				return json.Marshal(nested)
			}
		}
	}
	return data, nil
}

func remoteManagedSettingsSourceLabel(parsed *url.URL) string {
	if parsed == nil {
		return "Remote managed settings"
	}
	cleaned := *parsed
	cleaned.User = nil
	cleaned.RawQuery = ""
	cleaned.Fragment = ""
	return "Remote managed settings: " + cleaned.String()
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
