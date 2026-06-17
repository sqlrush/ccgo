package tool

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"ccgo/internal/contracts"
)

func ValidateSchema(schema contracts.JSONSchema, raw json.RawMessage) error {
	if len(schema) == 0 {
		return nil
	}
	raw = normalizeRawInput(raw)
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	return validateValue(schema, value, "input")
}

func validateValue(schema contracts.JSONSchema, value any, path string) error {
	if types := stringOrStrings(schema["type"]); len(types) > 0 {
		if !matchesAnyType(types, value) {
			return fmt.Errorf("%s must be %s", path, strings.Join(types, " or "))
		}
	}
	if enumValues, ok := schemaEnumValues(schema["enum"]); ok {
		if !matchesEnumValue(value, enumValues) {
			return fmt.Errorf("%s must be one of %s", path, describeEnumValues(enumValues))
		}
	}
	if minLength, ok := intSchemaConstraint(schema["minLength"]); ok {
		text, ok := value.(string)
		if ok && utf8.RuneCountInString(text) < minLength {
			return fmt.Errorf("%s must be at least %d characters", path, minLength)
		}
	}
	if itemsSchema, ok := schema["items"].(map[string]any); ok {
		items, ok := value.([]any)
		if ok {
			for idx, item := range items {
				if err := validateValue(contracts.JSONSchema(itemsSchema), item, fmt.Sprintf("%s[%d]", path, idx)); err != nil {
					return err
				}
			}
		}
	}

	if required := stringSlice(schema["required"]); len(required) > 0 {
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be object to check required fields", path)
		}
		for _, key := range required {
			if _, ok := obj[key]; !ok {
				return fmt.Errorf("%s.%s is required", path, key)
			}
		}
	}

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	for key, propertySchema := range properties {
		child, ok := obj[key]
		if !ok {
			continue
		}
		propertyMap, ok := propertySchema.(map[string]any)
		if !ok {
			continue
		}
		if err := validateValue(contracts.JSONSchema(propertyMap), child, path+"."+key); err != nil {
			return err
		}
	}
	return nil
}

func matchesAnyType(types []string, value any) bool {
	for _, typ := range types {
		switch typ {
		case "object":
			if _, ok := value.(map[string]any); ok {
				return true
			}
		case "array":
			if _, ok := value.([]any); ok {
				return true
			}
		case "string":
			if _, ok := value.(string); ok {
				return true
			}
		case "boolean":
			if _, ok := value.(bool); ok {
				return true
			}
		case "number":
			if _, ok := value.(float64); ok {
				return true
			}
		case "integer":
			if n, ok := value.(float64); ok && math.Trunc(n) == n {
				return true
			}
		case "null":
			if value == nil {
				return true
			}
		}
	}
	return false
}

func schemaEnumValues(value any) ([]any, bool) {
	switch v := value.(type) {
	case []any:
		return v, len(v) > 0
	case []string:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out, len(out) > 0
	default:
		return nil, false
	}
}

func matchesEnumValue(value any, enumValues []any) bool {
	for _, candidate := range enumValues {
		if equalSchemaValue(value, candidate) {
			return true
		}
	}
	return false
}

func equalSchemaValue(a any, b any) bool {
	if af, ok := schemaNumber(a); ok {
		if bf, ok := schemaNumber(b); ok {
			return af == bf
		}
	}
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case nil:
		return b == nil
	default:
		return fmt.Sprint(a) == fmt.Sprint(b)
	}
}

func describeEnumValues(enumValues []any) string {
	parts := make([]string, 0, len(enumValues))
	for _, value := range enumValues {
		parts = append(parts, fmt.Sprint(value))
	}
	return strings.Join(parts, ", ")
}

func schemaNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func intSchemaConstraint(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		if math.Trunc(v) == v {
			return int(v), true
		}
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i), true
		}
	}
	return 0, false
}

func stringOrStrings(value any) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), v...)
	default:
		return nil
	}
}

func stringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func normalizeRawInput(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}
