package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestSettingsJSONSchemaCoversSettingsFields(t *testing.T) {
	schema := SettingsJSONSchema()
	if schema["$schema"] != SettingsJSONSchemaDraft {
		t.Fatalf("draft = %#v", schema["$schema"])
	}
	if schema["$id"] != SettingsJSONSchemaID {
		t.Fatalf("id = %#v", schema["$id"])
	}
	if schema["additionalProperties"] != true {
		t.Fatalf("additionalProperties = %#v", schema["additionalProperties"])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %#v", schema["properties"])
	}
	names := settingsJSONFieldNames(t)
	for _, name := range names {
		if _, ok := properties[name]; !ok {
			t.Fatalf("schema missing property %q", name)
		}
	}
	if len(properties) != len(names) {
		t.Fatalf("property count = %d want %d", len(properties), len(names))
	}
}

func TestSettingsJSONSchemaIncludesNestedTypesAndEnums(t *testing.T) {
	properties := SettingsJSONSchema()["properties"].(map[string]any)

	schemaProperty(t, properties, "$schema", "const", SettingsJSONSchemaID)
	schemaProperty(t, properties, "env", "type", "object")
	env := properties["env"].(map[string]any)
	envValues := env["additionalProperties"].(map[string]any)
	if envValues["type"] != "string" {
		t.Fatalf("env additionalProperties = %#v", envValues)
	}

	permissions := properties["permissions"].(map[string]any)
	permissionProps := permissions["properties"].(map[string]any)
	defaultMode := permissionProps["defaultMode"].(map[string]any)
	if !stringSliceContains(schemaEnum(defaultMode), string(contracts.PermissionPlan)) ||
		!stringSliceContains(schemaEnum(defaultMode), string(contracts.PermissionBypassPermissions)) {
		t.Fatalf("defaultMode enum = %#v", defaultMode["enum"])
	}

	strictPluginOnly := properties["strictPluginOnlyCustomization"].(map[string]any)
	oneOf := strictPluginOnly["oneOf"].([]any)
	if len(oneOf) != 2 {
		t.Fatalf("strictPluginOnlyCustomization oneOf = %#v", oneOf)
	}
	surfaceItems := oneOf[1].(map[string]any)["items"].(map[string]any)
	if !stringSliceContains(schemaEnum(surfaceItems), CustomizationSurfaceSkills) ||
		!stringSliceContains(schemaEnum(surfaceItems), CustomizationSurfaceAgents) {
		t.Fatalf("strictPluginOnlyCustomization surfaces = %#v", surfaceItems["enum"])
	}

	mcpServers := properties["mcpServers"].(map[string]any)
	serverSchema := mcpServers["additionalProperties"].(map[string]any)
	serverProps := serverSchema["properties"].(map[string]any)
	if serverProps["headers"].(map[string]any)["type"] != "object" ||
		serverProps["oauth"].(map[string]any)["type"] != "object" {
		t.Fatalf("mcp server schema = %#v", serverProps)
	}

	advanced := properties["advanced"].(map[string]any)
	advancedProps := advanced["properties"].(map[string]any)
	if advancedProps["bridge"].(map[string]any)["type"] != "boolean" ||
		advancedProps["computerUse"].(map[string]any)["type"] != "boolean" ||
		advancedProps["tengu_glacier_2xr"].(map[string]any)["type"] != "boolean" {
		t.Fatalf("advanced schema = %#v", advancedProps)
	}
}

func TestSettingsJSONSchemaBytesIsFormattedJSON(t *testing.T) {
	data, err := SettingsJSONSchemaBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\n  \"") {
		t.Fatalf("schema is not indented: %q", string(data[:min(32, len(data))]))
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["title"] != "Claude Code settings" {
		t.Fatalf("decoded schema = %#v", decoded)
	}
}

func settingsJSONFieldNames(t *testing.T) []string {
	t.Helper()
	settings := reflect.TypeOf(contracts.Settings{})
	var names []string
	for i := 0; i < settings.NumField(); i++ {
		field := settings.Field(i)
		name, ok := jsonFieldName(field)
		if !ok {
			continue
		}
		names = append(names, name)
	}
	return names
}

func schemaProperty(t *testing.T, properties map[string]any, property string, key string, want any) {
	t.Helper()
	item, ok := properties[property].(map[string]any)
	if !ok {
		t.Fatalf("%s schema = %#v", property, properties[property])
	}
	if got := item[key]; got != want {
		t.Fatalf("%s.%s = %#v want %#v", property, key, got, want)
	}
}

func schemaEnum(schema map[string]any) []string {
	values, _ := schema["enum"].([]string)
	if values != nil {
		return values
	}
	raw, _ := schema["enum"].([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(string); ok {
			out = append(out, value)
		}
	}
	return out
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
