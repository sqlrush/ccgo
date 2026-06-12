package contracts

type Settings struct {
	Schema                          string                      `json:"$schema,omitempty"`
	APIKeyHelper                    string                      `json:"apiKeyHelper,omitempty"`
	AWSCredentialExport             string                      `json:"awsCredentialExport,omitempty"`
	AWSAuthRefresh                  string                      `json:"awsAuthRefresh,omitempty"`
	GCPAuthRefresh                  string                      `json:"gcpAuthRefresh,omitempty"`
	FileSuggestion                  *CommandSetting             `json:"fileSuggestion,omitempty"`
	RespectGitignore                *bool                       `json:"respectGitignore,omitempty"`
	CleanupPeriodDays               *int                        `json:"cleanupPeriodDays,omitempty"`
	Env                             map[string]string           `json:"env,omitempty"`
	Attribution                     *AttributionSetting         `json:"attribution,omitempty"`
	IncludeCoAuthoredBy             *bool                       `json:"includeCoAuthoredBy,omitempty"`
	IncludeGitInstructions          *bool                       `json:"includeGitInstructions,omitempty"`
	Permissions                     *PermissionsSetting         `json:"permissions,omitempty"`
	MCPServers                      map[string]MCPServer        `json:"mcpServers,omitempty"`
	Model                           string                      `json:"model,omitempty"`
	AvailableModels                 []string                    `json:"availableModels,omitempty"`
	ModelOverrides                  map[string]string           `json:"modelOverrides,omitempty"`
	EnableAllProjectMCPServers      *bool                       `json:"enableAllProjectMcpServers,omitempty"`
	EnabledMCPJSONServers           []string                    `json:"enabledMcpjsonServers,omitempty"`
	DisabledMCPJSONServers          []string                    `json:"disabledMcpjsonServers,omitempty"`
	AllowedMCPServers               []MCPServerPolicyEntry      `json:"allowedMcpServers,omitempty"`
	DeniedMCPServers                []MCPServerPolicyEntry      `json:"deniedMcpServers,omitempty"`
	Hooks                           map[string]any              `json:"hooks,omitempty"`
	Worktree                        *WorktreeSetting            `json:"worktree,omitempty"`
	DisableAllHooks                 *bool                       `json:"disableAllHooks,omitempty"`
	DefaultShell                    string                      `json:"defaultShell,omitempty"`
	AllowManagedHooksOnly           *bool                       `json:"allowManagedHooksOnly,omitempty"`
	AllowedHTTPHookURLs             []string                    `json:"allowedHttpHookUrls,omitempty"`
	HTTPHookAllowedEnvVars          []string                    `json:"httpHookAllowedEnvVars,omitempty"`
	AllowManagedPermissionRulesOnly *bool                       `json:"allowManagedPermissionRulesOnly,omitempty"`
	AllowManagedMCPServersOnly      *bool                       `json:"allowManagedMcpServersOnly,omitempty"`
	StrictPluginOnlyCustomization   any                         `json:"strictPluginOnlyCustomization,omitempty"`
	StatusLine                      *CommandSetting             `json:"statusLine,omitempty"`
	EnabledPlugins                  map[string]any              `json:"enabledPlugins,omitempty"`
	ExtraKnownMarketplaces          map[string]any              `json:"extraKnownMarketplaces,omitempty"`
	StrictKnownMarketplaces         []any                       `json:"strictKnownMarketplaces,omitempty"`
	BlockedMarketplaces             []any                       `json:"blockedMarketplaces,omitempty"`
	ForceLoginMethod                string                      `json:"forceLoginMethod,omitempty"`
	ForceLoginOrgUUID               string                      `json:"forceLoginOrgUUID,omitempty"`
	OtelHeadersHelper               string                      `json:"otelHeadersHelper,omitempty"`
	OutputStyle                     string                      `json:"outputStyle,omitempty"`
	Language                        string                      `json:"language,omitempty"`
	SkipWebFetchPreflight           *bool                       `json:"skipWebFetchPreflight,omitempty"`
	Sandbox                         map[string]any              `json:"sandbox,omitempty"`
	FeedbackSurveyRate              *float64                    `json:"feedbackSurveyRate,omitempty"`
	SpinnerTipsEnabled              *bool                       `json:"spinnerTipsEnabled,omitempty"`
	SpinnerVerbs                    *SpinnerVerbsSetting        `json:"spinnerVerbs,omitempty"`
	SpinnerTipsOverride             *SpinnerTipsOverrideSetting `json:"spinnerTipsOverride,omitempty"`
	SyntaxHighlightingDisabled      *bool                       `json:"syntaxHighlightingDisabled,omitempty"`
	TerminalTitleFromRename         *bool                       `json:"terminalTitleFromRename,omitempty"`
	AlwaysThinkingEnabled           *bool                       `json:"alwaysThinkingEnabled,omitempty"`
	EffortLevel                     string                      `json:"effortLevel,omitempty"`
	AdvisorModel                    string                      `json:"advisorModel,omitempty"`
	FastMode                        *bool                       `json:"fastMode,omitempty"`
	FastModePerSessionOptIn         *bool                       `json:"fastModePerSessionOptIn,omitempty"`
	PromptSuggestionEnabled         *bool                       `json:"promptSuggestionEnabled,omitempty"`
	ShowClearContextOnPlanAccept    *bool                       `json:"showClearContextOnPlanAccept,omitempty"`
	Agent                           string                      `json:"agent,omitempty"`
	CompanyAnnouncements            []string                    `json:"companyAnnouncements,omitempty"`
	PluginConfigs                   map[string]PluginConfig     `json:"pluginConfigs,omitempty"`
	Remote                          *RemoteSetting              `json:"remote,omitempty"`
	AutoUpdatesChannel              string                      `json:"autoUpdatesChannel,omitempty"`
	Plugins                         map[string]any              `json:"plugins,omitempty"`
	Extra                           map[string]any              `json:"-"`
}

type CommandSetting struct {
	Type    string `json:"type,omitempty"`
	Command string `json:"command,omitempty"`
	Padding *int   `json:"padding,omitempty"`
}

type AttributionSetting struct {
	Commit *string `json:"commit,omitempty"`
	PR     *string `json:"pr,omitempty"`
}

type WorktreeSetting struct {
	SymlinkDirectories []string `json:"symlinkDirectories,omitempty"`
	SparsePaths        []string `json:"sparsePaths,omitempty"`
}

type MCPServerPolicyEntry struct {
	Name          string   `json:"name,omitempty"`
	Command       string   `json:"command,omitempty"`
	URL           string   `json:"url,omitempty"`
	ServerName    string   `json:"serverName,omitempty"`
	ServerCommand []string `json:"serverCommand,omitempty"`
	ServerURL     string   `json:"serverUrl,omitempty"`
}

type SpinnerVerbsSetting struct {
	Mode  string   `json:"mode,omitempty"`
	Verbs []string `json:"verbs,omitempty"`
}

type SpinnerTipsOverrideSetting struct {
	ExcludeDefault *bool    `json:"excludeDefault,omitempty"`
	Tips           []string `json:"tips,omitempty"`
}

type PluginConfig struct {
	MCPServers map[string]map[string]any `json:"mcpServers,omitempty"`
	Options    map[string]any            `json:"options,omitempty"`
}

type RemoteSetting struct {
	DefaultEnvironmentID string `json:"defaultEnvironmentId,omitempty"`
}

type PermissionsSetting struct {
	Allow                 []string       `json:"allow,omitempty"`
	Deny                  []string       `json:"deny,omitempty"`
	Ask                   []string       `json:"ask,omitempty"`
	DefaultMode           PermissionMode `json:"defaultMode,omitempty"`
	DisableBypassMode     any            `json:"disableBypassPermissionsMode,omitempty"`
	DisableAutoMode       any            `json:"disableAutoMode,omitempty"`
	AdditionalDirectories []string       `json:"additionalDirectories,omitempty"`
}

type MCPServer struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Name    string            `json:"name,omitempty"`
	Scope   string            `json:"scope,omitempty"`
}
