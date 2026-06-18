package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/permissions"
)

type ValidationError struct {
	File         string
	Path         string
	Message      string
	Expected     string
	InvalidValue any
	Suggestion   string
}

type SettingsWithWarnings struct {
	Settings contracts.Settings
	Warnings []ValidationError
}

func LoadSettingsFileWithWarnings(path string) (contracts.Settings, []ValidationError, error) {
	data, err := readSettingsFile(path)
	if err != nil {
		return contracts.Settings{}, nil, err
	}
	return ParseSettingsJSON(data, path)
}

func ParseSettingsJSON(data []byte, filePath string) (contracts.Settings, []ValidationError, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return contracts.Settings{}, nil, nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return contracts.Settings{}, nil, err
	}
	warnings := FilterInvalidPermissionRules(raw, filePath)
	normalized, err := json.Marshal(raw)
	if err != nil {
		return contracts.Settings{}, warnings, err
	}
	var settings contracts.Settings
	if err := json.Unmarshal(normalized, &settings); err != nil {
		return contracts.Settings{}, warnings, err
	}
	warnings = append(warnings, ValidateSettings(settings, filePath)...)
	return settings, warnings, nil
}

func ValidateSettingsJSON(data []byte, filePath string) ([]ValidationError, error) {
	_, warnings, err := ParseSettingsJSON(data, filePath)
	return warnings, err
}

func FilterInvalidPermissionRules(data map[string]any, filePath string) []ValidationError {
	if data == nil {
		return nil
	}
	permissionsValue, ok := data["permissions"].(map[string]any)
	if !ok {
		return nil
	}
	var warnings []ValidationError
	for _, key := range []string{"allow", "deny", "ask"} {
		rules, ok := permissionsValue[key].([]any)
		if !ok {
			continue
		}
		filtered := make([]string, 0, len(rules))
		for _, rule := range rules {
			raw, ok := rule.(string)
			if !ok {
				warnings = append(warnings, ValidationError{
					File:         filePath,
					Path:         "permissions." + key,
					Message:      fmt.Sprintf("Non-string value in %s array was removed", key),
					InvalidValue: rule,
				})
				continue
			}
			result := permissions.ValidatePermissionRule(raw)
			if !result.Valid {
				message := fmt.Sprintf("Invalid permission rule %q was skipped", raw)
				if result.Error != "" {
					message += ": " + result.Error
				}
				if result.Suggestion != "" {
					message += ". " + result.Suggestion
				}
				warnings = append(warnings, ValidationError{
					File:         filePath,
					Path:         "permissions." + key,
					Message:      message,
					InvalidValue: raw,
					Suggestion:   result.Suggestion,
				})
				continue
			}
			filtered = append(filtered, raw)
		}
		permissionsValue[key] = filtered
	}
	return warnings
}

func ValidateSettings(settings contracts.Settings, filePath string) []ValidationError {
	var errors []ValidationError
	if settings.Permissions != nil {
		errors = append(errors, validatePermissionsSetting(*settings.Permissions, filePath)...)
	}
	if settings.Sandbox != nil {
		errors = append(errors, validateSandboxSetting(settings.Sandbox, filePath)...)
	}
	if settings.ExtraKnownMarketplaces != nil {
		errors = append(errors, validateExtraKnownMarketplaces(settings.ExtraKnownMarketplaces, filePath)...)
	}
	errors = append(errors, validateMarketplaceSourceList("strictKnownMarketplaces", settings.StrictKnownMarketplaces, filePath)...)
	errors = append(errors, validateMarketplaceSourceList("blockedMarketplaces", settings.BlockedMarketplaces, filePath)...)
	if settings.CleanupPeriodDays != nil && *settings.CleanupPeriodDays < 0 {
		errors = append(errors, ValidationError{
			File:         filePath,
			Path:         "cleanupPeriodDays",
			Message:      "Number must be greater than or equal to 0",
			Expected:     ">= 0",
			InvalidValue: *settings.CleanupPeriodDays,
		})
	}
	return errors
}

func validateExtraKnownMarketplaces(values map[string]any, filePath string) []ValidationError {
	var errors []ValidationError
	for key, rawEntry := range values {
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			errors = append(errors, ValidationError{
				File:         filePath,
				Path:         "extraKnownMarketplaces." + key,
				Message:      "Invalid marketplace entry. Expected object",
				Expected:     "object",
				InvalidValue: rawEntry,
			})
			continue
		}
		source, ok := entry["source"].(map[string]any)
		if !ok {
			errors = append(errors, ValidationError{
				File:         filePath,
				Path:         "extraKnownMarketplaces." + key + ".source",
				Message:      "Invalid marketplace source. Expected object",
				Expected:     "object",
				InvalidValue: entry["source"],
			})
			continue
		}
		sourcePath := "extraKnownMarketplaces." + key + ".source"
		errors = append(errors, validateMarketplaceSource(source, filePath, sourcePath)...)
		if sourceType, _ := source["source"].(string); sourceType != "settings" {
			continue
		}
		name, ok := source["name"].(string)
		if !ok {
			errors = append(errors, ValidationError{
				File:         filePath,
				Path:         "extraKnownMarketplaces." + key + ".source.name",
				Message:      "Settings-sourced marketplace source.name must be a string matching its extraKnownMarketplaces key",
				Expected:     key,
				InvalidValue: source["name"],
				Suggestion:   "Set source.name to " + key + ".",
			})
			continue
		}
		if name != key {
			errors = append(errors, ValidationError{
				File:         filePath,
				Path:         "extraKnownMarketplaces." + key + ".source.name",
				Message:      "Settings-sourced marketplace name must match its extraKnownMarketplaces key (got key \"" + key + "\" but source.name \"" + name + "\")",
				Expected:     key,
				InvalidValue: name,
				Suggestion:   "Set source.name to " + key + " or change the extraKnownMarketplaces key to " + name + ".",
			})
		}
	}
	return errors
}

func validateMarketplaceSourceList(path string, values []any, filePath string) []ValidationError {
	var errors []ValidationError
	for i, raw := range values {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		source, ok := raw.(map[string]any)
		if !ok {
			errors = append(errors, ValidationError{
				File:         filePath,
				Path:         itemPath,
				Message:      "Invalid marketplace source. Expected object",
				Expected:     "object",
				InvalidValue: raw,
			})
			continue
		}
		errors = append(errors, validateMarketplaceSource(source, filePath, itemPath)...)
	}
	return errors
}

func validateMarketplaceSource(source map[string]any, filePath string, path string) []ValidationError {
	sourceType, ok := source["source"].(string)
	if !ok || strings.TrimSpace(sourceType) == "" {
		return []ValidationError{{
			File:         filePath,
			Path:         path + ".source",
			Message:      "Marketplace source type is required",
			Expected:     "url | github | git | npm | file | directory | hostPattern | pathPattern | settings",
			InvalidValue: source["source"],
		}}
	}
	switch sourceType {
	case "url":
		var errors []ValidationError
		errors = append(errors, validateMarketplaceSourceString(source, filePath, path, "url")...)
		if raw, ok := source["url"].(string); ok && strings.TrimSpace(raw) != "" {
			parsed, err := url.Parse(raw)
			if err != nil || parsed.Scheme == "" || parsed.Host == "" {
				errors = append(errors, ValidationError{
					File:         filePath,
					Path:         path + ".url",
					Message:      "Invalid marketplace URL source. Expected absolute URL",
					Expected:     "absolute URL",
					InvalidValue: raw,
				})
			}
		}
		if headers, ok := source["headers"]; ok {
			errors = append(errors, validateStringRecord(headers, filePath, path+".headers")...)
		}
		return errors
	case "github":
		errors := validateMarketplaceSourceString(source, filePath, path, "repo")
		errors = append(errors, validateOptionalString(source, filePath, path, "ref")...)
		errors = append(errors, validateOptionalString(source, filePath, path, "path")...)
		errors = append(errors, validateOptionalStringArray(source, filePath, path, "sparsePaths")...)
		return errors
	case "git":
		errors := validateMarketplaceSourceString(source, filePath, path, "url")
		errors = append(errors, validateOptionalString(source, filePath, path, "ref")...)
		errors = append(errors, validateOptionalString(source, filePath, path, "path")...)
		errors = append(errors, validateOptionalStringArray(source, filePath, path, "sparsePaths")...)
		return errors
	case "npm":
		return validateMarketplaceSourceString(source, filePath, path, "package")
	case "file", "directory":
		return validateMarketplaceSourceString(source, filePath, path, "path")
	case "hostPattern":
		return validateMarketplaceSourceString(source, filePath, path, "hostPattern")
	case "pathPattern":
		return validateMarketplaceSourceString(source, filePath, path, "pathPattern")
	case "settings":
		errors := validateMarketplaceSourceString(source, filePath, path, "name")
		if raw, ok := source["plugins"]; !ok {
			errors = append(errors, ValidationError{
				File:     filePath,
				Path:     path + ".plugins",
				Message:  "Settings-sourced marketplace plugins are required",
				Expected: "array",
			})
		} else if _, ok := raw.([]any); !ok {
			errors = append(errors, ValidationError{
				File:         filePath,
				Path:         path + ".plugins",
				Message:      "Settings-sourced marketplace plugins must be an array",
				Expected:     "array",
				InvalidValue: raw,
			})
		}
		if name, ok := source["name"].(string); ok {
			errors = append(errors, validateMarketplaceName(name, filePath, path+".name")...)
		}
		return errors
	default:
		return []ValidationError{{
			File:         filePath,
			Path:         path + ".source",
			Message:      "Invalid marketplace source type",
			Expected:     "url | github | git | npm | file | directory | hostPattern | pathPattern | settings",
			InvalidValue: sourceType,
		}}
	}
}

func validateMarketplaceSourceString(source map[string]any, filePath string, path string, field string) []ValidationError {
	value, ok := source[field].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return []ValidationError{{
			File:         filePath,
			Path:         path + "." + field,
			Message:      "Marketplace source field is required",
			Expected:     "non-empty string",
			InvalidValue: source[field],
		}}
	}
	return nil
}

func validateOptionalString(source map[string]any, filePath string, path string, field string) []ValidationError {
	value, ok := source[field]
	if !ok {
		return nil
	}
	if _, ok := value.(string); !ok {
		return []ValidationError{{
			File:         filePath,
			Path:         path + "." + field,
			Message:      "Invalid marketplace source field. Expected string",
			Expected:     "string",
			InvalidValue: value,
		}}
	}
	return nil
}

func validateOptionalStringArray(source map[string]any, filePath string, path string, field string) []ValidationError {
	value, ok := source[field]
	if !ok {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return []ValidationError{{
			File:         filePath,
			Path:         path + "." + field,
			Message:      "Invalid marketplace source field. Expected string array",
			Expected:     "string[]",
			InvalidValue: value,
		}}
	}
	var errors []ValidationError
	for i, item := range items {
		if _, ok := item.(string); !ok {
			errors = append(errors, ValidationError{
				File:         filePath,
				Path:         fmt.Sprintf("%s.%s[%d]", path, field, i),
				Message:      "Invalid marketplace source field item. Expected string",
				Expected:     "string",
				InvalidValue: item,
			})
		}
	}
	return errors
}

func validateStringRecord(value any, filePath string, path string) []ValidationError {
	items, ok := value.(map[string]any)
	if !ok {
		return []ValidationError{{
			File:         filePath,
			Path:         path,
			Message:      "Invalid marketplace source field. Expected string record",
			Expected:     "object<string,string>",
			InvalidValue: value,
		}}
	}
	var errors []ValidationError
	for key, raw := range items {
		if _, ok := raw.(string); !ok {
			errors = append(errors, ValidationError{
				File:         filePath,
				Path:         path + "." + key,
				Message:      "Invalid marketplace source header value. Expected string",
				Expected:     "string",
				InvalidValue: raw,
			})
		}
	}
	return errors
}

func validateMarketplaceName(name string, filePath string, path string) []ValidationError {
	switch {
	case strings.TrimSpace(name) == "":
		return []ValidationError{{File: filePath, Path: path, Message: "Marketplace must have a name", Expected: "non-empty string", InvalidValue: name}}
	case strings.Contains(name, " "):
		return []ValidationError{{File: filePath, Path: path, Message: "Marketplace name cannot contain spaces", Expected: "kebab-case name", InvalidValue: name}}
	case strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") || name == ".":
		return []ValidationError{{File: filePath, Path: path, Message: `Marketplace name cannot contain path separators, "..", or be "."`, Expected: "safe marketplace name", InvalidValue: name}}
	case strings.EqualFold(name, "inline"):
		return []ValidationError{{File: filePath, Path: path, Message: `Marketplace name "inline" is reserved for session plugins`, Expected: "non-reserved marketplace name", InvalidValue: name}}
	case strings.EqualFold(name, "builtin"):
		return []ValidationError{{File: filePath, Path: path, Message: `Marketplace name "builtin" is reserved for built-in plugins`, Expected: "non-reserved marketplace name", InvalidValue: name}}
	case reservedOfficialMarketplaceName(name):
		return []ValidationError{{File: filePath, Path: path, Message: "Reserved official marketplace names cannot be used with settings sources", Expected: "non-official marketplace name", InvalidValue: name}}
	default:
		return nil
	}
}

func reservedOfficialMarketplaceName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "claude-code-marketplace",
		"claude-code-plugins",
		"claude-plugins-official",
		"anthropic-marketplace",
		"anthropic-plugins",
		"agent-skills",
		"life-sciences",
		"knowledge-work-plugins":
		return true
	default:
		return false
	}
}

func validateSandboxSetting(setting map[string]any, filePath string) []ValidationError {
	var errors []ValidationError
	for _, key := range []string{"enabled", "failIfUnavailable", "autoAllowBashIfSandboxed", "allowUnsandboxedCommands", "enableWeakerNestedSandbox", "enableWeakerNetworkIsolation"} {
		value, ok := setting[key]
		if !ok {
			continue
		}
		if _, ok := value.(bool); !ok {
			errors = append(errors, ValidationError{
				File:         filePath,
				Path:         "sandbox." + key,
				Message:      "Invalid value. Expected boolean",
				Expected:     "boolean",
				InvalidValue: value,
			})
		}
	}
	if filesystem, ok := setting["filesystem"]; ok {
		filesystemMap, ok := filesystem.(map[string]any)
		if !ok {
			errors = append(errors, ValidationError{
				File:         filePath,
				Path:         "sandbox.filesystem",
				Message:      "Invalid value. Expected object",
				Expected:     "object",
				InvalidValue: filesystem,
			})
			return errors
		}
		errors = append(errors, validateSandboxFilesystemSetting(filesystemMap, filePath)...)
	}
	return errors
}

func validateSandboxFilesystemSetting(setting map[string]any, filePath string) []ValidationError {
	var errors []ValidationError
	for _, key := range []string{"allowWrite", "denyWrite", "denyRead", "allowRead"} {
		value, ok := setting[key]
		if !ok {
			continue
		}
		items, ok := value.([]any)
		if !ok {
			errors = append(errors, ValidationError{
				File:         filePath,
				Path:         "sandbox.filesystem." + key,
				Message:      "Invalid value. Expected string array",
				Expected:     "string[]",
				InvalidValue: value,
			})
			continue
		}
		for _, item := range items {
			if _, ok := item.(string); !ok {
				errors = append(errors, ValidationError{
					File:         filePath,
					Path:         "sandbox.filesystem." + key,
					Message:      "Invalid value. Expected string array",
					Expected:     "string[]",
					InvalidValue: item,
				})
			}
		}
	}
	return errors
}

func validatePermissionsSetting(setting contracts.PermissionsSetting, filePath string) []ValidationError {
	var errors []ValidationError
	if setting.DefaultMode != "" && !validPermissionMode(setting.DefaultMode) {
		errors = append(errors, ValidationError{
			File:         filePath,
			Path:         "permissions.defaultMode",
			Message:      "Invalid value. Expected one of: \"default\", \"acceptEdits\", \"bypassPermissions\", \"dontAsk\", \"plan\", \"auto\", \"bubble\"",
			Expected:     "default | acceptEdits | bypassPermissions | dontAsk | plan | auto | bubble",
			InvalidValue: setting.DefaultMode,
			Suggestion:   "Use one of: default, acceptEdits, bypassPermissions, dontAsk, plan, auto, bubble",
		})
	}
	if setting.DisableBypassMode != nil && !disableSettingValid(setting.DisableBypassMode) {
		errors = append(errors, ValidationError{
			File:         filePath,
			Path:         "permissions.disableBypassPermissionsMode",
			Message:      `Invalid value. Expected "disable"`,
			Expected:     "disable",
			InvalidValue: setting.DisableBypassMode,
		})
	}
	if setting.DisableAutoMode != nil && !disableSettingValid(setting.DisableAutoMode) {
		errors = append(errors, ValidationError{
			File:         filePath,
			Path:         "permissions.disableAutoMode",
			Message:      `Invalid value. Expected "disable"`,
			Expected:     "disable",
			InvalidValue: setting.DisableAutoMode,
		})
	}
	return errors
}

func validPermissionMode(mode contracts.PermissionMode) bool {
	switch mode {
	case contracts.PermissionDefault,
		contracts.PermissionAcceptEdits,
		contracts.PermissionBypassPermissions,
		contracts.PermissionDontAsk,
		contracts.PermissionPlan,
		contracts.PermissionAuto,
		contracts.PermissionBubble:
		return true
	default:
		return false
	}
}

func disableSettingValid(value any) bool {
	switch v := value.(type) {
	case string:
		return v == "disable"
	default:
		return false
	}
}
