package contracts

import (
	"encoding/json"
	"fmt"
)

func (s *Settings) UnmarshalJSON(data []byte) error {
	type alias Settings
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if env, ok := raw["env"].(map[string]any); ok {
		coerced := map[string]string{}
		for key, value := range env {
			coerced[key] = fmt.Sprint(value)
		}
		raw["env"] = coerced
	}
	normalized, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	var a alias
	if err := json.Unmarshal(normalized, &a); err != nil {
		return err
	}
	for _, key := range knownSettingsJSONKeys {
		delete(raw, key)
	}
	*s = Settings(a)
	if len(raw) > 0 {
		s.Extra = raw
	}
	return nil
}

var knownSettingsJSONKeys = []string{
	"$schema",
	"apiKeyHelper",
	"awsCredentialExport",
	"awsAuthRefresh",
	"gcpAuthRefresh",
	"fileSuggestion",
	"respectGitignore",
	"cleanupPeriodDays",
	"env",
	"attribution",
	"includeCoAuthoredBy",
	"includeGitInstructions",
	"permissions",
	"mcpServers",
	"model",
	"availableModels",
	"modelOverrides",
	"enableAllProjectMcpServers",
	"enabledMcpjsonServers",
	"disabledMcpjsonServers",
	"allowedMcpServers",
	"deniedMcpServers",
	"hooks",
	"worktree",
	"disableAllHooks",
	"defaultShell",
	"allowManagedHooksOnly",
	"allowedHttpHookUrls",
	"httpHookAllowedEnvVars",
	"allowManagedPermissionRulesOnly",
	"allowManagedMcpServersOnly",
	"strictPluginOnlyCustomization",
	"statusLine",
	"enabledPlugins",
	"extraKnownMarketplaces",
	"strictKnownMarketplaces",
	"blockedMarketplaces",
	"forceLoginMethod",
	"forceLoginOrgUUID",
	"otelHeadersHelper",
	"outputStyle",
	"language",
	"skipWebFetchPreflight",
	"sandbox",
	"feedbackSurveyRate",
	"spinnerTipsEnabled",
	"spinnerVerbs",
	"spinnerTipsOverride",
	"syntaxHighlightingDisabled",
	"terminalTitleFromRename",
	"alwaysThinkingEnabled",
	"effortLevel",
	"advisorModel",
	"fastMode",
	"fastModePerSessionOptIn",
	"promptSuggestionEnabled",
	"showClearContextOnPlanAccept",
	"agent",
	"companyAnnouncements",
	"pluginConfigs",
	"remote",
	"advanced",
	"telemetryExport",
	"autoUpdatesChannel",
	"plugins",
}

func (s Settings) MarshalJSON() ([]byte, error) {
	type alias Settings
	data, err := json.Marshal(alias(s))
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	delete(raw, "Extra")
	for k, v := range s.Extra {
		raw[k] = v
	}
	return json.Marshal(raw)
}
