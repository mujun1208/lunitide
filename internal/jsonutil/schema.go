package jsonutil

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Validate checks args against a JSON Schema object (draft-ish subset:
// type, properties, required, enum, additionalProperties). Empty schema
// is a no-op. Errors are phrased so a model can retry the tool call.
func Validate(schema, args json.RawMessage) error {
	if len(bytesTrim(schema)) == 0 || string(bytesTrim(schema)) == "null" {
		return nil
	}
	var spec map[string]any
	if err := json.Unmarshal(schema, &spec); err != nil {
		return nil
	}
	repaired := Repair(args)
	var value any
	if err := json.Unmarshal(repaired, &value); err != nil {
		return fmt.Errorf("arguments are not valid JSON; resend a JSON object matching the tool schema")
	}
	return validateValue("", spec, value)
}

func bytesTrim(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func validateValue(path string, schema map[string]any, value any) error {
	typ, _ := schema["type"].(string)
	if typ != "" && !typeOK(typ, value) {
		return fmt.Errorf("%s must be %s", loc(path), typ)
	}
	if enums, ok := schema["enum"].([]any); ok && len(enums) > 0 {
		if !enumOK(enums, value) {
			return fmt.Errorf("%s must be one of %s", loc(path), joinEnums(enums))
		}
	}
	if typ == "object" || (typ == "" && isMap(value)) {
		obj, ok := value.(map[string]any)
		if !ok {
			if value == nil {
				obj = map[string]any{}
			} else {
				return fmt.Errorf("%s must be an object", loc(path))
			}
		}
		props, _ := schema["properties"].(map[string]any)
		if req, ok := schema["required"].([]any); ok {
			for _, r := range req {
				key, _ := r.(string)
				if key == "" {
					continue
				}
				if _, exists := obj[key]; !exists {
					return fmt.Errorf("missing required property %q; add it and retry", qualify(path, key))
				}
			}
		}
		additional := true
		if v, ok := schema["additionalProperties"]; ok {
			if b, isBool := v.(bool); isBool {
				additional = b
			}
		}
		for k, child := range obj {
			ps, _ := props[k].(map[string]any)
			if ps == nil {
				if !additional {
					return fmt.Errorf("unknown property %q; remove it and retry", qualify(path, k))
				}
				continue
			}
			if err := validateValue(qualify(path, k), ps, child); err != nil {
				return err
			}
		}
	}
	if typ == "array" {
		arr, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", loc(path))
		}
		if n, ok := asInt(schema["minItems"]); ok && len(arr) < n {
			return fmt.Errorf("%s needs at least %d items", loc(path), n)
		}
		if n, ok := asInt(schema["maxItems"]); ok && len(arr) > n {
			return fmt.Errorf("%s allows at most %d items", loc(path), n)
		}
		if itemSpec, ok := schema["items"].(map[string]any); ok {
			for i, item := range arr {
				if err := validateValue(fmt.Sprintf("%s[%d]", loc(path), i), itemSpec, item); err != nil {
					return err
				}
			}
		}
	}
	if typ == "string" {
		s, _ := value.(string)
		if n, ok := asInt(schema["minLength"]); ok && len([]rune(s)) < n {
			return fmt.Errorf("%s is too short", loc(path))
		}
		if n, ok := asInt(schema["maxLength"]); ok && len([]rune(s)) > n {
			return fmt.Errorf("%s is too long", loc(path))
		}
	}
	if typ == "integer" || typ == "number" {
		n, ok := asFloat(value)
		if ok {
			if m, ok := asFloat(schema["minimum"]); ok && n < m {
				return fmt.Errorf("%s must be >= %v", loc(path), schema["minimum"])
			}
			if m, ok := asFloat(schema["maximum"]); ok && n > m {
				return fmt.Errorf("%s must be <= %v", loc(path), schema["maximum"])
			}
		}
	}
	return nil
}

func typeOK(typ string, value any) bool {
	if value == nil {
		return typ == "null"
	}
	switch typ {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "integer":
		switch n := value.(type) {
		case float64:
			return n == float64(int64(n))
		case json.Number:
			_, err := n.Int64()
			return err == nil
		default:
			return false
		}
	case "number":
		switch value.(type) {
		case float64, json.Number:
			return true
		default:
			return false
		}
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	default:
		return true
	}
}

func enumOK(enums []any, value any) bool {
	got, _ := json.Marshal(value)
	for _, e := range enums {
		want, _ := json.Marshal(e)
		if string(got) == string(want) {
			return true
		}
	}
	return false
}

func joinEnums(enums []any) string {
	parts := make([]string, 0, len(enums))
	for _, e := range enums {
		b, _ := json.Marshal(e)
		parts = append(parts, string(b))
	}
	return strings.Join(parts, ", ")
}

func isMap(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

func asInt(v any) (int, bool) {
	n, ok := asFloat(v)
	if !ok {
		return 0, false
	}
	return int(n), true
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func loc(path string) string {
	if path == "" {
		return "arguments"
	}
	return path
}

func qualify(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// RetryMessage formats a tool-result the model can use to correct itself
// (Hermes-style: feed the error back instead of aborting the turn).
func RetryMessage(tool, detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		detail = "arguments did not match the tool schema"
	}
	return "ok:false\nretry: fix the " + tool + " arguments and call the tool again. " + detail
}
