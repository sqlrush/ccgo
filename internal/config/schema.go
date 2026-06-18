package config

import (
	"encoding/json"
	"reflect"
	"strings"

	"ccgo/internal/contracts"
)

const (
	SettingsJSONSchemaDraft = "https://json-schema.org/draft/2020-12/schema"
	SettingsJSONSchemaID    = "https://json.schemastore.org/claude-code-settings.json"
)

var (
	settingsType       = reflect.TypeOf(contracts.Settings{})
	permissionModeType = reflect.TypeOf(contracts.PermissionMode(""))
	anyType            = reflect.TypeOf((*any)(nil)).Elem()
)

func SettingsJSONSchema() map[string]any {
	schema := schemaForType(settingsType, nil)
	schema["$schema"] = SettingsJSONSchemaDraft
	schema["$id"] = SettingsJSONSchemaID
	schema["title"] = "Claude Code settings"
	schema["description"] = "Generated from ccgo contracts.Settings."
	schema["additionalProperties"] = true
	return schema
}

func SettingsJSONSchemaBytes() ([]byte, error) {
	return json.MarshalIndent(SettingsJSONSchema(), "", "  ")
}

func schemaForType(t reflect.Type, seen map[reflect.Type]bool) map[string]any {
	if t == nil {
		return map[string]any{}
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == anyType {
		return map[string]any{}
	}
	if t == permissionModeType {
		return map[string]any{
			"type": "string",
			"enum": []string{
				string(contracts.PermissionDefault),
				string(contracts.PermissionAcceptEdits),
				string(contracts.PermissionBypassPermissions),
				string(contracts.PermissionDontAsk),
				string(contracts.PermissionPlan),
				string(contracts.PermissionAuto),
				string(contracts.PermissionBubble),
			},
		}
	}

	switch t.Kind() {
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Slice, reflect.Array:
		return map[string]any{
			"type":  "array",
			"items": schemaForType(t.Elem(), seen),
		}
	case reflect.Map:
		schema := map[string]any{"type": "object"}
		if t.Key().Kind() != reflect.String {
			schema["additionalProperties"] = true
			return schema
		}
		valueSchema := schemaForType(t.Elem(), seen)
		if len(valueSchema) == 0 {
			schema["additionalProperties"] = true
		} else {
			schema["additionalProperties"] = valueSchema
		}
		return schema
	case reflect.Struct:
		if seen == nil {
			seen = map[reflect.Type]bool{}
		}
		if seen[t] {
			return map[string]any{}
		}
		seen[t] = true
		properties := map[string]any{}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name, ok := jsonFieldName(field)
			if !ok {
				continue
			}
			if override, ok := schemaFieldOverride(t, name); ok {
				properties[name] = override
				continue
			}
			properties[name] = schemaForType(field.Type, seen)
		}
		delete(seen, t)
		return map[string]any{
			"type":                 "object",
			"properties":           properties,
			"additionalProperties": false,
		}
	case reflect.Interface:
		return map[string]any{}
	default:
		return map[string]any{}
	}
}

func jsonFieldName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	name := strings.TrimSpace(strings.Split(tag, ",")[0])
	if name == "" {
		name = field.Name
	}
	return name, true
}

func schemaFieldOverride(parent reflect.Type, name string) (map[string]any, bool) {
	if parent == settingsType {
		switch name {
		case "$schema":
			return map[string]any{
				"type":  "string",
				"const": SettingsJSONSchemaID,
			}, true
		case "strictPluginOnlyCustomization":
			return map[string]any{
				"oneOf": []any{
					map[string]any{"type": "boolean"},
					map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "string",
							"enum": []string{
								CustomizationSurfaceSkills,
								CustomizationSurfaceAgents,
								CustomizationSurfaceHooks,
								CustomizationSurfaceMCP,
							},
						},
					},
				},
			}, true
		case "forceLoginMethod":
			return map[string]any{
				"type": "string",
				"enum": []string{"claudeai", "console"},
			}, true
		}
	}
	if parent == reflect.TypeOf(contracts.CommandSetting{}) && name == "type" {
		return map[string]any{
			"type": "string",
			"enum": []string{"command"},
		}, true
	}
	return nil, false
}
