package m8app_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestMemoryFabricEval(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "fabric-eval.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()
	if _, err := svc.CreateCanonicalItem(ctx, "local-user", m8app.CreateCanonicalItemInput{
		ScopeKind: "user", Text: "我住在杭州", OperationID: ulid.Make().String(), IdempotencyKey: ulid.Make().String(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateCanonicalItem(ctx, "local-user", m8app.CreateCanonicalItemInput{
		ScopeKind: "user", Text: "我喜欢简洁的回答", OperationID: ulid.Make().String(), IdempotencyKey: ulid.Make().String(),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.HybridRecall(ctx, m8core.HybridRecallQuery{
		SubjectID: "local-user", ScopeKind: "user", Query: "我现在住哪", TopK: 6, BudgetTokens: 320,
	})
	if err != nil {
		t.Fatal(err)
	}
	adopted := ""
	for _, hit := range got.Hits {
		if hit.Adopted {
			adopted += hit.Text
		}
	}
	if !strings.Contains(adopted, "杭州") {
		t.Fatalf("eval miss: %+v", got.Hits)
	}
	if strings.Contains(adopted, "简洁") && !strings.Contains(adopted, "杭州") {
		t.Fatalf("eval leak without target: %+v", got.Hits)
	}
}
