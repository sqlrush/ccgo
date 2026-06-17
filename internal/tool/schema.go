package tool

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
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
	if allOf := schemaList(schema["allOf"]); len(allOf) > 0 {
		for _, childSchema := range allOf {
			if err := validateValue(childSchema, value, path); err != nil {
				return err
			}
		}
	}
	if anyOf := schemaList(schema["anyOf"]); len(anyOf) > 0 {
		if !matchesAtLeastOneSchema(anyOf, value, path) {
			return fmt.Errorf("%s must match at least one allowed schema", path)
		}
	}
	if oneOf := schemaList(schema["oneOf"]); len(oneOf) > 0 {
		matches := countMatchingSchemas(oneOf, value, path)
		if matches != 1 {
			return fmt.Errorf("%s must match exactly one allowed schema", path)
		}
	}
	if notSchema, ok := schemaMap(schema["not"]); ok {
		if err := validateValue(notSchema, value, path); err == nil {
			return fmt.Errorf("%s must not match disallowed schema", path)
		}
	}
	if types := stringOrStrings(schema["type"]); len(types) > 0 {
		if !matchesAnyType(types, value) {
			return fmt.Errorf("%s must be %s", path, strings.Join(types, " or "))
		}
	}
	if constValue, ok := schema["const"]; ok {
		if !equalSchemaValue(value, constValue) {
			return fmt.Errorf("%s must be %s", path, fmt.Sprint(constValue))
		}
	}
	if enumValues, ok := schemaEnumValues(schema["enum"]); ok {
		if !matchesEnumValue(value, enumValues) {
			return fmt.Errorf("%s must be one of %s", path, describeEnumValues(enumValues))
		}
	}
	if minimum, ok := schemaNumberConstraint(schema["minimum"]); ok {
		if number, ok := schemaNumber(value); ok && number < minimum {
			return fmt.Errorf("%s must be at least %s", path, describeSchemaNumber(minimum))
		}
	}
	if exclusiveMinimum, ok := exclusiveNumberConstraint(schema["exclusiveMinimum"], schema["minimum"]); ok {
		if number, ok := schemaNumber(value); ok && number <= exclusiveMinimum {
			return fmt.Errorf("%s must be greater than %s", path, describeSchemaNumber(exclusiveMinimum))
		}
	}
	if maximum, ok := schemaNumberConstraint(schema["maximum"]); ok {
		if number, ok := schemaNumber(value); ok && number > maximum {
			return fmt.Errorf("%s must be at most %s", path, describeSchemaNumber(maximum))
		}
	}
	if exclusiveMaximum, ok := exclusiveNumberConstraint(schema["exclusiveMaximum"], schema["maximum"]); ok {
		if number, ok := schemaNumber(value); ok && number >= exclusiveMaximum {
			return fmt.Errorf("%s must be less than %s", path, describeSchemaNumber(exclusiveMaximum))
		}
	}
	if multiple, ok := schemaNumberConstraint(schema["multipleOf"]); ok {
		if multiple <= 0 {
			return fmt.Errorf("%s has invalid multipleOf %s", path, describeSchemaNumber(multiple))
		}
		if number, ok := schemaNumber(value); ok && !isMultipleOf(number, multiple) {
			return fmt.Errorf("%s must be a multiple of %s", path, describeSchemaNumber(multiple))
		}
	}
	if minLength, ok := intSchemaConstraint(schema["minLength"]); ok {
		text, ok := value.(string)
		if ok && utf8.RuneCountInString(text) < minLength {
			return fmt.Errorf("%s must be at least %d characters", path, minLength)
		}
	}
	if maxLength, ok := intSchemaConstraint(schema["maxLength"]); ok {
		text, ok := value.(string)
		if ok && utf8.RuneCountInString(text) > maxLength {
			return fmt.Errorf("%s must be at most %d characters", path, maxLength)
		}
	}
	if pattern, ok := schema["pattern"].(string); ok && pattern != "" {
		text, ok := value.(string)
		if ok {
			matched, err := regexp.MatchString(pattern, text)
			if err != nil {
				return fmt.Errorf("%s has invalid pattern %q: %w", path, pattern, err)
			}
			if !matched {
				return fmt.Errorf("%s must match pattern %s", path, pattern)
			}
		}
	}
	if minItems, ok := intSchemaConstraint(schema["minItems"]); ok {
		items, ok := value.([]any)
		if ok && len(items) < minItems {
			return fmt.Errorf("%s must contain at least %d items", path, minItems)
		}
	}
	if maxItems, ok := intSchemaConstraint(schema["maxItems"]); ok {
		items, ok := value.([]any)
		if ok && len(items) > maxItems {
			return fmt.Errorf("%s must contain at most %d items", path, maxItems)
		}
	}
	if unique, ok := schema["uniqueItems"].(bool); ok && unique {
		items, ok := value.([]any)
		if ok && !arrayItemsUnique(items) {
			return fmt.Errorf("%s must contain unique items", path)
		}
	}
	if items, ok := value.([]any); ok {
		prefixItems := schemaList(schema["prefixItems"])
		for idx, item := range items {
			if idx >= len(prefixItems) {
				break
			}
			if err := validateValue(prefixItems[idx], item, fmt.Sprintf("%s[%d]", path, idx)); err != nil {
				return err
			}
		}
		if itemsSchema, ok := schemaMap(schema["items"]); ok {
			start := 0
			if len(prefixItems) > 0 {
				start = len(prefixItems)
			}
			for idx := start; idx < len(items); idx++ {
				if err := validateValue(itemsSchema, items[idx], fmt.Sprintf("%s[%d]", path, idx)); err != nil {
					return err
				}
			}
		} else if additional, ok := schema["items"].(bool); ok && !additional {
			start := 0
			if len(prefixItems) > 0 {
				start = len(prefixItems)
			}
			if len(items) > start {
				return fmt.Errorf("%s[%d] is not allowed", path, start)
			}
		}
		if containsSchema, ok := schemaMap(schema["contains"]); ok {
			matches := countMatchingItems(containsSchema, items, path)
			minContains := 1
			if configured, ok := intSchemaConstraint(schema["minContains"]); ok {
				minContains = configured
			}
			if matches < minContains {
				return fmt.Errorf("%s must contain at least %d matching items", path, minContains)
			}
			if maxContains, ok := intSchemaConstraint(schema["maxContains"]); ok && matches > maxContains {
				return fmt.Errorf("%s must contain at most %d matching items", path, maxContains)
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

	properties, ok := objectMap(schema["properties"])
	obj, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	if minProperties, ok := intSchemaConstraint(schema["minProperties"]); ok && len(obj) < minProperties {
		return fmt.Errorf("%s must contain at least %d properties", path, minProperties)
	}
	if maxProperties, ok := intSchemaConstraint(schema["maxProperties"]); ok && len(obj) > maxProperties {
		return fmt.Errorf("%s must contain at most %d properties", path, maxProperties)
	}
	if ok {
		for key, propertySchema := range properties {
			child, ok := obj[key]
			if !ok {
				continue
			}
			propertyMap, ok := schemaMap(propertySchema)
			if !ok {
				continue
			}
			if err := validateValue(propertyMap, child, path+"."+key); err != nil {
				return err
			}
		}
	}
	patternDefined, err := validatePatternProperties(schema["patternProperties"], obj, path)
	if err != nil {
		return err
	}
	if requiredDeps, ok := objectMap(schema["dependentRequired"]); ok {
		for key, requiredValue := range requiredDeps {
			if _, present := obj[key]; !present {
				continue
			}
			for _, requiredKey := range stringSlice(requiredValue) {
				if _, ok := obj[requiredKey]; !ok {
					return fmt.Errorf("%s.%s is required when %s.%s is present", path, requiredKey, path, key)
				}
			}
		}
	}
	if additionalSchema, ok := schemaMap(schema["additionalProperties"]); ok {
		for key, child := range obj {
			if _, defined := properties[key]; defined || patternDefined[key] {
				continue
			}
			if err := validateValue(additionalSchema, child, path+"."+key); err != nil {
				return err
			}
		}
	} else if additional, ok := schema["additionalProperties"].(bool); ok && !additional {
		for key := range obj {
			if _, defined := properties[key]; !defined && !patternDefined[key] {
				return fmt.Errorf("%s.%s is not allowed", path, key)
			}
		}
	}
	return nil
}

func schemaMap(value any) (contracts.JSONSchema, bool) {
	switch typed := value.(type) {
	case contracts.JSONSchema:
		return typed, true
	case map[string]any:
		return contracts.JSONSchema(typed), true
	default:
		return nil, false
	}
}

func objectMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case contracts.JSONSchema:
		return map[string]any(typed), true
	default:
		return nil, false
	}
}

func schemaList(value any) []contracts.JSONSchema {
	switch items := value.(type) {
	case []any:
		out := make([]contracts.JSONSchema, 0, len(items))
		for _, item := range items {
			if child, ok := schemaMap(item); ok {
				out = append(out, child)
			}
		}
		return out
	case []map[string]any:
		out := make([]contracts.JSONSchema, 0, len(items))
		for _, item := range items {
			out = append(out, contracts.JSONSchema(item))
		}
		return out
	case []contracts.JSONSchema:
		return append([]contracts.JSONSchema(nil), items...)
	default:
		return nil
	}
}

func matchesAtLeastOneSchema(schemas []contracts.JSONSchema, value any, path string) bool {
	for _, schema := range schemas {
		if err := validateValue(schema, value, path); err == nil {
			return true
		}
	}
	return false
}

func countMatchingItems(schema contracts.JSONSchema, items []any, path string) int {
	matches := 0
	for idx, item := range items {
		if err := validateValue(schema, item, fmt.Sprintf("%s[%d]", path, idx)); err == nil {
			matches++
		}
	}
	return matches
}

func validatePatternProperties(value any, obj map[string]any, path string) (map[string]bool, error) {
	patterns, ok := objectMap(value)
	if !ok || len(patterns) == 0 {
		return map[string]bool{}, nil
	}
	defined := map[string]bool{}
	for pattern, rawSchema := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("%s has invalid patternProperties pattern %q: %w", path, pattern, err)
		}
		childSchema, ok := schemaMap(rawSchema)
		if !ok {
			continue
		}
		for key, child := range obj {
			if !re.MatchString(key) {
				continue
			}
			defined[key] = true
			if err := validateValue(childSchema, child, path+"."+key); err != nil {
				return nil, err
			}
		}
	}
	return defined, nil
}

func countMatchingSchemas(schemas []contracts.JSONSchema, value any, path string) int {
	matches := 0
	for _, schema := range schemas {
		if err := validateValue(schema, value, path); err == nil {
			matches++
		}
	}
	return matches
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
		if reflect.DeepEqual(a, b) {
			return true
		}
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

func schemaNumberConstraint(value any) (float64, bool) {
	return schemaNumber(value)
}

func isMultipleOf(number float64, multiple float64) bool {
	quotient := number / multiple
	return math.Abs(quotient-math.Round(quotient)) < 1e-9
}

func exclusiveNumberConstraint(value any, pairedLimit any) (float64, bool) {
	switch typed := value.(type) {
	case bool:
		if !typed {
			return 0, false
		}
		return schemaNumberConstraint(pairedLimit)
	default:
		return schemaNumberConstraint(value)
	}
}

func arrayItemsUnique(items []any) bool {
	for i := range items {
		for j := i + 1; j < len(items); j++ {
			if equalSchemaValue(items[i], items[j]) {
				return false
			}
		}
	}
	return true
}

func describeSchemaNumber(value float64) string {
	if math.Trunc(value) == value {
		return fmt.Sprintf("%.0f", value)
	}
	return fmt.Sprintf("%g", value)
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
	switch items := value.(type) {
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), items...)
	default:
		return nil
	}
}

func normalizeRawInput(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}
