// Package config holds runtime feature flags (M5 T-5.5.4): the frozen
// five-level M5 rollout gauge (internal / 1% / 10% / 50% / 100%), the
// one-way kill switch and hash-based cohort evaluation.
//
// FROZEN (M5): 档位、阈值与 kill-switch 语义已冻结，任何改动需走 ADR。
package config

import (
	"crypto/sha256"
	"errors"
	"sync"
)

// Level is one step of the frozen M5 rollout gauge.
type Level string

// The five frozen rollout levels: internal is whitelist-only, canary1/
// canary10/canary50 are percentage cohorts, ga100 is everyone.
const (
	LevelInternal Level = "internal" // 内部白名单
	LevelCanary1  Level = "canary1"  // 1%
	LevelCanary10 Level = "canary10" // 10%
	LevelCanary50 Level = "canary50" // 50%
	LevelGA100    Level = "ga100"    // 100%
)

// ErrLevelInvalid answers SetM5 (and any level parser) with a value
// outside the frozen gauge.
var ErrLevelInvalid = errors.New("config: invalid rollout level (allowed: internal, canary1, canary10, canary50, ga100)")

// ValidLevel reports whether l is one of the five frozen levels.
func ValidLevel(l Level) bool {
	switch l {
	case LevelInternal, LevelCanary1, LevelCanary10, LevelCanary50, LevelGA100:
		return true
	default:
		return false
	}
}

// InternalChecker reports whether a userID is on the internal (staff)
// whitelist. Production injects its own; the default (nil) admits nobody.
type InternalChecker func(userID string) bool

// Flags is the M5 feature-flag set. All access is mutex-guarded; the kill
// switch is a one-way latch: once tripped, new actions are refused
// product-wide while existing artifacts stay readable and exportable.
type Flags struct {
	mu         sync.RWMutex
	m5         Level
	killSwitch bool
	internal   InternalChecker
}

// NewFlags builds the flag set at defaultLevel. An invalid default is
// coerced to LevelInternal (the most conservative live gauge: whitelist
// only, and nobody whitelisted until a checker is injected).
func NewFlags(defaultLevel Level) *Flags {
	if !ValidLevel(defaultLevel) {
		defaultLevel = LevelInternal
	}
	return &Flags{m5: defaultLevel}
}

// SetInternalChecker injects the internal whitelist (nil = no internals).
func (f *Flags) SetInternalChecker(c InternalChecker) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.internal = c
}

// M5 returns the current rollout level.
func (f *Flags) M5() Level {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.m5
}

// SetM5 moves the rollout gauge. Values outside the frozen five levels
// answer ErrLevelInvalid and leave the gauge untouched.
func (f *Flags) SetM5(l Level) error {
	if !ValidLevel(l) {
		return ErrLevelInvalid
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m5 = l
	return nil
}

// EnableKillSwitch latches the kill switch on. It is deliberately
// one-way: clearing a kill switch is a release decision, not a flag flip.
func (f *Flags) EnableKillSwitch() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killSwitch = true
}

// KillSwitchEnabled reports the kill-switch state.
func (f *Flags) KillSwitchEnabled() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.killSwitch
}

// AllowNewAction reports whether the M5 surface may accept new actions:
// always false while the kill switch is latched; otherwise true for every
// valid gauge level (the level only selects which users Evaluate admits;
// an invalid/empty level behaves as closed).
func (f *Flags) AllowNewAction() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.killSwitch {
		return false
	}
	return ValidLevel(f.m5)
}

// AllowReadExport is always true: the kill switch never blocks reads or
// exports of existing artifacts — 存量产物只读可导出.
func (f *Flags) AllowReadExport() bool { return true }

// IsInternal consults the injected whitelist; without one, nobody is
// internal.
func (f *Flags) IsInternal(userID string) bool {
	f.mu.RLock()
	c := f.internal
	f.mu.RUnlock()
	if c == nil {
		return false
	}
	return c(userID)
}

// Evaluate reports whether userID is inside the M5 cohort at the current
// level. Membership is a pure function of (level, userID, salt): the
// first byte of sha256(userID|salt) decides percentage cohorts, so the
// same user always lands on the same side and gradual widening only adds
// users (a byte below a smaller threshold stays below a bigger one).
//
// FROZEN (M5) thresholds: 1% → first byte < 3, 10% → < 26,
// 50% → < 128, 100% → true, internal → whitelist. 改动需走 ADR。
func (f *Flags) Evaluate(userID, salt string) bool {
	f.mu.RLock()
	level := f.m5
	check := f.internal
	f.mu.RUnlock()

	switch level {
	case LevelGA100:
		return true
	case LevelInternal:
		return check != nil && check(userID)
	}
	if !ValidLevel(level) {
		return false
	}
	sum := sha256.Sum256([]byte(userID + "|" + salt))
	b := sum[0]
	switch level {
	case LevelCanary1:
		return b < 3
	case LevelCanary10:
		return b < 26
	case LevelCanary50:
		return b < 128
	default:
		return false
	}
}
