package m8app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

func TestAutoAcceptLowRiskConfirms(t *testing.T) {
	svc, _ := openMemoryService(t)
	prop := propose(t, svc, leafDoc("subject:local-user", "喜欢深色主题", ""), true)

	res, err := svc.AutoAcceptCandidate(context.Background(), prop.Candidate.CandidateID, "chat.auto")
	if err != nil {
		t.Fatalf("auto-accept low risk: %v", err)
	}
	if !res.Accepted || res.Risk != m8core.RiskLow || res.Fact == nil || res.Fact.Version != 1 {
		t.Fatalf("auto-accept result = %+v", res)
	}
	// The candidate is now terminal: a second auto-accept and a token
	// confirm both fail (single promotion).
	if _, err := svc.AutoAcceptCandidate(context.Background(), prop.Candidate.CandidateID, "chat.auto"); !errors.Is(err, m8app.ErrConfirmTokenInvalid) {
		t.Fatalf("re-accept err = %v, want ErrConfirmTokenInvalid", err)
	}
	if _, err := confirm(t, svc, m8app.ConfirmInput{CandidateID: prop.Candidate.CandidateID, Token: prop.ConfirmToken, Action: "confirm", RequestID: "r"}); !errors.Is(err, m8app.ErrConfirmTokenInvalid) {
		t.Fatalf("post-accept confirm err = %v, want ErrConfirmTokenInvalid", err)
	}
}

func TestAutoAcceptHighRiskHeldForHuman(t *testing.T) {
	svc, _ := openMemoryService(t)
	// A secret-bearing payload must never auto-accept.
	prop := propose(t, svc, leafDoc("subject:local-user", "我的密码是 hunter2", ""), true)

	res, err := svc.AutoAcceptCandidate(context.Background(), prop.Candidate.CandidateID, "chat.auto")
	if !errors.Is(err, m8app.ErrExplicitConfirmationRequired) {
		t.Fatalf("high-risk err = %v, want ErrExplicitConfirmationRequired", err)
	}
	if res.Accepted {
		t.Fatalf("high-risk must not accept: %+v", res)
	}
	// It is still pending, so the explicit human token path still works.
	confirmed, err := confirm(t, svc, m8app.ConfirmInput{CandidateID: prop.Candidate.CandidateID, Token: prop.ConfirmToken, Action: "confirm", RequestID: "r"})
	if err != nil {
		t.Fatalf("human confirm after hold: %v", err)
	}
	if confirmed.State != m8core.CandConfirmed || confirmed.Fact == nil {
		t.Fatalf("human confirm result = %+v", confirmed)
	}
}

func TestAutoAcceptSensitiveLevelHeld(t *testing.T) {
	svc, _ := openMemoryService(t)
	prop := propose(t, svc, leafDoc("subject:local-user", "benign text", m8core.SensSensitive), true)
	if _, err := svc.AutoAcceptCandidate(context.Background(), prop.Candidate.CandidateID, "chat.auto"); !errors.Is(err, m8app.ErrExplicitConfirmationRequired) {
		t.Fatalf("sensitive-level err = %v, want ErrExplicitConfirmationRequired", err)
	}
}

func TestAutoAcceptUnknownCandidate(t *testing.T) {
	svc, _ := openMemoryService(t)
	if _, err := svc.AutoAcceptCandidate(context.Background(), "0000000000000000000000000A", "chat.auto"); !errors.Is(err, m8app.ErrCandidateNotFound) {
		t.Fatalf("unknown err = %v, want ErrCandidateNotFound", err)
	}
}
