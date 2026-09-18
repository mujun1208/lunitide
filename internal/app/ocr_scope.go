package app

import (
	"encoding/json"
	"strings"
)

func parseOCRPublicScope(payload json.RawMessage) (scopeKind, scopeID string, ok bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(payload, &fields) != nil {
		return "", "", false
	}
	var kind string
	if json.Unmarshal(fields["scopeKind"], &kind) != nil {
		return "", "", false
	}
	switch kind {
	case "user":
		if _, present := fields["scopeId"]; present {
			return "", "", false
		}
		return "user", "", true
	case "project":
		var id string
		if json.Unmarshal(fields["scopeId"], &id) != nil {
			return "", "", false
		}
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 128 {
			return "", "", false
		}
		return "project", id, true
	default:
		return "", "", false
	}
}
