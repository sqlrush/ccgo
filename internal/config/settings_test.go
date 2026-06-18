package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ccgo/internal/contracts"
)

func TestLoadSettingsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":"opus","env":{"DEBUG":1,"TRACE":true},"permissions":{"allow":["Bash(ls *)"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	settings, err := LoadSettingsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Model != "opus" {
		t.Fatalf("model = %q", settings.Model)
	}
	if got := settings.Permissions.Allow[0]; got != "Bash(ls *)" {
		t.Fatalf("allow[0] = %q", got)
	}
	if settings.Env["DEBUG"] != "1" || settings.Env["TRACE"] != "true" {
		t.Fatalf("env = %#v", settings.Env)
	}
}

func TestLoadSettingsFileWithWarningsFiltersInvalidPermissionRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{
		"permissions": {
			"allow": ["Bash(ls *)", "Bash()", 42],
			"deny": ["WebFetch(https://example.com)"],
			"defaultMode": "bad-mode",
			"disableBypassPermissionsMode": true
		},
		"sandbox": {
			"allowUnsandboxedCommands": "no"
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	settings, warnings, err := LoadSettingsFileWithWarnings(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.Permissions.Allow) != 1 || settings.Permissions.Allow[0] != "Bash(ls *)" {
		t.Fatalf("allow = %#v", settings.Permissions.Allow)
	}
	if len(settings.Permissions.Deny) != 0 {
		t.Fatalf("deny = %#v", settings.Permissions.Deny)
	}
	if len(warnings) != 6 {
		t.Fatalf("warnings = %#v", warnings)
	}
	paths := map[string]int{}
	for _, warning := range warnings {
		paths[warning.Path]++
	}
	if paths["permissions.allow"] != 2 || paths["permissions.deny"] != 1 || paths["permissions.defaultMode"] != 1 || paths["permissions.disableBypassPermissionsMode"] != 1 || paths["sandbox.allowUnsandboxedCommands"] != 1 {
		t.Fatalf("warning paths = %#v warnings=%#v", paths, warnings)
	}
}

func TestSettingsFileCacheInvalidatesWhenFileChanges(t *testing.T) {
	ResetSettingsFileCache()
	t.Cleanup(ResetSettingsFileCache)

	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":"opus"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	settings, err := LoadSettingsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Model != "opus" {
		t.Fatalf("model = %q", settings.Model)
	}
	if got := settingsFileCacheLen(); got != 1 {
		t.Fatalf("cache entries = %d", got)
	}

	later := time.Now().Add(2 * time.Second)
	if err := os.WriteFile(path, []byte(`{"model":"sonnet"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}

	settings, err = LoadSettingsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Model != "sonnet" {
		t.Fatalf("model after change = %q", settings.Model)
	}
	if got := settingsFileCacheLen(); got != 1 {
		t.Fatalf("cache entries after change = %d", got)
	}
}

func TestSettingsChangeDetectorReportsChangesAndResetsCache(t *testing.T) {
	ResetSettingsFileCache()
	t.Cleanup(ResetSettingsFileCache)

	path := filepath.Join(t.TempDir(), "settings.json")
	detector, err := NewSettingsChangeDetector([]string{path})
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(`{"model":"opus"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := detector.DetectChanges([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Kind != SettingsChangeCreated {
		t.Fatalf("create changes = %#v", changes)
	}

	if _, err := LoadSettingsFile(path); err != nil {
		t.Fatal(err)
	}
	if got := settingsFileCacheLen(); got != 1 {
		t.Fatalf("cache entries = %d", got)
	}

	later := time.Now().Add(2 * time.Second)
	if err := os.WriteFile(path, []byte(`{"model":"sonnet"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	changes, err = detector.DetectChanges([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Kind != SettingsChangeModified {
		t.Fatalf("modify changes = %#v", changes)
	}
	if got := settingsFileCacheLen(); got != 0 {
		t.Fatalf("cache entries after detected change = %d", got)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	changes, err = detector.DetectChanges([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Kind != SettingsChangeDeleted {
		t.Fatalf("delete changes = %#v", changes)
	}
}

func TestSettingsChangeDetectorIgnoresUnchangedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":"opus"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	detector, err := NewSettingsChangeDetector([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	changes, err := detector.DetectChanges([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestValidateSettingsWarnsForInvalidSandboxFilesystem(t *testing.T) {
	_, warnings, err := ParseSettingsJSON([]byte(`{
		"sandbox": {
			"filesystem": {
				"allowWrite": "../tmp",
				"denyRead": ["private", 42]
			}
		}
	}`), "settings.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %#v", warnings)
	}
	paths := map[string]int{}
	for _, warning := range warnings {
		paths[warning.Path]++
	}
	if paths["sandbox.filesystem.allowWrite"] != 1 || paths["sandbox.filesystem.denyRead"] != 1 {
		t.Fatalf("warning paths = %#v warnings=%#v", paths, warnings)
	}
}

func TestValidateSettingsWarnsForMismatchedSettingsMarketplaceName(t *testing.T) {
	_, warnings, err := ParseSettingsJSON([]byte(`{
		"extraKnownMarketplaces": {
			"team": {
				"source": {
					"source": "settings",
					"name": "other",
					"plugins": []
				}
			}
		}
	}`), "settings.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if warnings[0].Path != "extraKnownMarketplaces.team.source.name" ||
		warnings[0].Expected != "team" ||
		warnings[0].InvalidValue != "other" {
		t.Fatalf("warning = %#v", warnings[0])
	}
}

func TestValidateSettingsAllowsMatchingSettingsMarketplaceAndFetchedSources(t *testing.T) {
	_, warnings, err := ParseSettingsJSON([]byte(`{
		"extraKnownMarketplaces": {
			"team": {
				"source": {
					"source": "settings",
					"name": "team",
					"plugins": []
				}
			},
			"github-key": {
				"source": {
					"source": "github",
					"repo": "owner/repo"
				}
			}
		}
	}`), "settings.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestValidateSettingsWarnsForInvalidMarketplaceSources(t *testing.T) {
	_, warnings, err := ParseSettingsJSON([]byte(`{
		"extraKnownMarketplaces": {
			"bad-url": {
				"source": {
					"source": "url",
					"url": "not a url",
					"headers": {"Authorization": 42}
				}
			},
			"bad-settings": {
				"source": {
					"source": "settings",
					"name": "inline"
				}
			},
			"missing-repo": {
				"source": {
					"source": "github",
					"sparsePaths": [".claude-plugin", 12]
				}
			}
		},
		"strictKnownMarketplaces": [
			{"source": "git", "url": 42},
			{"source": "unknown"}
		],
		"blockedMarketplaces": [
			{"source": "file", "path": "/opt/marketplace.json"},
			"bad"
		]
	}`), "settings.json")
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]int{}
	for _, warning := range warnings {
		paths[warning.Path]++
	}
	for _, path := range []string{
		"extraKnownMarketplaces.bad-url.source.url",
		"extraKnownMarketplaces.bad-url.source.headers.Authorization",
		"extraKnownMarketplaces.bad-settings.source.plugins",
		"extraKnownMarketplaces.bad-settings.source.name",
		"extraKnownMarketplaces.bad-settings.source.name",
		"extraKnownMarketplaces.missing-repo.source.repo",
		"extraKnownMarketplaces.missing-repo.source.sparsePaths[1]",
		"strictKnownMarketplaces[0].url",
		"strictKnownMarketplaces[1].source",
		"blockedMarketplaces[1]",
	} {
		if paths[path] == 0 {
			t.Fatalf("missing warning path %q in %#v warnings=%#v", path, paths, warnings)
		}
	}
	if paths["blockedMarketplaces[0].path"] != 0 {
		t.Fatalf("valid blocked marketplace produced warning paths=%#v warnings=%#v", paths, warnings)
	}
}

func TestMergeSettings(t *testing.T) {
	defaultWorktree := true
	overrideWorktree := false
	bridgeEnabled := true
	telemetryDisabled := false
	telemetryEnabled := true
	a := contracts.Settings{
		Env: map[string]string{"A": "1"},
		Permissions: &contracts.PermissionsSetting{
			Allow:       []string{"Read"},
			DefaultMode: contracts.PermissionDefault,
		},
		Worktree: &contracts.WorktreeSetting{
			Enabled:            &defaultWorktree,
			SparsePaths:        []string{"README.md"},
			SymlinkDirectories: []string{"cache"},
		},
		Advanced: &contracts.AdvancedSetting{
			Bridge:    &bridgeEnabled,
			Telemetry: &telemetryDisabled,
		},
		TelemetryExport: &contracts.TelemetryExportSetting{
			Path: "/tmp/old.jsonl",
			Headers: map[string]string{
				"X-Old": "old",
			},
		},
		Remote: &contracts.RemoteSetting{
			DefaultEnvironmentID: "env-base",
			AuthToken:            "base-token",
		},
	}
	b := contracts.Settings{
		Env:   map[string]string{"A": "2", "B": "3"},
		Model: "sonnet",
		Permissions: &contracts.PermissionsSetting{
			Deny:        []string{"Bash(rm *)"},
			DefaultMode: contracts.PermissionPlan,
		},
		Worktree: &contracts.WorktreeSetting{
			Enabled:            &overrideWorktree,
			SparsePaths:        []string{"docs"},
			SymlinkDirectories: []string{"node_modules"},
		},
		Advanced: &contracts.AdvancedSetting{
			Telemetry: &telemetryEnabled,
		},
		TelemetryExport: &contracts.TelemetryExportSetting{
			Path: "/tmp/new.jsonl",
			URL:  "https://example.com/telemetry",
			Headers: map[string]string{
				"X-New": "new",
			},
		},
		Remote: &contracts.RemoteSetting{
			RegistrationURL: "https://example.com/register",
		},
	}
	merged := MergeSettings(a, b)
	if merged.Env["A"] != "2" || merged.Env["B"] != "3" {
		t.Fatalf("env = %#v", merged.Env)
	}
	if merged.Permissions.DefaultMode != contracts.PermissionPlan {
		t.Fatalf("default mode = %q", merged.Permissions.DefaultMode)
	}
	if len(merged.Permissions.Allow) != 1 || len(merged.Permissions.Deny) != 1 {
		t.Fatalf("permissions = %#v", merged.Permissions)
	}
	if merged.Worktree == nil || merged.Worktree.Enabled == nil || *merged.Worktree.Enabled {
		t.Fatalf("worktree enabled = %#v", merged.Worktree)
	}
	if len(merged.Worktree.SparsePaths) != 2 || merged.Worktree.SparsePaths[0] != "README.md" || merged.Worktree.SparsePaths[1] != "docs" {
		t.Fatalf("worktree sparse paths = %#v", merged.Worktree.SparsePaths)
	}
	if len(merged.Worktree.SymlinkDirectories) != 2 || merged.Worktree.SymlinkDirectories[0] != "cache" || merged.Worktree.SymlinkDirectories[1] != "node_modules" {
		t.Fatalf("worktree symlink dirs = %#v", merged.Worktree.SymlinkDirectories)
	}
	if merged.Advanced == nil || merged.Advanced.Bridge == nil || !*merged.Advanced.Bridge || merged.Advanced.Telemetry == nil || !*merged.Advanced.Telemetry {
		t.Fatalf("advanced = %#v", merged.Advanced)
	}
	if merged.TelemetryExport == nil ||
		merged.TelemetryExport.Path != "/tmp/new.jsonl" ||
		merged.TelemetryExport.URL != "https://example.com/telemetry" ||
		merged.TelemetryExport.Headers["X-New"] != "new" ||
		merged.TelemetryExport.Headers["X-Old"] != "" {
		t.Fatalf("telemetry export = %#v", merged.TelemetryExport)
	}
	if merged.Remote == nil ||
		merged.Remote.DefaultEnvironmentID != "env-base" ||
		merged.Remote.RegistrationURL != "https://example.com/register" ||
		merged.Remote.AuthToken != "base-token" {
		t.Fatalf("remote = %#v", merged.Remote)
	}
}

func TestMergeSettingsPreservesSandboxSettings(t *testing.T) {
	merged := MergeSettings(
		contracts.Settings{Sandbox: map[string]any{
			"enabled":                  true,
			"allowUnsandboxedCommands": true,
			"filesystem":               map[string]any{"allowWrite": []any{"tmp"}},
		}},
		contracts.Settings{Sandbox: map[string]any{
			"allowUnsandboxedCommands": false,
			"filesystem":               map[string]any{"denyRead": []any{"private"}},
		}},
	)
	if merged.Sandbox["enabled"] != true || merged.Sandbox["allowUnsandboxedCommands"] != false {
		t.Fatalf("sandbox = %#v", merged.Sandbox)
	}
	filesystem, ok := merged.Sandbox["filesystem"].(map[string]any)
	if !ok || filesystem["allowWrite"] == nil || filesystem["denyRead"] == nil {
		t.Fatalf("sandbox filesystem = %#v", merged.Sandbox["filesystem"])
	}
}

func TestMergeSettingsSourcesHonorsManagedPermissionRulesOnly(t *testing.T) {
	managedOnly := true
	policy := contracts.Settings{
		AllowManagedPermissionRulesOnly: &managedOnly,
		Permissions: &contracts.PermissionsSetting{
			Allow: []string{"Read"},
			Deny:  []string{"Bash(rm *)"},
		},
	}
	user := contracts.Settings{
		Permissions: &contracts.PermissionsSetting{
			Allow:                 []string{"Bash(ls *)"},
			Ask:                   []string{"Write"},
			AdditionalDirectories: []string{"/tmp/work"},
			DefaultMode:           contracts.PermissionPlan,
		},
	}
	merged := MergeSettingsSources(
		SourceSettings{Source: contracts.PermissionSourcePolicySettings, Settings: policy},
		SourceSettings{Source: contracts.PermissionSourceUserSettings, Settings: user},
	)
	if len(merged.Permissions.Allow) != 1 || merged.Permissions.Allow[0] != "Read" {
		t.Fatalf("allow = %#v", merged.Permissions.Allow)
	}
	if len(merged.Permissions.Deny) != 1 || merged.Permissions.Deny[0] != "Bash(rm *)" {
		t.Fatalf("deny = %#v", merged.Permissions.Deny)
	}
	if len(merged.Permissions.Ask) != 0 {
		t.Fatalf("ask = %#v", merged.Permissions.Ask)
	}
	if merged.Permissions.DefaultMode != contracts.PermissionPlan || len(merged.Permissions.AdditionalDirectories) != 1 {
		t.Fatalf("permissions metadata should still merge: %#v", merged.Permissions)
	}
}
