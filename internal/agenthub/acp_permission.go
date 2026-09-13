package agenthub

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func acpAutoAllowPermission(access, method string, params json.RawMessage) bool {
	if method != "session/request_permission" {
		return false
	}
	if access == "full-access" {
		return true
	}
	if access != "auto-edit" {
		return false
	}
	raw := strings.ToLower(string(params))
	if strings.Contains(raw, "execute") || strings.Contains(raw, "shell") || strings.Contains(raw, "terminal") || strings.Contains(raw, "network") || strings.Contains(raw, "fetch") {
		return false
	}
	return strings.Contains(raw, "edit") || strings.Contains(raw, "write") || strings.Contains(raw, `"file"`) || strings.Contains(raw, "fs")
}

func acpRespondResult(option, text string) json.RawMessage {
	if strings.TrimSpace(text) == "" {
		return json.RawMessage(fmt.Sprintf(`{"outcome":{"outcome":"selected","optionId":%s}}`, strconv.Quote(option)))
	}
	return json.RawMessage(fmt.Sprintf(`{"outcome":{"outcome":"selected","optionId":%s},"text":%s}`, strconv.Quote(option), strconv.Quote(strings.TrimSpace(text))))
}
