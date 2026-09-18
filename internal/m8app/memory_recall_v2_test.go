package m8app_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestMemoryRecallRoutes(t *testing.T) {
	TestMemoryHistoricalRecall(t)
}

func TestMemoryHistoricalRecall(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "recall-v2.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()
	created, err := svc.CreateCanonicalItem(ctx, "local-user", m8app.CreateCanonicalItemInput{
		ScopeKind: "user", Text: "我住在上海", OperationID: ulid.Make().String(), IdempotencyKey: ulid.Make().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	asOf, err := time.Parse(time.RFC3339Nano, created.Item.UpdatedAt)
	if err != nil {
		asOf, err = time.Parse(time.RFC3339, created.Item.UpdatedAt)
	}
	if err != nil || asOf.IsZero() {
		t.Fatalf("created timestamp %q err=%v", created.Item.UpdatedAt, err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, _, err := svc.CorrectCanonicalItem(ctx, "local-user", created.Item.FactID, "我住在杭州", "迁居", ulid.Make().String(), ulid.Make().String(), created.Item.Revision, nil); err != nil {
		t.Fatal(err)
	}
	current, err := svc.HybridRecall(ctx, m8core.HybridRecallQuery{
		SubjectID: "local-user", ScopeKind: "user", Query: "我现在住哪", TopK: 6, BudgetTokens: 320,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(adoptedRecallText(current), "杭州") || strings.Contains(adoptedRecallText(current), "上海") {
		t.Fatalf("current should prefer hangzhou: %+v", current.Hits)
	}
	historic, err := svc.HybridRecall(ctx, m8core.HybridRecallQuery{
		SubjectID: "local-user", ScopeKind: "user", Query: "以前住哪", TopK: 6, BudgetTokens: 320, AsOf: &asOf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(adoptedRecallText(historic), "上海") {
		t.Fatalf("historical should keep shanghai: %+v", historic.Hits)
	}
}

func TestMemoryTokenBudget(t *testing.T) {
	if got := m8core.MemoryTotalBudget(4000, false); got != 320 {
		t.Fatalf("4000*8%%=%d", got)
	}
	if got := m8core.MemoryTotalBudget(40000, false); got != 1536 {
		t.Fatalf("cap 1536 got %d", got)
	}
	unknown := token.CountTokensWithMode("", "hello world")
	if unknown.Mode != "estimated" || unknown.Tokens < 1 {
		t.Fatalf("unknown model must stay estimated: %+v", unknown)
	}
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "recall-budget.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()
	if _, err := svc.CreateCanonicalItem(ctx, "local-user", m8app.CreateCanonicalItemInput{
		ScopeKind: "user", Text: "我住在杭州西湖边", OperationID: ulid.Make().String(), IdempotencyKey: ulid.Make().String(),
	}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.HybridRecall(ctx, m8core.HybridRecallQuery{
		SubjectID: "local-user", ScopeKind: "user", Query: "我现在住哪", TopK: 6, BudgetTokens: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.BudgetTokens != 8 || res.UsedTokens > res.BudgetTokens {
		t.Fatalf("budget overrun %+v", res)
	}
}

func adoptedRecallText(res m8core.HybridRecallResult) string {
	var b strings.Builder
	for _, hit := range res.Hits {
		if hit.Adopted {
			b.WriteString(hit.Text)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
