package app

import (
	"strings"
	"testing"
)

func TestProjectPhaseWorkflowInjectionDev(t *testing.T) {
	hint := projectPhaseWorkflowInjection(5, "开发")
	if hint == "" {
		t.Fatal("expected dev hint")
	}
	for _, skill := range []string{"implement", "tdd-loop", "code-reviewer", "pm-phase-5"} {
		if !strings.Contains(hint, skill) {
			t.Fatalf("hint missing %s: %s", skill, hint)
		}
	}
}

func TestProjectPhaseWorkflowInjectionOpsDev(t *testing.T) {
	hint := projectPhaseWorkflowInjection(4, "开发")
	if !strings.Contains(hint, "pm-phase-4") {
		t.Fatalf("expected ops dev pm-phase-4, got %q", hint)
	}
}
