// Learning-loop service tests (P3-3): feedback recording, pending
// candidate listing, confirmed-snapshot budgeting - against a fully
// migrated SQLite store. The FR-11 invariant is re-asserted end-to-end:
// corrections only propose; the explicit token path is the sole promotion.
package m8app_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/m8app"
)

func TestRecordFeedbackAcceptRejectOnlyWritesEvidence(t *testing.T) {
	svc, _ := openMemoryService(t)
	for _, action := range []string{m8app.FeedbackAccept, m8app.FeedbackReject} {
		res, err := svc.RecordFeedback(context.Background(), m8app.FeedbackRecordInput{
			Action: action, TargetType: "message", TargetID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		})
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		if res.EventID == "" || res.CandidateID != "" || res.ConfirmationToken != "" {
			t.Fatalf("%s: result = %+v, want event only", action, res)
		}
	}
}

func TestRecordFeedbackCorrectProposesPendingCandidate(t *testing.T) {
	svc, _ := openMemoryService(t)
	res, err := svc.RecordFeedback(context.Background(), m8app.FeedbackRecordInput{
		Action: m8app.FeedbackCorrect, TargetType: "message", TargetID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Text: "回答默认使用中文",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.CandidateID == "" || len(res.ConfirmationToken) != 64 {
		t.Fatalf("result = %+v, want candidate + token", res)
	}
	pending, err := svc.ListPendingCandidates(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].CandidateID != res.CandidateID {
		t.Fatalf("pending = %+v", pending)
	}
	if pending[0].Content != "回答默认使用中文" || pending[0].ScopeID != m8app.LearningScope {
		t.Fatalf("pending[0] = %+v", pending[0])
	}
}

func TestRecordFeedbackValidation(t *testing.T) {
	svc, _ := openMemoryService(t)
	ctx := context.Background()
	if _, err := svc.RecordFeedback(ctx, m8app.FeedbackRecordInput{Action: "maybe", TargetType: "message", TargetID: "m1"}); !errors.Is(err, m8app.ErrPayloadInvalid) {
		t.Fatalf("action err = %v", err)
	}
	if _, err := svc.RecordFeedback(ctx, m8app.FeedbackRecordInput{Action: m8app.FeedbackCorrect, TargetType: "message", TargetID: "m1"}); !errors.Is(err, m8app.ErrPayloadInvalid) {
		t.Fatalf("empty-correct err = %v", err)
	}
	if _, err := svc.RecordFeedback(ctx, m8app.FeedbackRecordInput{Action: m8app.FeedbackAccept, TargetType: "", TargetID: "m1"}); !errors.Is(err, m8app.ErrPayloadInvalid) {
		t.Fatalf("target err = %v", err)
	}
}

func TestConfirmedSnapshotOnlyAfterExplicitConfirm(t *testing.T) {
	svc, clock := openMemoryService(t)
	ctx := context.Background()
	res, err := svc.RecordFeedback(ctx, m8app.FeedbackRecordInput{
		Action: m8app.FeedbackCorrect, TargetType: "message", TargetID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Text: "偏好A：代码注释用中文",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Before confirmation the snapshot stays empty (FR-11).
	prefs, err := svc.ConfirmedSnapshot(ctx, m8app.LearningScope, 8, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if len(prefs) != 0 {
		t.Fatalf("snapshot before confirm = %v", prefs)
	}
	clock.now = clock.now.Add(time.Minute)
	if _, err := svc.ConfirmCandidate(ctx, m8app.ConfirmInput{
		CandidateID: res.CandidateID, Token: res.ConfirmationToken,
		Action: "confirm", RequestID: "req-l1",
	}); err != nil {
		t.Fatal(err)
	}
	prefs, err = svc.ConfirmedSnapshot(ctx, m8app.LearningScope, 8, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if len(prefs) != 1 || prefs[0] != "偏好A：代码注释用中文" {
		t.Fatalf("snapshot after confirm = %v", prefs)
	}
	// Rejected corrections never enter the snapshot.
	res2, err := svc.RecordFeedback(ctx, m8app.FeedbackRecordInput{
		Action: m8app.FeedbackCorrect, TargetType: "message", TargetID: "01ARZ3NDEKTSV4RRFFQ69G5FAB",
		Text: "偏好B：回答要简短",
	})
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Minute)
	if _, err := svc.ConfirmCandidate(ctx, m8app.ConfirmInput{
		CandidateID: res2.CandidateID, Token: res2.ConfirmationToken,
		Action: "reject", RequestID: "req-l2",
	}); err != nil {
		t.Fatal(err)
	}
	prefs, err = svc.ConfirmedSnapshot(ctx, m8app.LearningScope, 8, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if len(prefs) != 1 {
		t.Fatalf("snapshot after reject = %v", prefs)
	}
}

func TestConfirmedSnapshotBudgetSkipsNotTruncates(t *testing.T) {
	svc, clock := openMemoryService(t)
	ctx := context.Background()
	long := strings.Repeat("长", 100) // 300 bytes
	for i := 0; i < 3; i++ {
		res, err := svc.RecordFeedback(ctx, m8app.FeedbackRecordInput{
			Action: m8app.FeedbackCorrect, TargetType: "message", TargetID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
			Text: long,
		})
		if err != nil {
			t.Fatal(err)
		}
		clock.now = clock.now.Add(time.Minute)
		if _, err := svc.ConfirmCandidate(ctx, m8app.ConfirmInput{
			CandidateID: res.CandidateID, Token: res.ConfirmationToken,
			Action: "confirm", RequestID: "req-b",
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Budget 640 bytes: two 300-byte preferences fit, the third is skipped
	// wholesale - never a half-rune truncation.
	prefs, err := svc.ConfirmedSnapshot(ctx, m8app.LearningScope, 8, 640)
	if err != nil {
		t.Fatal(err)
	}
	if len(prefs) != 2 {
		t.Fatalf("prefs = %d, want 2", len(prefs))
	}
	for _, p := range prefs {
		if p != long {
			t.Fatalf("preference mutated: len=%d", len(p))
		}
	}
}
