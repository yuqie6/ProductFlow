package providers

import (
	"fmt"
	"regexp"
	"unicode/utf8"
)

func matchJSONSchema(schema map[string]any, value any) error {
	if schema == nil {
		return fmt.Errorf("missing schema")
	}
	if alts, ok := schema["anyOf"].([]any); ok {
		var last error
		for _, raw := range alts {
			alt, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if err := matchJSONSchema(alt, value); err == nil {
				return nil
			} else {
				last = err
			}
		}
		if last != nil {
			return last
		}
		return fmt.Errorf("value does not match anyOf")
	}
	if constVal, ok := schema["const"]; ok {
		if !jsonSchemaEqual(constVal, value) {
			return fmt.Errorf("const mismatch")
		}
	}
	if enum, ok := schema["enum"].([]any); ok {
		matched := false
		for _, item := range enum {
			if jsonSchemaEqual(item, value) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("enum mismatch")
		}
	}
	switch schema["type"] {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok || obj == nil {
			return fmt.Errorf("expected object")
		}
		properties, _ := schema["properties"].(map[string]any)
		if forbid, _ := schema["additionalProperties"].(bool); !forbid {
			for key := range obj {
				if _, ok := properties[key]; !ok {
					return fmt.Errorf("unexpected property %s", key)
				}
			}
		}
		if required, ok := schema["required"].([]any); ok {
			for _, raw := range required {
				key, _ := raw.(string)
				if key == "" {
					continue
				}
				if _, ok := obj[key]; !ok {
					return fmt.Errorf("missing property %s", key)
				}
			}
		}
		for key, raw := range properties {
			child, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			got, exists := obj[key]
			if !exists {
				continue
			}
			if err := matchJSONSchema(child, got); err != nil {
				return err
			}
		}
	case "array":
		arr, ok := value.([]any)
		if !ok {
			return fmt.Errorf("expected array")
		}
		if minItems, ok := jsonSchemaInt(schema["minItems"]); ok && len(arr) < minItems {
			return fmt.Errorf("array too short")
		}
		if maxItems, ok := jsonSchemaInt(schema["maxItems"]); ok && len(arr) > maxItems {
			return fmt.Errorf("array too long")
		}
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for _, item := range arr {
				if err := matchJSONSchema(itemSchema, item); err != nil {
					return err
				}
			}
		}
	case "string":
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected string")
		}
		n := utf8.RuneCountInString(s)
		if minLength, ok := jsonSchemaInt(schema["minLength"]); ok && n < minLength {
			return fmt.Errorf("string too short")
		}
		if maxLength, ok := jsonSchemaInt(schema["maxLength"]); ok && n > maxLength {
			return fmt.Errorf("string too long")
		}
		if pattern, ok := schema["pattern"].(string); ok && pattern != "" {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return err
			}
			if !re.MatchString(s) {
				return fmt.Errorf("string pattern mismatch")
			}
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean")
		}
	case "integer":
		if _, ok := jsonSchemaInt(value); !ok {
			return fmt.Errorf("expected integer")
		}
	case "number":
		n, ok := jsonSchemaFloat(value)
		if !ok {
			return fmt.Errorf("expected number")
		}
		if min, ok := jsonSchemaFloat(schema["exclusiveMinimum"]); ok && !(n > min) {
			return fmt.Errorf("number below exclusiveMinimum")
		}
		if max, ok := jsonSchemaFloat(schema["maximum"]); ok && n > max {
			return fmt.Errorf("number above maximum")
		}
	case "null":
		if value != nil {
			return fmt.Errorf("expected null")
		}
	}
	return nil
}

func jsonSchemaEqual(schemaVal, got any) bool {
	if got == nil {
		return schemaVal == nil
	}
	if a, ok := jsonSchemaFloat(schemaVal); ok {
		b, ok := jsonSchemaFloat(got)
		return ok && a == b
	}
	return schemaVal == got
}

func jsonSchemaFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

func jsonSchemaInt(v any) (int, bool) {
	n, ok := jsonSchemaFloat(v)
	if !ok || n != float64(int(n)) {
		return 0, false
	}
	return int(n), true
}
