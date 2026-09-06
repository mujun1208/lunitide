package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestValidPublicChatMessagesRejectsPrivilegedRoles(t *testing.T) {
	for _, role := range []llmadapter.Role{llmadapter.RoleSystem, llmadapter.RoleTool} {
		if validChatMessages("model", []llmadapter.Message{{Role: role, Content: "renderer-controlled"}}) {
			t.Fatalf("public chat.start accepted privileged role %q", role)
		}
	}
	if !validChatMessages("model", []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "question"}, {Role: llmadapter.RoleAssistant, Content: "answer"}}) {
		t.Fatal("public chat.start rejected legal public roles")
	}
}

func TestExecutionModeDefaultsValidatesAndProducesEngineOwnedPolicy(t *testing.T) {
	mode, ok := normalizeExecutionMode("")
	if !ok || mode != executionModeApproval {
		t.Fatalf("default = %q, %v", mode, ok)
	}
	if _, ok := normalizeExecutionMode("unrestricted"); ok {
		t.Fatal("accepted unknown execution mode")
	}
	for _, mode := range []executionMode{executionModeApproval, executionModeAutoEdit, executionModePlan, executionModeFullAccess} {
		if instruction := executionModeInstruction(mode); !strings.Contains(instruction, "Execution mode:") {
			t.Fatalf("mode %q instruction = %q", mode, instruction)
		}
	}
}

func TestPlanExecutionModeStrictlyForbidsExecutionAndMutationClaims(t *testing.T) {
	// Legacy "plan" mode was replaced by system-automatic complexity routing:
	// it must normalize to approval, whose instruction still forbids claiming
	// mutations that never happened.
	mode, ok := normalizeExecutionMode(executionModePlan)
	if !ok || mode != executionModeApproval {
		t.Fatalf("legacy plan mode must map to approval, got %q (ok=%v)", mode, ok)
	}
	instruction := executionModeInstruction(mode)
	for _, required := range []string{"Execution mode: approval", "obtain explicit user approval", "never claim that a command ran"} {
		if !strings.Contains(instruction, required) {
			t.Fatalf("approval instruction lacks %q: %s", required, instruction)
		}
	}
}

func TestTrustedExecutionInstructionPrecedesRawNearLimitMessage(t *testing.T) {
	raw := strings.Repeat("x", 16*1024)
	if !validChatMessages("m", []llmadapter.Message{{Role: llmadapter.RoleUser, Content: raw}}) {
		t.Fatal("near-limit public message rejected")
	}
	trusted := append([]llmadapter.Message{{Role: llmadapter.RoleSystem, Content: executionModeInstruction(executionModeApproval)}}, llmadapter.Message{Role: llmadapter.RoleUser, Content: raw})
	if trusted[0].Role != llmadapter.RoleSystem || trusted[1].Content != raw {
		t.Fatalf("trusted assembly changed authority or raw content: %#v", trusted)
	}
}
