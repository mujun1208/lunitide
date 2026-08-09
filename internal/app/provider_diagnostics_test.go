package app

import (
	"errors"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
)

func TestDiscoveredModelsPreservesDefaultDeduplicatesAndBounds(t *testing.T) {
	current := provider.Provider{Models: []provider.Model{{ModelID: "m25", DisplayName: "old", IsDefault: true}}}
	input := gateway.Discovery{}
	for i := 59; i >= 0; i-- {
		id := "m" + twoDigits(i)
		input.Models = append(input.Models, gateway.Model{ID: id})
	}
	input.Models = append(input.Models, gateway.Model{ID: "m25"}, gateway.Model{ID: " bad "}, gateway.Model{ID: ""})
	models, warning, ok := discoveredModels(current, input)
	if !ok || warning != "" || len(models) != 50 {
		t.Fatalf("models=%d warning=%q ok=%v", len(models), warning, ok)
	}
	defaults := 0
	for _, m := range models {
		if m.IsDefault {
			defaults++
			if m.ModelID != "m25" {
				t.Fatalf("default changed to %q", m.ModelID)
			}
		}
	}
	if defaults != 1 || models[0].ModelID != "m00" || models[49].ModelID != "m49" {
		t.Fatalf("non-deterministic result: %#v", models)
	}
}

func TestDiscoveredModelsSelectsDeterministicDefaultAndAnthropicPreserves(t *testing.T) {
	current := provider.Provider{Models: []provider.Model{{ModelID: "old", DisplayName: "Old", IsDefault: true}}}
	models, _, ok := discoveredModels(current, gateway.Discovery{Models: []gateway.Model{{ID: "z"}, {ID: "a"}}})
	if !ok || !models[0].IsDefault || models[0].ModelID != "a" {
		t.Fatalf("models=%#v", models)
	}
	models, warning, ok := discoveredModels(current, gateway.Discovery{Unsupported: true, Warning: "upstream free text must not cross"})
	if !ok || warning != "MODEL_DISCOVERY_UNSUPPORTED" || len(models) != 1 || !models[0].IsDefault {
		t.Fatalf("unsupported result=%#v %q", models, warning)
	}
}

func TestDiagnosticResultIsStableAndContainsNoUpstreamText(t *testing.T) {
	canary := "SECRET-CANARY upstream says no"
	d := diagnosticResult(&gateway.Error{Code: "HTTP_401", Stage: gateway.StageHTTP, HTTPStatus: 401, Message: canary}, time.Millisecond, time.Unix(1, 0).UTC())
	if d.Status != "failed" || d.Stage != "authenticate" || d.HTTPStatus != 401 || d.ErrorCode != "HTTP_401" || d.SanitizedMessage == canary || d.Retryable {
		t.Fatalf("unsafe diagnostic: %#v", d)
	}
	d = diagnosticResult(errors.New(canary), 0, time.Unix(1, 0).UTC())
	if d.SanitizedMessage == canary || d.Stage != "resolve" {
		t.Fatalf("unsafe generic diagnostic: %#v", d)
	}
}

func twoDigits(i int) string { return string([]byte{'0' + byte(i/10), '0' + byte(i%10)}) }
