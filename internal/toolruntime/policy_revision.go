package toolruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/lunitide/lunitide/internal/workspace"
	"github.com/oklog/ulid/v2"
)

var ErrPolicyRevisionConflict = errors.New("设置已被修改，请读取最新版本并保留当前草稿后再保存")

func readPolicyDocument(path string, fallback []byte) ([]byte, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return fallback, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 64<<10 {
		return nil, errors.New("policy exceeds 64 KiB")
	}
	if !json.Valid(raw) {
		return nil, errors.New("stored policy is not valid JSON")
	}
	return raw, nil
}

type PolicyStatus struct {
	Revision        string `json:"revision"`
	AppliedRevision string `json:"appliedRevision"`
	State           string `json:"state"`
}

func policyRevision(raw []byte) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	canonical, _ := json.Marshal(value)
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}
func (r *Runtime) policyRaw(kind string) ([]byte, error) {
	if kind == "commands" {
		return r.CommandPolicyJSON()
	}
	return r.HooksPolicyJSON()
}
func (r *Runtime) policyStatus(kind string, raw []byte) PolicyStatus {
	revision := policyRevision(raw)
	applied := r.policyApplied[kind]
	state := "pending"
	if revision == applied {
		state = "applied"
	}
	return PolicyStatus{Revision: revision, AppliedRevision: applied, State: state}
}
func (r *Runtime) PolicySnapshot(kind string) (json.RawMessage, error) {
	r.policyMu.Lock()
	defer r.policyMu.Unlock()
	raw, err := r.policyRaw(kind)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err = json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, errors.New("stored policy is not an object")
	}
	delete(doc, "revisionToken")
	status := r.policyStatus(kind, raw)
	doc["revision"] = status.Revision
	doc["appliedRevision"] = status.AppliedRevision
	doc["state"] = status.State
	out, err := json.Marshal(doc)
	return out, err
}
func (r *Runtime) rememberAppliedPolicy(kind string, raw []byte) {
	if r.policyApplied == nil {
		r.policyApplied = map[string]string{}
	}
	r.policyApplied[kind] = policyRevision(raw)
}

// SetPolicyVersioned shares the exact validation and activation path used by
// local trusted callers. The renderer must supply its observed revision.
func (r *Runtime) SetPolicyVersioned(kind string, raw []byte, expected string) (PolicyStatus, error) {
	if expected == "" {
		return PolicyStatus{}, ErrPolicyRevisionConflict
	}
	return r.commitPolicy(kind, raw, &expected)
}
func (r *Runtime) commitPolicy(kind string, raw []byte, expected *string) (PolicyStatus, error) {
	r.policyMu.Lock()
	defer r.policyMu.Unlock()
	if kind != "commands" && kind != "hooks" {
		return PolicyStatus{}, errors.New("unknown policy")
	}
	if len(raw) > 64<<10 {
		return PolicyStatus{}, errors.New("policy exceeds 64 KiB")
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil || doc == nil {
		return PolicyStatus{}, errors.New("invalid policy document")
	}
	delete(doc, "expectedRevision")
	delete(doc, "revision")
	delete(doc, "appliedRevision")
	delete(doc, "state")
	current, err := r.policyRaw(kind)
	if err != nil {
		return PolicyStatus{}, err
	}
	if expected != nil && *expected != policyRevision(current) {
		return PolicyStatus{}, ErrPolicyRevisionConflict
	}
	token, _ := json.Marshal(ulid.Make().String())
	doc["revisionToken"] = token
	persisted, err := json.Marshal(doc)
	if err != nil {
		return PolicyStatus{}, err
	}
	var apply func()
	path := r.hooksRulesPath
	if kind == "commands" {
		rules, err := buildUserRules(persisted)
		if err != nil {
			return PolicyStatus{}, err
		}
		var parsed userPolicyDoc
		if err = json.Unmarshal(persisted, &parsed); err != nil {
			return PolicyStatus{}, err
		}
		allRules := append(builtinCommandRules(), rules...)
		apply = func() {
			r.rulesMu.Lock()
			r.commandRules = allRules
			r.fullDisk = parsed.FullAccess
			r.rulesMu.Unlock()
		}
		path = r.userRulesPath
	} else {
		rules, err := buildHookRules(persisted)
		if err != nil {
			return PolicyStatus{}, err
		}
		apply = func() { r.hooksMu.Lock(); r.hookRules = rules; r.hooksMu.Unlock() }
	}
	root, err := workspace.NewSecureRoot(r.root)
	if err != nil {
		return PolicyStatus{}, err
	}
	if err = root.WriteAtomic(filepath.Base(path), persisted, 0600); err != nil {
		return PolicyStatus{}, err
	}
	apply()
	r.rememberAppliedPolicy(kind, persisted)
	return r.policyStatus(kind, persisted), nil
}
