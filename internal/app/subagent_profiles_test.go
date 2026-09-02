package app

import (
	"encoding/json"
	"testing"
)

func TestParseSubagentChatPolicyDefaults(t *testing.T) {
	p := parseSubagentChatPolicy(nil)
	if p.DelegationMode != delegationProactive {
		t.Fatalf("default mode = %q, want proactive", p.DelegationMode)
	}
}

func TestParseSubagentChatPolicyModes(t *testing.T) {
	raw := json.RawMessage(`{"delegationMode":"disabled"}`)
	if parseSubagentChatPolicy(raw).DelegationMode != delegationDisabled {
		t.Fatal("expected disabled")
	}
	raw = json.RawMessage(`{"delegationMode":"explicit"}`)
	if parseSubagentChatPolicy(raw).DelegationMode != delegationExplicit {
		t.Fatal("expected explicit")
	}
}

func TestResolveSubagentProfileBuiltin(t *testing.T) {
	def, _, ok := resolveSubagentProfile(defaultSubagentChatPolicy(), "research")
	if !ok || def.ID != "research" {
		t.Fatalf("profile = %+v ok=%v", def, ok)
	}
	if len(def.ReadCaps) == 0 {
		t.Fatal("expected read caps")
	}
}

func TestResolveSubagentProfileDisabled(t *testing.T) {
	disabled := false
	policy := subagentChatPolicy{
		DelegationMode: delegationProactive,
		Overrides: map[string]subagentProfileOverride{
			"explore": {Enabled: &disabled},
		},
	}
	_, _, ok := resolveSubagentProfile(policy, "explore")
	if ok {
		t.Fatal("disabled profile should not resolve")
	}
}

func TestResolveSubagentProfileCustom(t *testing.T) {
	policy := subagentChatPolicy{
		DelegationMode: delegationProactive,
		CustomProfiles: []subagentProfileDef{{
			ID: "my-bot", DisplayName: "My bot", SystemPrompt: "read only",
			ReadCaps: []string{"web.search"}, MaxSteps: 2, BudgetTokens: 2000,
		}},
	}
	def, _, ok := resolveSubagentProfile(policy, "my-bot")
	if !ok || def.ID != "my-bot" {
		t.Fatalf("custom profile missing: %+v ok=%v", def, ok)
	}
}

func TestImplementerProfileWriteGate(t *testing.T) {
	// Under approval / plan mode the implementer degrades to read-only.
	for _, mode := range []executionMode{executionModeApproval, executionModePlan, ""} {
		policy := defaultSubagentChatPolicy()
		policy.ParentMode = mode
		def, _, ok := resolveSubagentProfile(policy, "implementer")
		if !ok || def.ID != "implementer" {
			t.Fatalf("implementer missing under %q: %+v", mode, def)
		}
		if len(def.WriteTools) != 0 {
			t.Fatalf("implementer must be read-only under %q, got %v", mode, def.WriteTools)
		}
	}
	// Under a write-capable parent turn it carries exactly the file writers.
	for _, mode := range []executionMode{executionModeAutoEdit, executionModeFullAccess} {
		policy := defaultSubagentChatPolicy()
		policy.ParentMode = mode
		def, _, ok := resolveSubagentProfile(policy, "implementer")
		if !ok {
			t.Fatalf("implementer missing under %q", mode)
		}
		if len(def.WriteTools) != 2 {
			t.Fatalf("implementer writers under %q = %v, want workspace.write/edit", mode, def.WriteTools)
		}
		for _, name := range def.WriteTools {
			if name != "workspace.write" && name != "workspace.edit" {
				t.Fatalf("unexpected implementer write tool %q under %q", name, mode)
			}
		}
	}
}

func TestResolveSubagentProfileUnknownFallsBack(t *testing.T) {
	def, _, ok := resolveSubagentProfile(defaultSubagentChatPolicy(), "does-not-exist")
	if !ok || def.ID != "general-purpose" {
		t.Fatalf("fallback = %+v", def)
	}
}

func TestBuiltinSubagentProfileSteps(t *testing.T) {
	if subagentMaxSteps != 16 {
		t.Fatalf("subagentMaxSteps = %d", subagentMaxSteps)
	}
	want := map[string]int{
		"explore": 8, "research": 16, "general-purpose": 16,
		"review": 8, "browser": 8, "test": 8, "shell": 3, "writer": 3,
	}
	for id, steps := range want {
		def, _, ok := resolveSubagentProfile(defaultSubagentChatPolicy(), id)
		if !ok {
			t.Fatalf("%s missing", id)
		}
		if def.MaxSteps != steps {
			t.Fatalf("%s MaxSteps = %d, want %d", id, def.MaxSteps, steps)
		}
	}
}

func TestSubagentProfileCatalogInjectionSkipsDisabled(t *testing.T) {
	disabled := false
	policy := subagentChatPolicy{
		DelegationMode: delegationProactive,
		Overrides: map[string]subagentProfileOverride{
			"shell": {Enabled: &disabled},
		},
	}
	text := subagentProfileCatalogInjection(policy)
	if text == "" {
		t.Fatal("expected catalog text")
	}
	if indexOf(text, "\n- shell ·") >= 0 {
		t.Fatal("disabled shell should not appear in catalog")
	}
}

func TestSubagentStoredPurposeTagsProfile(t *testing.T) {
	got := subagentStoredPurpose(subagentProfileDef{ID: "explore", DisplayName: "Explore"}, "find auth code")
	want := "[Explore] find auth code"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
