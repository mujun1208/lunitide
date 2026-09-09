package skillapp

import (
	"encoding/json"
	"errors"
)

// Creation provenance belongs to the stored skill, not a model's replacement
// manifest. Preserve only this navigation field; package/source replacements
// continue to follow the caller's explicit manifest and validation.
func preserveSkillOrigin(previous, replacement string) (string, error) {
	var next map[string]json.RawMessage
	if json.Unmarshal([]byte(replacement), &next) != nil || next == nil {
		return "", errors.New("skill manifest must be a JSON object")
	}
	var old map[string]json.RawMessage
	_ = json.Unmarshal([]byte(previous), &old)
	delete(next, "originSessionId")
	if source, ok := old["originSessionId"]; ok {
		next["originSessionId"] = source
	}
	data, err := json.Marshal(next)
	if err != nil {
		return "", err
	}
	if len(data) > 65536 {
		return "", errors.New("skill manifest_json size out of bounds after preserving creation origin")
	}
	return string(data), nil
}
