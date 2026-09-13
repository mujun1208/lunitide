package agenthub

import (
	"encoding/json"
	"fmt"
	"strings"
)

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
