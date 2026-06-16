package permissions

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestParseRuleAndMatchCommand(t *testing.T) {
	rule, err := ParseRule(contracts.PermissionSourceUserSettings, contracts.PermissionAllow, "Bash(git status*)")
	if err != nil {
		t.Fatal(err)
	}
	if !rule.Matches(Request{ToolName: "Bash", Command: "git status --short"}) {
		t.Fatalf("expected command to match")
	}
	if rule.Matches(Request{ToolName: "Bash", Command: "git push origin main"}) {
		t.Fatalf("unexpected command match")
	}
}

func TestParseRuleEscapesAndLegacyNames(t *testing.T) {
	value := PermissionRuleValueFromString(`Bash(python -c "print\(1\)")`)
	if value.ToolName != "Bash" || value.RuleContent != `python -c "print(1)"` {
		t.Fatalf("value = %#v", value)
	}
	if got := PermissionRuleValueToString(value); got != `Bash(python -c "print\(1\)")` {
		t.Fatalf("string = %q", got)
	}
	legacy := PermissionRuleValueFromString("Task")
	if legacy.ToolName != "Agent" {
		t.Fatalf("legacy tool = %q", legacy.ToolName)
	}
	if got := PermissionRuleValueFromString("TaskStop"); got.ToolName != "KillTask" {
		t.Fatalf("task stop tool = %q", got.ToolName)
	}
	if got := PermissionRuleValueFromString("AgentOutputTool"); got.ToolName != "TaskOutput" {
		t.Fatalf("agent output tool = %q", got.ToolName)
	}
	if got := PermissionRuleValueFromString("TaskResume"); got.ToolName != "ResumeTask" {
		t.Fatalf("task resume tool = %q", got.ToolName)
	}
}

func TestShellRuleMatching(t *testing.T) {
	tests := []struct {
		pattern string
		command string
		want    bool
	}{
		{pattern: "git:*", command: "git", want: true},
		{pattern: "git:*", command: "git status", want: true},
		{pattern: "git *", command: "git", want: true},
		{pattern: "git *", command: "git status --short", want: true},
		{pattern: `echo \*`, command: "echo *", want: true},
		{pattern: `echo \*`, command: "echo abc", want: false},
	}
	for _, tt := range tests {
		if got := matchPattern(tt.pattern, tt.command); got != tt.want {
			t.Fatalf("matchPattern(%q, %q) = %v, want %v", tt.pattern, tt.command, got, tt.want)
		}
	}
}

func TestDenyBeatsAllow(t *testing.T) {
	engine := NewEngine(contracts.PermissionContext{Mode: contracts.PermissionDefault},
		MustParseRule(contracts.PermissionSourceUserSettings, contracts.PermissionAllow, "Bash(*)"),
		MustParseRule(contracts.PermissionSourcePolicySettings, contracts.PermissionDeny, "Bash(rm *)"),
	)

	got := engine.Decide(Request{ToolName: "Bash", Command: "rm -rf tmp"})
	if got.Behavior != contracts.PermissionDeny {
		t.Fatalf("behavior = %q, want deny", got.Behavior)
	}
}

func TestModes(t *testing.T) {
	tests := []struct {
		name string
		mode contracts.PermissionMode
		req  Request
		want contracts.PermissionBehavior
	}{
		{name: "default read", mode: contracts.PermissionDefault, req: Request{ToolName: "Read", ReadOnly: true}, want: contracts.PermissionAllow},
		{name: "default write", mode: contracts.PermissionDefault, req: Request{ToolName: "Write", WritesFiles: true}, want: contracts.PermissionAsk},
		{name: "dont ask write", mode: contracts.PermissionDontAsk, req: Request{ToolName: "Write", WritesFiles: true}, want: contracts.PermissionDeny},
		{name: "accept edits write", mode: contracts.PermissionAcceptEdits, req: Request{ToolName: "Edit", WritesFiles: true}, want: contracts.PermissionAllow},
		{name: "plan read", mode: contracts.PermissionPlan, req: Request{ToolName: "Read", ReadOnly: true}, want: contracts.PermissionAllow},
		{name: "plan write", mode: contracts.PermissionPlan, req: Request{ToolName: "Edit", WritesFiles: true}, want: contracts.PermissionAsk},
		{name: "bypass", mode: contracts.PermissionBypassPermissions, req: Request{ToolName: "Bash", Destructive: true}, want: contracts.PermissionAllow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine(contracts.PermissionContext{Mode: tt.mode, BypassAvailable: true, AutoAvailable: true})
			got := engine.Decide(tt.req)
			if got.Behavior != tt.want {
				t.Fatalf("behavior = %q, want %q", got.Behavior, tt.want)
			}
		})
	}
}

func TestSandboxOverrideRequiresConfirmation(t *testing.T) {
	tests := []struct {
		name  string
		mode  contracts.PermissionMode
		rules []Rule
		want  contracts.PermissionBehavior
	}{
		{name: "default asks", mode: contracts.PermissionDefault, want: contracts.PermissionAsk},
		{name: "dont ask denies", mode: contracts.PermissionDontAsk, want: contracts.PermissionDeny},
		{name: "bypass allows", mode: contracts.PermissionBypassPermissions, want: contracts.PermissionAllow},
		{
			name:  "allow rule does not bypass sandbox confirmation",
			mode:  contracts.PermissionDefault,
			rules: []Rule{MustParseRule(contracts.PermissionSourceSession, contracts.PermissionAllow, "Bash(git status*)")},
			want:  contracts.PermissionAsk,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine(contracts.PermissionContext{Mode: tt.mode, BypassAvailable: true}, tt.rules...)
			got := engine.Decide(Request{
				ToolName:                  "Bash",
				Command:                   "git status --short",
				ReadOnly:                  true,
				DangerouslyDisableSandbox: true,
			})
			if got.Behavior != tt.want || !strings.Contains(got.Message, "sandbox override") {
				t.Fatalf("decision = %#v, want %q sandbox override decision", got, tt.want)
			}
		})
	}
}

func TestSandboxAllowUnsandboxedCommandsSettingControlsOverride(t *testing.T) {
	disallow := false
	engine := NewEngine(contracts.PermissionContext{
		Mode:                     contracts.PermissionBypassPermissions,
		BypassAvailable:          true,
		AllowUnsandboxedCommands: &disallow,
	})
	got := engine.Decide(Request{
		ToolName:                  "Bash",
		Command:                   "git status --short",
		ReadOnly:                  true,
		DangerouslyDisableSandbox: true,
	})
	if got.Behavior != contracts.PermissionDeny || !strings.Contains(got.Message, "allowUnsandboxedCommands") {
		t.Fatalf("decision = %#v", got)
	}
}

func TestNewEngineFromSettingsSourcesMergesSandboxAllowUnsandboxedCommands(t *testing.T) {
	engine, err := NewEngineFromSettingsSources(false,
		SettingsSource{
			Source:  contracts.PermissionSourcePolicySettings,
			Sandbox: map[string]any{"allowUnsandboxedCommands": true},
		},
		SettingsSource{
			Source:  contracts.PermissionSourceUserSettings,
			Sandbox: map[string]any{"allowUnsandboxedCommands": false},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	value := engine.Context().AllowUnsandboxedCommands
	if value == nil || *value {
		t.Fatalf("allow unsandboxed commands = %#v", value)
	}
	got := engine.Decide(Request{
		ToolName:                  "Bash",
		Command:                   "git status --short",
		ReadOnly:                  true,
		DangerouslyDisableSandbox: true,
	})
	if got.Behavior != contracts.PermissionDeny {
		t.Fatalf("decision = %#v", got)
	}
}

func TestSandboxFilesystemPolicyAffectsPathDecisions(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	policy := &contracts.SandboxFilesystemPolicy{
		AllowWrite: []string{outside},
		DenyWrite:  []string{"blocked-write"},
		DenyRead:   []string{"blocked-read"},
		AllowRead:  []string{"blocked-read/public"},
	}
	engine := NewEngine(contracts.PermissionContext{
		Mode:              contracts.PermissionDefault,
		SandboxFilesystem: policy,
	})

	deniedRead := engine.Decide(Request{
		ToolName:         "Read",
		Path:             filepath.Join(root, "blocked-read", "secret.txt"),
		WorkingDirectory: root,
		ReadOnly:         true,
	})
	if deniedRead.Behavior != contracts.PermissionDeny || !strings.Contains(deniedRead.Message, "denyRead") {
		t.Fatalf("denied read = %#v", deniedRead)
	}

	allowedRead := engine.Decide(Request{
		ToolName:         "Read",
		Path:             filepath.Join(root, "blocked-read", "public", "note.txt"),
		WorkingDirectory: root,
		ReadOnly:         true,
	})
	if allowedRead.Behavior != contracts.PermissionAllow {
		t.Fatalf("allowed read = %#v", allowedRead)
	}

	deniedWrite := engine.Decide(Request{
		ToolName:         "Write",
		Path:             filepath.Join(root, "blocked-write", "out.txt"),
		WorkingDirectory: root,
		WritesFiles:      true,
	})
	if deniedWrite.Behavior != contracts.PermissionDeny || !strings.Contains(deniedWrite.Message, "denyWrite") {
		t.Fatalf("denied write = %#v", deniedWrite)
	}

	allowedWrite := engine.Decide(Request{
		ToolName:         "Write",
		Path:             filepath.Join(outside, "out.txt"),
		WorkingDirectory: root,
		WritesFiles:      true,
	})
	if allowedWrite.Behavior != contracts.PermissionAllow || !strings.Contains(allowedWrite.Message, "allowWrite") {
		t.Fatalf("allowed write = %#v", allowedWrite)
	}

	sensitiveWrite := engine.Decide(Request{
		ToolName:         "Write",
		Path:             filepath.Join(root, ".git", "config"),
		WorkingDirectory: root,
		WritesFiles:      true,
	})
	if sensitiveWrite.Behavior != contracts.PermissionAsk || !strings.Contains(sensitiveWrite.Message, "sensitive") {
		t.Fatalf("sensitive write = %#v", sensitiveWrite)
	}
}

func TestSandboxPathMatchesRelativePaths(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "blocked-read", "secret.txt")
	if !sandboxPathMatches(PathsForPermissionCheck(path), root, []string{"blocked-read"}) {
		t.Fatalf("expected relative sandbox path to match %q within %q", path, root)
	}
	if sandboxPathMatches(PathsForPermissionCheck(path), root, []string{"blocked"}) {
		t.Fatalf("unexpected partial path match")
	}
}

func TestNewEngineFromSettingsSourcesMergesSandboxFilesystem(t *testing.T) {
	engine, err := NewEngineFromSettingsSources(false,
		SettingsSource{
			Source: contracts.PermissionSourcePolicySettings,
			Sandbox: map[string]any{
				"filesystem": map[string]any{
					"allowWrite": []any{"/tmp/policy-write"},
					"denyRead":   []any{"/tmp/secret"},
				},
			},
		},
		SettingsSource{
			Source: contracts.PermissionSourceUserSettings,
			Sandbox: map[string]any{
				"filesystem": map[string]any{
					"allowWrite": []any{"/tmp/user-write"},
					"allowRead":  []any{"/tmp/secret/public"},
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	policy := engine.Context().SandboxFilesystem
	if policy == nil || len(policy.AllowWrite) != 2 || len(policy.DenyRead) != 1 || len(policy.AllowRead) != 1 {
		t.Fatalf("sandbox filesystem = %#v", policy)
	}
}

func TestEnginePathSafetyRunsBeforeAllowRules(t *testing.T) {
	root := t.TempDir()
	engine := NewEngine(contracts.PermissionContext{Mode: contracts.PermissionAcceptEdits},
		MustParseRule(contracts.PermissionSourceUserSettings, contracts.PermissionAllow, "Edit(*)"),
	)
	got := engine.Decide(Request{
		ToolName:         "Edit",
		Path:             filepath.Join(root, ".git", "config"),
		WorkingDirectory: root,
		WritesFiles:      true,
	})
	if got.Behavior != contracts.PermissionAsk || !strings.Contains(got.Message, "sensitive") {
		t.Fatalf("decision = %#v", got)
	}
}

func TestEngineAllowsInternalEditablePathBeforeSafety(t *testing.T) {
	root := t.TempDir()
	scratchpad := filepath.Join(t.TempDir(), ".claude", "scratchpad")
	engine := NewEngine(contracts.PermissionContext{Mode: contracts.PermissionDefault})
	got := engine.Decide(Request{
		ToolName:         "Write",
		Path:             filepath.Join(scratchpad, "note.txt"),
		WorkingDirectory: root,
		WritesFiles:      true,
		InternalPaths: InternalPathContext{
			ScratchpadEnabled: true,
			ScratchpadDir:     scratchpad,
		},
	})
	if got.Behavior != contracts.PermissionAllow || !strings.Contains(got.Message, "scratchpad") {
		t.Fatalf("decision = %#v", got)
	}
}

func TestSettingsRulesUseJSONInputTarget(t *testing.T) {
	settings := &contracts.PermissionsSetting{
		Allow:       []string{"Read(/tmp/*)"},
		DefaultMode: contracts.PermissionDontAsk,
	}
	engine, err := NewEngineFromSettings(contracts.PermissionSourceProjectSettings, settings)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := json.Marshal(map[string]string{"path": "/tmp/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	got := engine.Decide(Request{ToolName: "Read", Input: raw})
	if got.Behavior != contracts.PermissionAllow {
		t.Fatalf("behavior = %q, want allow", got.Behavior)
	}
}

func TestNewEngineFromSettingsSourcesHonorsManagedRulesOnly(t *testing.T) {
	engine, err := NewEngineFromSettingsSources(true,
		SettingsSource{
			Source: contracts.PermissionSourcePolicySettings,
			Permissions: &contracts.PermissionsSetting{
				Deny: []string{"Bash(rm *)"},
			},
		},
		SettingsSource{
			Source: contracts.PermissionSourceUserSettings,
			Permissions: &contracts.PermissionsSetting{
				Allow:                 []string{"Bash(rm *)"},
				DefaultMode:           contracts.PermissionPlan,
				AdditionalDirectories: []string{"/tmp/work"},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if engine.Mode() != contracts.PermissionPlan {
		t.Fatalf("mode = %q", engine.Mode())
	}
	if _, ok := engine.Context().AdditionalWorkingDirectories["/tmp/work"]; !ok {
		t.Fatalf("additional directories = %#v", engine.Context().AdditionalWorkingDirectories)
	}
	decision := engine.Decide(Request{ToolName: "Bash", Command: "rm -rf /tmp/x"})
	if decision.Behavior != contracts.PermissionDeny {
		t.Fatalf("decision = %#v", decision)
	}
	if len(engine.Rules()) != 1 || engine.Rules()[0].Source != contracts.PermissionSourcePolicySettings {
		t.Fatalf("rules = %#v", engine.Rules())
	}
}

func TestApplyRuleUpdate(t *testing.T) {
	engine := NewEngine(contracts.PermissionContext{Mode: contracts.PermissionDefault})
	next, err := engine.ApplyUpdate(contracts.PermissionUpdate{
		Type:        "addRules",
		Destination: string(contracts.PermissionSourceSession),
		Behavior:    contracts.PermissionAllow,
		Rules:       []contracts.PermissionRuleValue{{ToolName: "Bash", RuleContent: "go test *"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := next.Decide(Request{ToolName: "Bash", Command: "go test ./..."})
	if got.Behavior != contracts.PermissionAllow {
		t.Fatalf("behavior = %q, want allow", got.Behavior)
	}
}

func TestReplaceAndRemoveRules(t *testing.T) {
	engine := NewEngine(contracts.PermissionContext{Mode: contracts.PermissionDefault},
		MustParseRule(contracts.PermissionSourceSession, contracts.PermissionAllow, "Bash(go test *)"),
	)
	replaced, err := engine.ApplyUpdate(contracts.PermissionUpdate{
		Type:        "replaceRules",
		Destination: string(contracts.PermissionSourceSession),
		Behavior:    contracts.PermissionAllow,
		Rules:       []contracts.PermissionRuleValue{{ToolName: "Bash", RuleContent: "npm test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := replaced.Decide(Request{ToolName: "Bash", Command: "go test ./..."}); got.Behavior == contracts.PermissionAllow {
		t.Fatalf("old rule still matched")
	}
	removed, err := replaced.ApplyUpdate(contracts.PermissionUpdate{
		Type:        "removeRules",
		Destination: string(contracts.PermissionSourceSession),
		Behavior:    contracts.PermissionAllow,
		Rules:       []contracts.PermissionRuleValue{{ToolName: "Bash", RuleContent: "npm test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := removed.Decide(Request{ToolName: "Bash", Command: "npm test"}); got.Behavior == contracts.PermissionAllow {
		t.Fatalf("removed rule still matched")
	}
}

func TestValidatePermissionRule(t *testing.T) {
	if got := ValidatePermissionRule("bash(ls)"); got.Valid || got.Error == "" {
		t.Fatalf("got = %#v", got)
	}
	if got := ValidatePermissionRule("Bash(:*)"); got.Valid || got.Error == "" {
		t.Fatalf("got = %#v", got)
	}
	if got := ValidatePermissionRule(`Bash(python -c "print\(1\)")`); !got.Valid {
		t.Fatalf("got = %#v", got)
	}
	if got := ValidatePermissionRule("mcp__server__tool(pattern)"); got.Valid {
		t.Fatalf("got = %#v", got)
	}
	if got := ValidatePermissionRule("WebSearch(foo*)"); got.Valid || got.Error != "WebSearch does not support wildcards" {
		t.Fatalf("got = %#v", got)
	}
	if got := ValidatePermissionRule("WebFetch(https://example.com)"); got.Valid || got.Error != "WebFetch permissions use domain format, not URLs" {
		t.Fatalf("got = %#v", got)
	}
	if got := ValidatePermissionRule("WebFetch(domain:example.com)"); !got.Valid {
		t.Fatalf("got = %#v", got)
	}
}
