package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// delegationMode controls whether chat exposes subagent.spawn/join and how
// aggressively the system prompt nudges parallel read-only delegation.
type delegationMode string

const (
	delegationDisabled  delegationMode = "disabled"
	delegationExplicit  delegationMode = "explicit"
	delegationProactive delegationMode = "proactive"
)

func delegationModeValid(m delegationMode) bool {
	switch m {
	case delegationDisabled, delegationExplicit, delegationProactive:
		return true
	default:
		return false
	}
}

// subagentProfileDef is one spawn profile (built-in or user-defined).
type subagentProfileDef struct {
	ID           string   `json:"id"`
	DisplayName  string   `json:"displayName"`
	Description  string   `json:"description"`
	SystemPrompt string   `json:"systemPrompt"`
	ReadCaps     []string `json:"readCaps"`
	MaxSteps     int      `json:"maxSteps"`
	BudgetTokens int64    `json:"budgetTokens"`
	Builtin      bool     `json:"builtin,omitempty"`
}

type subagentProfileOverride struct {
	Enabled    *bool  `json:"enabled,omitempty"`
	ProviderID string `json:"providerId,omitempty"`
	ModelID    string `json:"modelId,omitempty"`
}

// subagentChatPolicy is the per-stream subagent configuration from chat.start.
type subagentChatPolicy struct {
	DelegationMode delegationMode                       `json:"delegationMode"`
	Overrides      map[string]subagentProfileOverride   `json:"overrides,omitempty"`
	CustomProfiles []subagentProfileDef                 `json:"customProfiles,omitempty"`
}

func defaultSubagentChatPolicy() subagentChatPolicy {
	return subagentChatPolicy{DelegationMode: delegationProactive}
}

func parseSubagentChatPolicy(raw json.RawMessage) subagentChatPolicy {
	policy := defaultSubagentChatPolicy()
	if len(raw) == 0 || string(raw) == "null" {
		return policy
	}
	var p struct {
		DelegationMode string                          `json:"delegationMode"`
		Overrides      map[string]subagentProfileOverride `json:"overrides"`
		CustomProfiles []subagentProfileDef            `json:"customProfiles"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return policy
	}
	switch delegationMode(p.DelegationMode) {
	case delegationDisabled, delegationExplicit, delegationProactive:
		policy.DelegationMode = delegationMode(p.DelegationMode)
	}
	if len(p.Overrides) > 0 {
		policy.Overrides = p.Overrides
	}
	if len(p.CustomProfiles) > 0 {
		if len(p.CustomProfiles) > 16 {
			p.CustomProfiles = p.CustomProfiles[:16]
		}
		policy.CustomProfiles = sanitizeCustomProfiles(p.CustomProfiles)
	}
	return policy
}

func sanitizeCustomProfiles(in []subagentProfileDef) []subagentProfileDef {
	out := make([]subagentProfileDef, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, p := range in {
		id := strings.TrimSpace(p.ID)
		if id == "" || len(id) > 64 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		if _, ok := builtinSubagentProfiles()[id]; ok {
			continue
		}
		name := strings.TrimSpace(p.DisplayName)
		if name == "" {
			name = id
		}
		prompt := strings.TrimSpace(p.SystemPrompt)
		if prompt == "" {
			prompt = "You are a read-only subagent. Investigate the assigned purpose and return one concise report."
		}
		if len(prompt) > 4000 {
			prompt = prompt[:4000]
		}
		caps := sanitizeProfileCaps(p.ReadCaps)
		if len(caps) == 0 {
			caps = append([]string(nil), defaultSubagentProfileCaps()...)
		}
		maxSteps := p.MaxSteps
		if maxSteps < 1 {
			maxSteps = subagentMaxSteps
		}
		if maxSteps > 8 {
			maxSteps = 8
		}
		budget := p.BudgetTokens
		if budget < 1000 {
			budget = subagentDefaultBudgetTokens
		}
		if budget > 50000 {
			budget = 50000
		}
		seen[id] = struct{}{}
		out = append(out, subagentProfileDef{
			ID: id, DisplayName: name, Description: strings.TrimSpace(p.Description),
			SystemPrompt: prompt, ReadCaps: caps, MaxSteps: maxSteps, BudgetTokens: budget,
		})
	}
	return out
}

func sanitizeProfileCaps(caps []string) []string {
	if len(caps) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(caps))
	out := make([]string, 0, len(caps))
	for _, cap := range caps {
		cap = strings.TrimSpace(cap)
		if cap == "" {
			continue
		}
		if _, ok := seen[cap]; ok {
			continue
		}
		seen[cap] = struct{}{}
		out = append(out, cap)
	}
	sort.Strings(out)
	return out
}

func defaultSubagentProfileCaps() []string {
	return []string{"fs.read", "fs.tree", "fs.grep", "fs.glob", "web.fetch", "web.search"}
}

func builtinSubagentProfiles() map[string]subagentProfileDef {
	return map[string]subagentProfileDef{
		"explore": {
			ID: "explore", DisplayName: "Explore", Builtin: true,
			Description: "Read-only codebase search agent for broad fan-out lookups.",
			SystemPrompt: "You are the Explore subagent: search and read the workspace read-only. Use listing, glob, grep and file reads. Return one concise report with paths and findings (max 2000 characters). Do not write files or run mutating commands.",
			ReadCaps: []string{"fs.read", "fs.readMany", "fs.glob", "fs.grep", "fs.tree", "fs.stat"},
			MaxSteps: 4, BudgetTokens: subagentDefaultBudgetTokens,
		},
		"research": {
			ID: "research", DisplayName: "Research", Builtin: true,
			Description: "Web search and page fetch for market or documentation research.",
			SystemPrompt: "You are the Research subagent: use web.search and web.fetch only. Summarize sources with URLs in one report (max 2000 characters). Do not invent sources.",
			ReadCaps: []string{"web.search", "web.fetch"},
			MaxSteps: 4, BudgetTokens: subagentDefaultBudgetTokens,
		},
		"general-purpose": {
			ID: "general-purpose", DisplayName: "General purpose", Builtin: true,
			Description: "General-purpose agent for multi-step read-only investigation.",
			SystemPrompt: "You are a general-purpose read-only subagent. Investigate using workspace reads, allowlisted commands, and web tools as needed. Return one concise report (max 2000 characters).",
			ReadCaps: defaultSubagentProfileCaps(),
			MaxSteps: 4, BudgetTokens: subagentDefaultBudgetTokens,
		},
		"review": {
			ID: "review", DisplayName: "Review", Builtin: true,
			Description: "Structured code/doc review with path:line references.",
			SystemPrompt: "You are the Review subagent: read-only code and doc review. Report findings as 严重/建议 with file paths; cite path:line when possible. Max 2000 characters.",
			ReadCaps: []string{"fs.read", "fs.readMany", "fs.grep", "fs.tree", "fs.stat"},
			MaxSteps: 4, BudgetTokens: subagentDefaultBudgetTokens,
		},
		"browser": {
			ID: "browser", DisplayName: "Browser", Builtin: true,
			Description: "Navigate and read public pages through the restricted browser channel.",
			SystemPrompt: "You are the Browser subagent: use browser.act navigate/read and web.fetch for public pages. Filter noise; return only relevant excerpts and URLs (max 2000 characters).",
			ReadCaps: []string{"web.fetch", "web.search", "browser.act:navigate", "browser.act:read", "browser.act:snapshot"},
			MaxSteps: 4, BudgetTokens: subagentDefaultBudgetTokens,
		},
		"shell": {
			ID: "shell", DisplayName: "Shell", Builtin: true,
			Description: "Runs read-only shell commands; verbose output stays in the subagent.",
			SystemPrompt: "You are the Shell subagent: run only read-only allowlisted commands (git status/diff/log, go test/vet, etc.). Summarize command output for the parent; max 2000 characters.",
			ReadCaps: []string{"fs.read", "fs.tree"},
			MaxSteps: 3, BudgetTokens: 6144,
		},
		"writer": {
			ID: "writer", DisplayName: "Writer", Builtin: true,
			Description: "Drafts outlines and prose in the report (no direct file writes).",
			SystemPrompt: "You are the Writer subagent: read context read-only and draft structured prose, outlines, or document sections in your final report. Do not write files; max 2000 characters.",
			ReadCaps: []string{"fs.read", "fs.tree", "web.fetch", "web.search"},
			MaxSteps: 3, BudgetTokens: subagentDefaultBudgetTokens,
		},
		"test": {
			ID: "test", DisplayName: "Test", Builtin: true,
			Description: "Finds test gaps and suggests cases using read-only tools.",
			SystemPrompt: "You are the Test subagent: read code read-only, infer test gaps, suggest cases and commands. Max 2000 characters.",
			ReadCaps: []string{"fs.read", "fs.grep", "fs.glob", "fs.tree"},
			MaxSteps: 4, BudgetTokens: subagentDefaultBudgetTokens,
		},
	}
}

func mergeSubagentProfiles(policy subagentChatPolicy) map[string]subagentProfileDef {
	out := builtinSubagentProfiles()
	for _, custom := range policy.CustomProfiles {
		out[custom.ID] = custom
	}
	return out
}

func resolveSubagentProfile(policy subagentChatPolicy, profileID string) (subagentProfileDef, subagentProfileOverride, bool) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		profileID = "general-purpose"
	}
	profiles := mergeSubagentProfiles(policy)
	def, ok := profiles[profileID]
	if !ok {
		def = profiles["general-purpose"]
		profileID = "general-purpose"
		ok = true
	}
	ov := policy.Overrides[profileID]
	if ov.Enabled != nil && !*ov.Enabled {
		return subagentProfileDef{}, ov, false
	}
	caps := sanitizeProfileCaps(def.ReadCaps)
	filtered := make([]string, 0, len(caps))
	for _, cap := range caps {
		if m7CapAllowedForSpawn(cap) {
			filtered = append(filtered, cap)
		}
	}
	if len(filtered) == 0 {
		filtered = append([]string(nil), defaultSubagentProfileCaps()...)
	}
	def.ReadCaps = filtered
	if def.MaxSteps < 1 {
		def.MaxSteps = subagentMaxSteps
	}
	if def.BudgetTokens < 1000 {
		def.BudgetTokens = subagentDefaultBudgetTokens
	}
	return def, ov, ok
}

func m7CapAllowedForSpawn(cap string) bool {
	// chat-spawned subagents use the same frozen whitelist as M7 bridge spawn.
	for _, allowed := range []string{
		"fs.tree", "fs.stat", "fs.read", "fs.readMany", "fs.glob", "fs.grep",
		"web.fetch", "web.search", "evidence.list",
		"browser.act:navigate", "browser.act:read", "browser.act:snapshot",
	} {
		if cap == allowed {
			return true
		}
	}
	return false
}

func subagentPersonaDigest(def subagentProfileDef) string {
	body, _ := json.Marshal(struct {
		ID           string   `json:"id"`
		SystemPrompt string   `json:"systemPrompt"`
		ReadCaps     []string `json:"readCaps"`
		MaxSteps     int      `json:"maxSteps"`
		BudgetTokens int64    `json:"budgetTokens"`
	}{def.ID, def.SystemPrompt, def.ReadCaps, def.MaxSteps, def.BudgetTokens})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func subagentProfileCatalogInjection(policy subagentChatPolicy) string {
	profiles := mergeSubagentProfiles(policy)
	ids := make([]string, 0, len(profiles))
	for id := range profiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n[子智能体] 可用 profile（subagent.spawn 的 profile 参数）：")
	for _, id := range ids {
		def := profiles[id]
		if ov := policy.Overrides[id]; ov.Enabled != nil && !*ov.Enabled {
			continue
		}
		b.WriteString("\n- ")
		b.WriteString(id)
		b.WriteString(" · ")
		b.WriteString(def.DisplayName)
		if def.Description != "" {
			b.WriteString("：")
			b.WriteString(def.Description)
		}
	}
	b.WriteString("\n复杂、可并行的只读子任务优先 subagent.spawn（可指定 profile）；独立任务同一轮可并行最多 3 个。子代理摘要会出现在工作区「子智能体」面板。")
	return b.String()
}
