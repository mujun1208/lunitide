package app

import "github.com/lunitide/lunitide/internal/bridge"

func nonemptyPeopleRequestKey(r bridge.Request) string {
	if r.IdempotencyKey != "" {
		return r.IdempotencyKey
	}
	// Legacy clients still receive transport-level replay protection.
	return r.ID
}
