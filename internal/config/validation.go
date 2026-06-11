package config

import (
	"encoding/json"
	"fmt"
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
