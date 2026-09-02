package toolruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// commandRule is one allowlist entry: an argv prefix plus how many total
// argv items the invocation may carry and the deadline granted to it.
type commandRule struct {
	prefix   []string
	maxArgs  int
	deadline time.Duration
}

const (
	commandDeadlineDefault = 10 * time.Second
	commandDeadlineMin     = time.Second
	commandDeadlineMax     = 5 * time.Minute
	commandMaxArgv         = 16
	// toolProgressMaxChunks bounds how many incremental output chunks one
	// command.run invocation may push to the live stream (P1-2). Chunks
	// beyond the cap still land in the final combined result; only the
	// live feed stops, keeping the event pipe flood-safe.
	toolProgressMaxChunks = 40
)

// builtinCommandRules is the fixed observation + reversible-write set. git
// runs only through --no-pager explicit flags (pagers/filters disabled both
// via the flag and the sanitized environment set in runCommand).
//
// The write entries (add / commit / stash) are deliberately NON-destructive:
// they only stage, snapshot or shelve work, all reversible. command.run is a
// mutating tool, so in approval / auto-edit mode these still require an
// explicit per-call user approval ("git write behind confirmation", E2);
// full-access runs them unattended. Destructive git verbs (checkout / reset /
// clean / restore / rm / push) are intentionally NOT here — they can silently
// discard uncommitted work or mutate the remote, so they stay opt-in through
// command-policy.json where the operator accepts that risk explicitly.
func builtinCommandRules() []commandRule {
	return []commandRule{
		{prefix: []string{"go", "version"}, maxArgs: 2, deadline: 10 * time.Second},
		{prefix: []string{"git", "--no-pager", "status"}, maxArgs: 4, deadline: 10 * time.Second},
		{prefix: []string{"git", "--no-pager", "log"}, maxArgs: 8, deadline: 10 * time.Second},
		{prefix: []string{"git", "--no-pager", "diff"}, maxArgs: 6, deadline: 10 * time.Second},
		{prefix: []string{"git", "--no-pager", "show"}, maxArgs: 6, deadline: 10 * time.Second},
		{prefix: []string{"git", "--no-pager", "branch"}, maxArgs: 4, deadline: 10 * time.Second},
		// Reversible writes (E2): gated by the mutating-tool approval flow.
		{prefix: []string{"git", "--no-pager", "add"}, maxArgs: 12, deadline: 15 * time.Second},
		{prefix: []string{"git", "--no-pager", "commit"}, maxArgs: 12, deadline: 20 * time.Second},
		{prefix: []string{"git", "--no-pager", "stash"}, maxArgs: 8, deadline: 15 * time.Second},
	}
}

// loadUserCommandPolicy merges the optional user whitelist file
// (<root>/command-policy.json). A present-but-invalid file fails closed so
// the operator notices instead of running with a half-applied policy.
func (r *Runtime) loadUserCommandPolicy() error {
	raw, err := os.ReadFile(r.userRulesPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Missing file: fail closed. Full-disk is an explicit Settings
			// opt-in, never the implicit default of a fresh data root.
			r.rulesMu.Lock()
			r.fullDisk = false
			r.rulesMu.Unlock()
			return nil
		}
		return err
	}
	userRules, err := buildUserRules(raw)
	if err != nil {
		return err
	}
	var doc userPolicyDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	rules := builtinCommandRules()
	rules = append(rules, userRules...)
	r.rulesMu.Lock()
	r.commandRules = rules
	r.fullDisk = doc.FullAccess
	r.rulesMu.Unlock()
	return nil
}

// userPolicyDoc is the command-policy.json wire shape shared by load, get
// and set.
type userPolicyDoc struct {
	Commands []struct {
		Prefix    []string `json:"prefix"`
		MaxArgs   int      `json:"maxArgs,omitempty"`
		TimeoutMS int64    `json:"timeoutMs,omitempty"`
	} `json:"commands"`
	// FullAccess is the opt-in "full-disk full-access" switch: with it on,
	// full-access mode runs any command and accepts absolute paths on any
	// drive. Off keeps the whitelist plus workspace-root confinement.
	FullAccess bool `json:"fullAccess,omitempty"`
}

// buildUserRules validates one whitelist document and renders it into
// concrete rules without touching runtime state (build-then-swap keeps a
// rejected document from half-applying).
func buildUserRules(raw []byte) ([]commandRule, error) {
	var doc userPolicyDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("command-policy.json: %w", err)
	}
	if len(doc.Commands) > 128 {
		return nil, errors.New("command-policy.json: more than 128 commands")
	}
	rules := make([]commandRule, 0, len(doc.Commands))
	for _, c := range doc.Commands {
		if len(c.Prefix) < 1 || len(c.Prefix) > 8 {
			return nil, errors.New("command-policy.json: prefix must have 1-8 items")
		}
		for _, item := range c.Prefix {
			if item == "" || strings.Contains(item, "..") || strings.HasPrefix(item, "/") || strings.HasPrefix(item, `\`) || len(item) > 2 && item[1] == ':' {
				return nil, errors.New("command-policy.json: invalid prefix item")
			}
		}
		maxArgs := c.MaxArgs
		if maxArgs <= 0 {
			maxArgs = len(c.Prefix)
		}
		if maxArgs > commandMaxArgv {
			maxArgs = commandMaxArgv
		}
		deadline := time.Duration(c.TimeoutMS) * time.Millisecond
		if deadline <= 0 {
			deadline = commandDeadlineDefault
		}
		if deadline > commandDeadlineMax {
			deadline = commandDeadlineMax
		}
		rules = append(rules, commandRule{prefix: c.Prefix, maxArgs: maxArgs, deadline: deadline})
	}
	return rules, nil
}

// CommandPolicyJSON answers the persisted user whitelist document
// ({"commands":[]} when the file does not exist yet).
func (r *Runtime) CommandPolicyJSON() ([]byte, error) {
	raw, err := os.ReadFile(r.userRulesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []byte(`{"commands":[]}`), nil
		}
		return nil, err
	}
	if !json.Valid(raw) {
		return nil, errors.New("command-policy.json: stored document is not valid JSON")
	}
	return raw, nil
}

// SetCommandPolicyJSON validates, atomically persists and hot-applies a
// new user whitelist. An invalid document is refused without touching the
// file or the live rules.
func (r *Runtime) SetCommandPolicyJSON(raw []byte) error {
	if len(raw) > 64<<10 {
		return errors.New("command-policy.json: document exceeds 64 KiB")
	}
	if !json.Valid(raw) {
		return errors.New("command-policy.json: document is not valid JSON")
	}
	userRules, err := buildUserRules(raw)
	if err != nil {
		return err
	}
	var doc userPolicyDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	tmp := r.userRulesPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	// os.Rename on Windows refuses to replace an existing destination.
	_ = os.Remove(r.userRulesPath)
	if err := os.Rename(tmp, r.userRulesPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	rules := builtinCommandRules()
	rules = append(rules, userRules...)
	r.rulesMu.Lock()
	r.commandRules = rules
	r.fullDisk = doc.FullAccess
	r.rulesMu.Unlock()
	return nil
}

// FullDiskEnabled answers whether the user opted into full-disk full-access
// (command-policy.json "fullAccess": true).
func (r *Runtime) FullDiskEnabled() bool {
	r.rulesMu.RLock()
	defer r.rulesMu.RUnlock()
	return r.fullDisk
}

// matchCommandRule finds the first allowlist entry whose prefix matches the
// argv head and whose total length stays within maxArgs. Longer argv lists
// are denied even when the prefix matches, so one entry cannot become a
// wildcard.
func matchCommandRule(rules []commandRule, a []string) (commandRule, bool) {
	if len(a) < 1 || len(a) > commandMaxArgv {
		return commandRule{}, false
	}
	for _, rule := range rules {
		if len(a) > rule.maxArgs || len(a) < len(rule.prefix) {
			continue
		}
		match := true
		for i := range rule.prefix {
			if a[i] != rule.prefix[i] {
				match = false
				break
			}
		}
		if match {
			return rule, true
		}
	}
	return commandRule{}, false
}
