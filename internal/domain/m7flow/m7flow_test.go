package m7flow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFixedStagesAreNineWithCanonicalChain(t *testing.T) {
	defs := FixedStages()
	if len(defs) != 9 {
		t.Fatalf("expected 9 stages, got %d", len(defs))
	}
	wantKeys := []StageKey{
		StageInitiation, StageResearch, StageRequirement, StageSolution,
		StageArchitecture, StageDevelopment, StageVerification, StageRelease,
		StageOperations,
	}
	for i, d := range defs {
		if d.StageKey != string(wantKeys[i]) {
			t.Fatalf("ordinal %d: got %s want %s", i+1, d.StageKey, wantKeys[i])
		}
		if d.Ordinal != i+1 {
			t.Fatalf("ordinal mismatch for %s: %d", d.StageKey, d.Ordinal)
		}
	}
	// First stage has no deps; every later stage chains on its predecessor.
	var deps []string
	if err := json.Unmarshal([]byte(defs[0].DependencyKeys), &deps); err != nil {
		t.Fatal(err)
	}
	if len(deps) != 0 {
		t.Fatalf("first stage must have no deps, got %v", deps)
	}
	for i := 1; i < 9; i++ {
		var deps []string
		if err := json.Unmarshal([]byte(defs[i].DependencyKeys), &deps); err != nil {
			t.Fatal(err)
		}
		if len(deps) != 1 || deps[0] != string(wantKeys[i-1]) {
			t.Fatalf("stage %s deps = %v, want [%s]", defs[i].StageKey, deps, wantKeys[i-1])
		}
	}
}

func TestValidateFixedSetRejectsTampering(t *testing.T) {
	base := FixedStages()
	if err := ValidateFixedSet(base); err != nil {
		t.Fatalf("fixed set must validate: %v", err)
	}
	missing := append([]StageDefinition(nil), base[:8]...)
	if err := ValidateFixedSet(missing); err == nil {
		t.Fatal("8-stage set must be rejected")
	}
	renamed := append([]StageDefinition(nil), base...)
	renamed[3].StageKey = "DESIGN_MAGIC"
	if err := ValidateFixedSet(renamed); err == nil {
		t.Fatal("renamed stage must be rejected")
	}
	subprocess := append([]StageDefinition(nil), base...)
	subprocess[8].StageKey = "security"
	if err := ValidateFixedSet(subprocess); err != ErrSubprocessAsStage {
		t.Fatalf("subprocess key must map to ErrSubprocessAsStage, got %v", err)
	}
}

func TestValidateDAGRejectsCycles(t *testing.T) {
	defs := FixedStages()
	cyclic := append([]StageDefinition(nil), defs...)
	// ARCHITECTURE_PLAN -> SOLUTION_EXPERIENCE -> ARCHITECTURE_PLAN cycle.
	cyclic[4].DependencyKeys = `["SOLUTION_EXPERIENCE","ARCHITECTURE_PLAN"]`
	cyclic[3].DependencyKeys = `["ARCHITECTURE_PLAN"]`
	if err := ValidateDAG(cyclic); err != ErrStageCycle {
		t.Fatalf("cycle must map to ErrStageCycle, got %v", err)
	}
	selfRef := append([]StageDefinition(nil), defs...)
	selfRef[2].DependencyKeys = `["REQUIREMENT_DEFINITION"]`
	if err := ValidateDAG(selfRef); err != ErrStageCycle {
		t.Fatalf("self dependency must map to ErrStageCycle, got %v", err)
	}
}

func TestDefinitionDigestStableAndOrderSensitive(t *testing.T) {
	a, b := FixedStages(), FixedStages()
	if DefinitionDigest(a) != DefinitionDigest(b) {
		t.Fatal("same content must yield the same digest")
	}
	// Slice order is irrelevant: the digest is computed in fixed ordinal
	// order, so shuffling the slice must not change it.
	shuffled := append([]StageDefinition(nil), b...)
	shuffled[0], shuffled[1] = shuffled[1], shuffled[0]
	if DefinitionDigest(shuffled) != DefinitionDigest(a) {
		t.Fatal("digest must be computed in fixed ordinal order, not slice order")
	}
	// Content drift (renaming one stage) must change the digest.
	renamed := append([]StageDefinition(nil), b...)
	renamed[2].Name = "需求定义v2"
	if DefinitionDigest(renamed) == DefinitionDigest(a) {
		t.Fatal("content drift must change the digest")
	}
	// Ordinal drift must change the digest too.
	reordered := append([]StageDefinition(nil), b...)
	reordered[2].Ordinal = 9
	if DefinitionDigest(reordered) == DefinitionDigest(a) {
		t.Fatal("ordinal drift must change the digest")
	}
}

func TestRunStateMachine(t *testing.T) {
	legal := [][2]string{
		{RunDraft, RunReady}, {RunReady, RunRunning}, {RunRunning, RunWaitingReview},
		{RunWaitingReview, RunApproved}, {RunApproved, RunCompleted},
		{RunRunning, RunPaused}, {RunPaused, RunRunning}, {RunRunning, RunBlocked},
		{RunBlocked, RunRunning}, {RunReady, RunCancelled},
	}
	for _, tr := range legal {
		if !LegalRunTransition(tr[0], tr[1]) {
			t.Fatalf("%s -> %s must be legal", tr[0], tr[1])
		}
	}
	illegal := [][2]string{
		{RunDraft, RunRunning}, {RunCompleted, RunRunning}, {RunCancelled, RunReady},
		{RunApproved, RunRunning}, {RunWaitingReview, RunCompleted}, {RunDraft, RunCompleted},
	}
	for _, tr := range illegal {
		if LegalRunTransition(tr[0], tr[1]) {
			t.Fatalf("%s -> %s must be illegal", tr[0], tr[1])
		}
	}
	if !IsTerminalRun(RunCompleted) || !IsTerminalRun(RunCancelled) {
		t.Fatal("completed/cancelled are terminal")
	}
	if IsTerminalRun(RunPaused) || IsTerminalRun(RunBlocked) {
		t.Fatal("paused/blocked are not terminal")
	}
}

func TestNormalizeInputsCanonical(t *testing.T) {
	j1, d1, err := NormalizeInputs(map[string]any{"b": "2", "a": "1"})
	if err != nil {
		t.Fatal(err)
	}
	j2, d2, err := NormalizeInputs(map[string]any{"a": "1", "b": "2"})
	if err != nil {
		t.Fatal(err)
	}
	if j1 != j2 || d1 != d2 {
		t.Fatal("key order must not affect the canonical form")
	}
	if !strings.HasPrefix(j1, `{"a"`) {
		t.Fatalf("keys must be sorted: %s", j1)
	}
	if len(d1) != 64 {
		t.Fatalf("digest must be sha-256 hex, got %d chars", len(d1))
	}
}
