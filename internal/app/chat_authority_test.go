package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/gateway"
)

func TestValidPublicChatMessagesRejectsPrivilegedRoles(t *testing.T) {
	for _, role := range []gateway.Role{gateway.RoleSystem, gateway.RoleTool} {
		if validChatMessages("model", []gateway.Message{{Role: role, Content: "renderer-controlled"}}) {
			t.Fatalf("public chat.start accepted privileged role %q", role)
		}
	}
	if !validChatMessages("model", []gateway.Message{{Role: gateway.RoleUser, Content: "question"}, {Role: gateway.RoleAssistant, Content: "answer"}}) {
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
	instruction := executionModeInstruction(executionModePlan)
	for _, required := range []string{"Planning only", "Do not invoke tools", "execute commands", "create, edit, or delete files", "Do not claim"} {
		if !strings.Contains(instruction, required) {
			t.Fatalf("plan instruction lacks %q: %s", required, instruction)
		}
	}
}

func TestTrustedExecutionInstructionPrecedesRawNearLimitMessage(t *testing.T) {
	raw := strings.Repeat("x", 16*1024)
	if !validChatMessages("m", []gateway.Message{{Role: gateway.RoleUser, Content: raw}}) {
		t.Fatal("near-limit public message rejected")
	}
	trusted := append([]gateway.Message{{Role: gateway.RoleSystem, Content: executionModeInstruction(executionModeApproval)}}, gateway.Message{Role: gateway.RoleUser, Content: raw})
	if trusted[0].Role != gateway.RoleSystem || trusted[1].Content != raw {
		t.Fatalf("trusted assembly changed authority or raw content: %#v", trusted)
	}
}
