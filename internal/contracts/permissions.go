package contracts

type PermissionMode string

const (
	PermissionDefault           PermissionMode = "default"
	PermissionAcceptEdits       PermissionMode = "acceptEdits"
	PermissionBypassPermissions PermissionMode = "bypassPermissions"
	PermissionDontAsk           PermissionMode = "dontAsk"
	PermissionPlan              PermissionMode = "plan"
	PermissionAuto              PermissionMode = "auto"
	PermissionBubble            PermissionMode = "bubble"
)

type PermissionBehavior string

const (
	PermissionAllow PermissionBehavior = "allow"
	PermissionDeny  PermissionBehavior = "deny"
	PermissionAsk   PermissionBehavior = "ask"
	// PermissionPassthrough is not a final decision in Claude Code; it lets
	// a tool-specific checker defer to the generic permission pipeline.
	PermissionPassthrough PermissionBehavior = "passthrough"
)

type PermissionRuleSource string

const (
	PermissionSourceUserSettings    PermissionRuleSource = "userSettings"
	PermissionSourceProjectSettings PermissionRuleSource = "projectSettings"
	PermissionSourceLocalSettings   PermissionRuleSource = "localSettings"
	PermissionSourceFlagSettings    PermissionRuleSource = "flagSettings"
	PermissionSourcePolicySettings  PermissionRuleSource = "policySettings"
	PermissionSourceCLIArg          PermissionRuleSource = "cliArg"
	PermissionSourceCommand         PermissionRuleSource = "command"
	PermissionSourceSession         PermissionRuleSource = "session"
)

type PermissionRuleValue struct {
	ToolName    string `json:"toolName"`
	RuleContent string `json:"ruleContent,omitempty"`
}

type PermissionRule struct {
	Source   PermissionRuleSource `json:"source"`
	Behavior PermissionBehavior   `json:"ruleBehavior"`
	Value    PermissionRuleValue  `json:"ruleValue"`
}

type PermissionDecision struct {
	Behavior       PermissionBehavior `json:"behavior"`
	Message        string             `json:"message,omitempty"`
	UpdatedInput   map[string]any     `json:"updatedInput,omitempty"`
	UserModified   bool               `json:"userModified,omitempty"`
	DecisionReason any                `json:"decisionReason,omitempty"`
	ToolUseID      ID                 `json:"toolUseID,omitempty"`
	AcceptFeedback string             `json:"acceptFeedback,omitempty"`
	Suggestions    []PermissionUpdate `json:"suggestions,omitempty"`
	BlockedPath    string             `json:"blockedPath,omitempty"`
	Metadata       map[string]any     `json:"metadata,omitempty"`
	ContentBlocks  []ContentBlock     `json:"contentBlocks,omitempty"`
}

type PermissionUpdate struct {
	Type        string                `json:"type"`
	Destination string                `json:"destination"`
	Rules       []PermissionRuleValue `json:"rules,omitempty"`
	Behavior    PermissionBehavior    `json:"behavior,omitempty"`
	Mode        PermissionMode        `json:"mode,omitempty"`
	Directories []string              `json:"directories,omitempty"`
}

type PermissionContext struct {
	Mode                         PermissionMode                    `json:"mode"`
	AdditionalWorkingDirectories map[string]PermissionRuleSource   `json:"additional_working_directories,omitempty"`
	AlwaysAllowRules             map[PermissionRuleSource][]string `json:"always_allow_rules,omitempty"`
	AlwaysDenyRules              map[PermissionRuleSource][]string `json:"always_deny_rules,omitempty"`
	AlwaysAskRules               map[PermissionRuleSource][]string `json:"always_ask_rules,omitempty"`
	BypassAvailable              bool                              `json:"bypass_available,omitempty"`
	AutoAvailable                bool                              `json:"auto_available,omitempty"`
	AllowUnsandboxedCommands     *bool                             `json:"allow_unsandboxed_commands,omitempty"`
	SandboxFilesystem            *SandboxFilesystemPolicy          `json:"sandbox_filesystem,omitempty"`
	StrippedDangerousRules       map[PermissionRuleSource][]string `json:"stripped_dangerous_rules,omitempty"`
	ShouldAvoidPermissionPrompts bool                              `json:"should_avoid_permission_prompts,omitempty"`
	AwaitAutomatedChecks         bool                              `json:"await_automated_checks_before_dialog,omitempty"`
	PrePlanMode                  PermissionMode                    `json:"pre_plan_mode,omitempty"`
}

type SandboxFilesystemPolicy struct {
	AllowWrite []string `json:"allow_write,omitempty"`
	DenyWrite  []string `json:"deny_write,omitempty"`
	DenyRead   []string `json:"deny_read,omitempty"`
	AllowRead  []string `json:"allow_read,omitempty"`
}
