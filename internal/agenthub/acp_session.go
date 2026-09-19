package agenthub

import (
	"encoding/json"
	"fmt"
	"strings"
)

func acpAuthMethodID(init *acpRPC) string {
	if init == nil || len(init.Result) == 0 {
		return ""
	}
	var payload struct {
		AuthMethods []struct {
			ID string `json:"id"`
		} `json:"authMethods"`
	}
	if json.Unmarshal(init.Result, &payload) != nil {
		return ""
	}
	preferred := ""
	for _, method := range payload.AuthMethods {
		id := strings.TrimSpace(method.ID)
		if id == "" {
			continue
		}
		low := strings.ToLower(id)
		if strings.Contains(low, "cursor") || strings.Contains(low, "login") || strings.Contains(low, "agent") {
			return id
		}
		if preferred == "" {
			preferred = id
		}
	}
	return preferred
}

func acpAuthenticateIfNeeded(call func(string, any) (*acpRPC, error), init *acpRPC) error {
	methodID := acpAuthMethodID(init)
	if methodID == "" {
		return nil
	}
	_, err := call("authenticate", map[string]any{"methodId": methodID})
	return err
}

func keepNativeID(returned, stored string) string {
	if id := strings.TrimSpace(returned); id != "" {
		return id
	}
	return strings.TrimSpace(stored)
}

func acpOpenNativeSession(call func(string, any) (*acpRPC, error), cwd, nativeID string) (string, error) {
	params := map[string]any{"cwd": cwd, "mcpServers": []any{}}
	var resp *acpRPC
	var err error
	usedLoad := false
	if nativeID != "" {
		load := map[string]any{"cwd": cwd, "mcpServers": []any{}, "sessionId": nativeID}
		resp, err = call("session/load", load)
		if err != nil {
			resp, err = call("session/new", params)
		} else {
			usedLoad = true
		}
	} else {
		resp, err = call("session/new", params)
	}
	if err != nil {
		return "", err
	}
	var created struct {
		SessionID string `json:"sessionId"`
	}
	if resp != nil {
		_ = json.Unmarshal(resp.Result, &created)
	}
	if usedLoad {
		return keepNativeID(created.SessionID, nativeID), nil
	}
	if created.SessionID == "" {
		return "", fmt.Errorf("session/new 未返回 sessionId")
	}
	return created.SessionID, nil
}
