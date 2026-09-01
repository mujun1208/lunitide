package app

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// pluginConfirmTTL bounds the lifetime of a plugin.uninstall confirmation
// nonce. The token is issued by plugin.confirmToken and must be presented to
// plugin.uninstall within this window; expired tokens are swept on access.
const pluginConfirmTTL = 2 * time.Minute

type pluginConfirmEntry struct {
	installID string
	expiresAt time.Time
}

// pluginConfirmVault is a process-local, single-use nonce store for the
// plugin.uninstall handshake. Before W6 the frontend derived the confirm
// token from a public formula (sha256("plugin.uninstall|"+installId)), so any
// caller could forge or replay it and the backend accepted it as long as it
// was valid hex. The vault replaces that with a server-issued 256-bit random
// nonce that is bound to one installId, expires, and is consumed on first use
// — mirroring the mc.confirm.token guarantee without a schema migration. The
// zero value is ready to use.
type pluginConfirmVault struct {
	mu     sync.Mutex
	tokens map[string]pluginConfirmEntry
}

// issue mints a fresh single-use token bound to installID and returns it with
// its expiry. Expired entries are swept opportunistically so the map cannot
// grow without bound under repeated issue-without-consume.
func (v *pluginConfirmVault) issue(installID string, now time.Time) (string, time.Time, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", time.Time{}, err
	}
	token := hex.EncodeToString(raw[:])
	expires := now.Add(pluginConfirmTTL)
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.tokens == nil {
		v.tokens = make(map[string]pluginConfirmEntry)
	}
	for k, e := range v.tokens {
		if !e.expiresAt.After(now) {
			delete(v.tokens, k)
		}
	}
	v.tokens[token] = pluginConfirmEntry{installID: installID, expiresAt: expires}
	return token, expires, nil
}

// consume validates and removes the token in one step. It returns true only if
// the token exists, is bound to installID and has not expired; the entry is
// always deleted so a token can never be replayed, whether or not it matched.
func (v *pluginConfirmVault) consume(token, installID string, now time.Time) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.tokens == nil {
		return false
	}
	e, ok := v.tokens[token]
	if !ok {
		return false
	}
	delete(v.tokens, token)
	return e.installID == installID && e.expiresAt.After(now)
}
