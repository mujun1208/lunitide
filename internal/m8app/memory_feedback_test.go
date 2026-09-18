package m8app_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestMemoryFeedbackCannotPromoteAuthority(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "feedback.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()
	green, err := svc.CreateCanonicalItem(ctx, "local-user", m8app.CreateCanonicalItemInput{
		ScopeKind: "user", Text: "偏好绿茶作为默认饮品", OperationID: ulid.Make().String(), IdempotencyKey: ulid.Make().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	red, err := svc.CreateCanonicalItem(ctx, "local-user", m8app.CreateCanonicalItemInput{
		ScopeKind: "user", Text: "偏好红茶作为默认饮品", OperationID: ulid.Make().String(), IdempotencyKey: ulid.Make().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordCanonicalFeedback(ctx, "local-user", green.Item.FactID, green.Item.Version, "turn-helpful", m8core.MemoryFeedbackHelpful); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordCanonicalFeedback(ctx, "local-user", red.Item.FactID, red.Item.Version, "turn-no", m8core.MemoryFeedbackContradicted); err != nil {
		t.Fatal(err)
	}
	afterGreen, err := svc.GetCanonicalItem(ctx, "local-user", green.Item.FactID)
	if err != nil || afterGreen.Version != green.Item.Version || afterGreen.Text != green.Item.Text {
		t.Fatalf("helpful must not rewrite %+v err=%v", afterGreen, err)
	}
	afterRed, err := svc.GetCanonicalItem(ctx, "local-user", red.Item.FactID)
	if err != nil || afterRed.Version != red.Item.Version || afterRed.Text != red.Item.Text {
		t.Fatalf("contradicted must not rewrite %+v err=%v", afterRed, err)
	}
	res, err := svc.HybridRecall(ctx, m8core.HybridRecallQuery{
		SubjectID: "local-user", ScopeKind: "user", Query: "默认饮品", TopK: 4, BudgetTokens: 400,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) < 2 {
		t.Fatalf("expected both drinks: %+v", res.Hits)
	}
	var greenHit, redHit m8core.HybridRecallHit
	for _, hit := range res.Hits {
		if hit.FactID == green.Item.FactID {
			greenHit = hit
		}
		if hit.FactID == red.Item.FactID {
			redHit = hit
		}
	}
	if greenHit.FeedbackScore <= 0 || redHit.FeedbackScore >= 0 {
		t.Fatalf("feedback scores green=%v red=%v", greenHit.FeedbackScore, redHit.FeedbackScore)
	}
	if greenHit.FusedScore <= redHit.FusedScore {
		t.Fatalf("helpful must rank above contradicted green=%v red=%v", greenHit.FusedScore, redHit.FusedScore)
	}
	pending, err := svc.ListPendingCandidates(ctx, 20)
	if err != nil || len(pending) != 0 {
		t.Fatalf("helpful must not propose candidates %v err=%v", pending, err)
	}
}
