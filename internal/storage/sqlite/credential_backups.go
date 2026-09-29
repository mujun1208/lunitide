package sqlite

import (
	"encoding/json"
	"fmt"
)

func encodeCredentialBackups(refs []string) (string, error) {
	if len(refs) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(refs)
	if err != nil {
		return "", fmt.Errorf("encode credential backups: %w", err)
	}
	return string(b), nil
}

func decodeCredentialBackups(raw string) ([]string, error) {
	if raw == "" || raw == "[]" {
		return nil, nil
	}
	var refs []string
	if err := json.Unmarshal([]byte(raw), &refs); err != nil {
		return nil, fmt.Errorf("decode credential backups: %w", err)
	}
	return refs, nil
}
