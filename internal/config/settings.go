package config

import (
	"encoding/json"

	"ccgo/internal/contracts"
)

type SourceSettings struct {
	Source   contracts.PermissionRuleSource
	Settings contracts.Settings
}

func LoadSettingsFile(path string) (contracts.Settings, error) {
	settings, _, err := LoadSettingsFileWithWarnings(path)
	return settings, err
}

func loadSettingsFileStrict(path string) (contracts.Settings, error) {
	data, err := readSettingsFile(path)
	if err != nil {
		return contracts.Settings{}, err
	}
	if len(data) == 0 {
		return contracts.Settings{}, nil
	}
	var settings contracts.Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return contracts.Settings{}, err
	}
	return settings, nil
}

func MergeSettings(settings ...contracts.Settings) contracts.Settings {
	var out contracts.Settings
	for _, s := range settings {
		if s.Schema != "" {
			out.Schema = s.Schema
		}
		if s.APIKeyHelper != "" {
			out.APIKeyHelper = s.APIKeyHelper
		}
		if s.AWSCredentialExport != "" {
			out.AWSCredentialExport = s.AWSCredentialExport
		}
		if s.AWSAuthRefresh != "" {
			out.AWSAuthRefresh = s.AWSAuthRefresh
		}
		if s.GCPAuthRefresh != "" {
			out.GCPAuthRefresh = s.GCPAuthRefresh
		}
		if s.FileSuggestion != nil {
			cp := *s.FileSuggestion
			out.FileSuggestion = &cp
		}
		if s.RespectGitignore != nil {
			out.RespectGitignore = clonePtr(s.RespectGitignore)
		}
		if s.CleanupPeriodDays != nil {
			out.CleanupPeriodDays = clonePtr(s.CleanupPeriodDays)
		}
		if s.Env != nil {
			if out.Env == nil {
				out.Env = map[string]string{}
			}
			for k, v := range s.Env {
				out.Env[k] = v
			}
		}
		if s.Permissions != nil {
			out.Permissions = mergePermissions(out.Permissions, s.Permissions)
		}
		if s.MCPServers != nil {
			if out.MCPServers == nil {
				out.MCPServers = map[string]contracts.MCPServer{}
			}
			for k, v := range s.MCPServers {
				out.MCPServers[k] = v
			}
		}
		if s.Model != "" {
			out.Model = s.Model
		}
		out.AvailableModels = mergeStrings(out.AvailableModels, s.AvailableModels)
		out.ModelOverrides = mergeStringMap(out.ModelOverrides, s.ModelOverrides)
		if s.EnableAllProjectMCPServers != nil {
			out.EnableAllProjectMCPServers = clonePtr(s.EnableAllProjectMCPServers)
		}
		out.EnabledMCPJSONServers = mergeStrings(out.EnabledMCPJSONServers, s.EnabledMCPJSONServers)
		out.DisabledMCPJSONServers = mergeStrings(out.DisabledMCPJSONServers, s.DisabledMCPJSONServers)
		out.AllowedMCPServers = append(out.AllowedMCPServers, s.AllowedMCPServers...)
		out.DeniedMCPServers = append(out.DeniedMCPServers, s.DeniedMCPServers...)
		if s.Hooks != nil {
			out.Hooks = mergeAnyMap(out.Hooks, s.Hooks)
		}
		if s.Worktree != nil {
			if out.Worktree == nil {
				out.Worktree = &contracts.WorktreeSetting{}
			}
			if s.Worktree.Enabled != nil {
				out.Worktree.Enabled = clonePtr(s.Worktree.Enabled)
			}
			if s.Worktree.Default != nil {
				out.Worktree.Default = clonePtr(s.Worktree.Default)
			}
			if s.Worktree.Auto != nil {
				out.Worktree.Auto = clonePtr(s.Worktree.Auto)
			}
			out.Worktree.SymlinkDirectories = mergeStrings(out.Worktree.SymlinkDirectories, s.Worktree.SymlinkDirectories)
			out.Worktree.SparsePaths = mergeStrings(out.Worktree.SparsePaths, s.Worktree.SparsePaths)
		}
		if s.DisableAllHooks != nil {
			out.DisableAllHooks = clonePtr(s.DisableAllHooks)
		}
		if s.DefaultShell != "" {
			out.DefaultShell = s.DefaultShell
		}
		if s.AllowManagedHooksOnly != nil {
			out.AllowManagedHooksOnly = clonePtr(s.AllowManagedHooksOnly)
		}
		out.AllowedHTTPHookURLs = mergeStrings(out.AllowedHTTPHookURLs, s.AllowedHTTPHookURLs)
		out.HTTPHookAllowedEnvVars = mergeStrings(out.HTTPHookAllowedEnvVars, s.HTTPHookAllowedEnvVars)
		if s.AllowManagedPermissionRulesOnly != nil {
			out.AllowManagedPermissionRulesOnly = clonePtr(s.AllowManagedPermissionRulesOnly)
		}
		if s.AllowManagedMCPServersOnly != nil {
			out.AllowManagedMCPServersOnly = clonePtr(s.AllowManagedMCPServersOnly)
		}
		if s.StrictPluginOnlyCustomization != nil {
			out.StrictPluginOnlyCustomization = s.StrictPluginOnlyCustomization
		}
		if s.StatusLine != nil {
			cp := *s.StatusLine
			out.StatusLine = &cp
		}
		out.EnabledPlugins = mergeAnyMap(out.EnabledPlugins, s.EnabledPlugins)
		out.ExtraKnownMarketplaces = mergeAnyMap(out.ExtraKnownMarketplaces, s.ExtraKnownMarketplaces)
		out.StrictKnownMarketplaces = append(out.StrictKnownMarketplaces, s.StrictKnownMarketplaces...)
		out.BlockedMarketplaces = append(out.BlockedMarketplaces, s.BlockedMarketplaces...)
		if s.ForceLoginMethod != "" {
			out.ForceLoginMethod = s.ForceLoginMethod
		}
		if s.ForceLoginOrgUUID != "" {
			out.ForceLoginOrgUUID = s.ForceLoginOrgUUID
		}
		if s.OtelHeadersHelper != "" {
			out.OtelHeadersHelper = s.OtelHeadersHelper
		}
		if s.OutputStyle != "" {
			out.OutputStyle = s.OutputStyle
		}
		if s.Language != "" {
			out.Language = s.Language
		}
		if s.SkipWebFetchPreflight != nil {
			out.SkipWebFetchPreflight = clonePtr(s.SkipWebFetchPreflight)
		}
		out.Sandbox = mergeNestedAnyMap(out.Sandbox, s.Sandbox)
		if s.FeedbackSurveyRate != nil {
			out.FeedbackSurveyRate = clonePtr(s.FeedbackSurveyRate)
		}
		if s.SpinnerTipsEnabled != nil {
			out.SpinnerTipsEnabled = clonePtr(s.SpinnerTipsEnabled)
		}
		if s.SpinnerVerbs != nil {
			cp := *s.SpinnerVerbs
			cp.Verbs = append([]string(nil), s.SpinnerVerbs.Verbs...)
			out.SpinnerVerbs = &cp
		}
		if s.SpinnerTipsOverride != nil {
			cp := *s.SpinnerTipsOverride
			cp.Tips = append([]string(nil), s.SpinnerTipsOverride.Tips...)
			if s.SpinnerTipsOverride.ExcludeDefault != nil {
				cp.ExcludeDefault = clonePtr(s.SpinnerTipsOverride.ExcludeDefault)
			}
			out.SpinnerTipsOverride = &cp
		}
		if s.SyntaxHighlightingDisabled != nil {
			out.SyntaxHighlightingDisabled = clonePtr(s.SyntaxHighlightingDisabled)
		}
		if s.TerminalTitleFromRename != nil {
			out.TerminalTitleFromRename = clonePtr(s.TerminalTitleFromRename)
		}
		if s.AlwaysThinkingEnabled != nil {
			out.AlwaysThinkingEnabled = clonePtr(s.AlwaysThinkingEnabled)
		}
		if s.EffortLevel != "" {
			out.EffortLevel = s.EffortLevel
		}
		if s.AdvisorModel != "" {
			out.AdvisorModel = s.AdvisorModel
		}
		if s.FastMode != nil {
			out.FastMode = clonePtr(s.FastMode)
		}
		if s.FastModePerSessionOptIn != nil {
			out.FastModePerSessionOptIn = clonePtr(s.FastModePerSessionOptIn)
		}
		if s.PromptSuggestionEnabled != nil {
			out.PromptSuggestionEnabled = clonePtr(s.PromptSuggestionEnabled)
		}
		if s.ShowClearContextOnPlanAccept != nil {
			out.ShowClearContextOnPlanAccept = clonePtr(s.ShowClearContextOnPlanAccept)
		}
		if s.Agent != "" {
			out.Agent = s.Agent
		}
		out.CompanyAnnouncements = mergeStrings(out.CompanyAnnouncements, s.CompanyAnnouncements)
		if s.PluginConfigs != nil {
			if out.PluginConfigs == nil {
				out.PluginConfigs = map[string]contracts.PluginConfig{}
			}
			for k, v := range s.PluginConfigs {
				out.PluginConfigs[k] = v
			}
		}
		if s.Remote != nil {
			out.Remote = mergeRemoteSetting(out.Remote, s.Remote)
		}
		if s.Advanced != nil {
			out.Advanced = mergeAdvancedSetting(out.Advanced, s.Advanced)
		}
		if s.TelemetryExport != nil {
			out.TelemetryExport = cloneTelemetryExportSetting(s.TelemetryExport)
		}
		if s.AutoUpdatesChannel != "" {
			out.AutoUpdatesChannel = s.AutoUpdatesChannel
		}
		if s.Plugins != nil {
			out.Plugins = mergeAnyMap(out.Plugins, s.Plugins)
		}
		if s.Extra != nil {
			out.Extra = mergeAnyMap(out.Extra, s.Extra)
		}
	}
	return out
}

func cloneTelemetryExportSetting(setting *contracts.TelemetryExportSetting) *contracts.TelemetryExportSetting {
	if setting == nil {
		return nil
	}
	cp := *setting
	if setting.Headers != nil {
		cp.Headers = map[string]string{}
		for key, value := range setting.Headers {
			cp.Headers[key] = value
		}
	}
	return &cp
}

func mergeRemoteSetting(base *contracts.RemoteSetting, override *contracts.RemoteSetting) *contracts.RemoteSetting {
	if override == nil {
		if base == nil {
			return nil
		}
		cp := *base
		return &cp
	}
	var out contracts.RemoteSetting
	if base != nil {
		out = *base
	}
	if override.DefaultEnvironmentID != "" {
		out.DefaultEnvironmentID = override.DefaultEnvironmentID
	}
	if override.RegistrationURL != "" {
		out.RegistrationURL = override.RegistrationURL
	}
	if override.AuthToken != "" {
		out.AuthToken = override.AuthToken
	}
	return &out
}

func MergeSettingsSources(sources ...SourceSettings) contracts.Settings {
	settings := make([]contracts.Settings, 0, len(sources))
	for _, source := range sources {
		settings = append(settings, source.Settings)
	}
	merged := MergeSettings(settings...)
	if merged.AllowManagedPermissionRulesOnly == nil || !*merged.AllowManagedPermissionRulesOnly || merged.Permissions == nil {
		return merged
	}
	var policyAllow []string
	var policyDeny []string
	var policyAsk []string
	for _, source := range sources {
		if source.Source != contracts.PermissionSourcePolicySettings || source.Settings.Permissions == nil {
			continue
		}
		policyAllow = append(policyAllow, source.Settings.Permissions.Allow...)
		policyDeny = append(policyDeny, source.Settings.Permissions.Deny...)
		policyAsk = append(policyAsk, source.Settings.Permissions.Ask...)
	}
	merged.Permissions.Allow = append([]string(nil), policyAllow...)
	merged.Permissions.Deny = append([]string(nil), policyDeny...)
	merged.Permissions.Ask = append([]string(nil), policyAsk...)
	return merged
}

func mergeAdvancedSetting(a, b *contracts.AdvancedSetting) *contracts.AdvancedSetting {
	if a == nil {
		cp := *b
		if b.Bridge != nil {
			cp.Bridge = clonePtr(b.Bridge)
		}
		if b.LSP != nil {
			cp.LSP = clonePtr(b.LSP)
		}
		if b.Telemetry != nil {
			cp.Telemetry = clonePtr(b.Telemetry)
		}
		if b.Chrome != nil {
			cp.Chrome = clonePtr(b.Chrome)
		}
		if b.Voice != nil {
			cp.Voice = clonePtr(b.Voice)
		}
		if b.ComputerUse != nil {
			cp.ComputerUse = clonePtr(b.ComputerUse)
		}
		if b.NativeIntegrations != nil {
			cp.NativeIntegrations = clonePtr(b.NativeIntegrations)
		}
		return &cp
	}
	out := *a
	if a.Bridge != nil {
		out.Bridge = clonePtr(a.Bridge)
	}
	if a.LSP != nil {
		out.LSP = clonePtr(a.LSP)
	}
	if a.Telemetry != nil {
		out.Telemetry = clonePtr(a.Telemetry)
	}
	if a.Chrome != nil {
		out.Chrome = clonePtr(a.Chrome)
	}
	if a.Voice != nil {
		out.Voice = clonePtr(a.Voice)
	}
	if a.ComputerUse != nil {
		out.ComputerUse = clonePtr(a.ComputerUse)
	}
	if a.NativeIntegrations != nil {
		out.NativeIntegrations = clonePtr(a.NativeIntegrations)
	}
	if b.Bridge != nil {
		out.Bridge = clonePtr(b.Bridge)
	}
	if b.LSP != nil {
		out.LSP = clonePtr(b.LSP)
	}
	if b.Telemetry != nil {
		out.Telemetry = clonePtr(b.Telemetry)
	}
	if b.Chrome != nil {
		out.Chrome = clonePtr(b.Chrome)
	}
	if b.Voice != nil {
		out.Voice = clonePtr(b.Voice)
	}
	if b.ComputerUse != nil {
		out.ComputerUse = clonePtr(b.ComputerUse)
	}
	if b.NativeIntegrations != nil {
		out.NativeIntegrations = clonePtr(b.NativeIntegrations)
	}
	return &out
}

func mergePermissions(a, b *contracts.PermissionsSetting) *contracts.PermissionsSetting {
	if a == nil {
		cp := *b
		cp.Allow = append([]string(nil), b.Allow...)
		cp.Deny = append([]string(nil), b.Deny...)
		cp.Ask = append([]string(nil), b.Ask...)
		cp.AdditionalDirectories = append([]string(nil), b.AdditionalDirectories...)
		return &cp
	}
	out := *a
	out.Allow = append(append([]string(nil), a.Allow...), b.Allow...)
	out.Deny = append(append([]string(nil), a.Deny...), b.Deny...)
	out.Ask = append(append([]string(nil), a.Ask...), b.Ask...)
	out.AdditionalDirectories = append(append([]string(nil), a.AdditionalDirectories...), b.AdditionalDirectories...)
	if b.DefaultMode != "" {
		out.DefaultMode = b.DefaultMode
	}
	if b.DisableBypassMode != nil && b.DisableBypassMode != "" {
		out.DisableBypassMode = b.DisableBypassMode
	}
	if b.DisableAutoMode != nil && b.DisableAutoMode != "" {
		out.DisableAutoMode = b.DisableAutoMode
	}
	return &out
}

func clonePtr[T any](in *T) *T {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func mergeStringMap(a, b map[string]string) map[string]string {
	if a == nil && b == nil {
		return nil
	}
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func mergeStrings(a, b []string) []string {
	if len(a) == 0 {
		return append([]string(nil), b...)
	}
	out := append([]string(nil), a...)
	seen := map[string]struct{}{}
	for _, item := range out {
		seen[item] = struct{}{}
	}
	for _, item := range b {
		if _, ok := seen[item]; ok {
			continue
		}
		out = append(out, item)
		seen[item] = struct{}{}
	}
	return out
}

func mergeAnyMap(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func mergeNestedAnyMap(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if existing, ok := out[k].(map[string]any); ok {
			if incoming, ok := v.(map[string]any); ok {
				out[k] = mergeNestedAnyMap(existing, incoming)
				continue
			}
		}
		out[k] = v
	}
	return out
}
