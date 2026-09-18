package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/domain/token"
)

func TestMemoryRecallHangzhouCurrentShanghaiHistorical(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "recall.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	subject := "local-user"
	created, err := store.CreateCanonicalMemoryItem(ctx, m8core.CanonicalMemoryWrite{
		SubjectID: subject, ScopeKind: "user", Text: "我住在上海", OperationID: ulid.Make().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.GetCanonicalMemoryItem(ctx, subject, created.FactID)
	if err != nil {
		t.Fatal(err)
	}
	asOf := parseRecallTime(item.UpdatedAt)
	if asOf.IsZero() {
		t.Fatal("created fact missing timestamp")
	}
	time.Sleep(20 * time.Millisecond)
	if _, err = store.CorrectCanonicalMemoryItem(ctx, subject, created.FactID, "我住在杭州", "迁居", ulid.Make().String(), ulid.Make().String(), item.Revision, nil); err != nil {
		t.Fatal(err)
	}
	current, err := store.HybridRecallCanonical(ctx, m8core.HybridRecallQuery{
		SubjectID: subject, ScopeKind: "user", Query: "我现在住哪", TopK: 6, BudgetTokens: 320,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(adoptedText(current), "杭州") || strings.Contains(adoptedText(current), "上海") {
		t.Fatalf("current should prefer hangzhou: %+v", current.Hits)
	}
	historic, err := store.HybridRecallCanonical(ctx, m8core.HybridRecallQuery{
		SubjectID: subject, ScopeKind: "user", Query: "以前住哪", TopK: 6, BudgetTokens: 320, AsOf: &asOf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(adoptedText(historic), "上海") {
		t.Fatalf("historical should keep shanghai: %+v", historic.Hits)
	}
}

func TestMemoryRecallSameFactOnceAndForgetBarrier(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "recall-once.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	subject := "local-user"
	created, err := store.CreateCanonicalMemoryItem(ctx, m8core.CanonicalMemoryWrite{
		SubjectID: subject, ScopeKind: "user", Text: "我住在杭州西湖边", OperationID: ulid.Make().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := store.HybridRecallCanonical(ctx, m8core.HybridRecallQuery{
		SubjectID: subject, ScopeKind: "user", Query: "杭州西湖", TopK: 6, BudgetTokens: 320,
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, hit := range res.Hits {
		if hit.Adopted {
			seen[hit.FactID]++
		}
	}
	if seen[created.FactID] != 1 {
		t.Fatalf("same fact should appear once, got %v hits=%d", seen, len(res.Hits))
	}
	head, err := store.GetCanonicalMemoryItem(ctx, subject, created.FactID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ForgetCanonicalMemoryItem(ctx, subject, created.FactID, ulid.Make().String(), head.Revision); err != nil {
		t.Fatal(err)
	}
	after, err := store.HybridRecallCanonical(ctx, m8core.HybridRecallQuery{
		SubjectID: subject, ScopeKind: "user", Query: "杭州西湖", TopK: 6, BudgetTokens: 320,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range after.Hits {
		if hit.FactID == created.FactID && hit.Adopted {
			t.Fatal("forgotten fact still recalled")
		}
	}
}

func TestMemoryRecallSkipGenericAndNoEmbedding(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "recall-skip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.CreateCanonicalMemoryItem(ctx, m8core.CanonicalMemoryWrite{
		SubjectID: "local-user", ScopeKind: "user", Text: "我住在杭州", OperationID: ulid.Make().String(),
	}); err != nil {
		t.Fatal(err)
	}
	skip, err := store.HybridRecallCanonical(ctx, m8core.HybridRecallQuery{
		SubjectID: "local-user", ScopeKind: "user", Query: "你好", TopK: 6,
	})
	if err != nil {
		t.Fatal(err)
	}
	if skip.Degraded != "skip_generic" || len(skip.Hits) != 0 {
		t.Fatalf("greeting should skip: %+v", skip)
	}
	weather, err := store.HybridRecallCanonical(ctx, m8core.HybridRecallQuery{
		SubjectID: "local-user", ScopeKind: "user", Query: "今天天气怎么样", TopK: 6,
	})
	if err != nil {
		t.Fatal(err)
	}
	if weather.Degraded != "skip_generic" {
		t.Fatalf("weather without deixis should skip: %+v", weather)
	}
	res, err := store.HybridRecallCanonical(ctx, m8core.HybridRecallQuery{
		SubjectID: "local-user", ScopeKind: "user", Query: "我现在住哪", TopK: 6, BudgetTokens: 320,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Degraded != "no_embedding" {
		t.Fatalf("missing vectors must degrade, got %q", res.Degraded)
	}
}

func TestMemoryTokenBudgetAndMode(t *testing.T) {
	if got := m8core.MemoryTotalBudget(4000, false); got != 320 {
		t.Fatalf("4000*8%%=%d", got)
	}
	if got := m8core.MemoryTotalBudget(40000, false); got != 1536 {
		t.Fatalf("cap 1536 got %d", got)
	}
	if got := m8core.MemoryTotalBudget(4000, true); got != 320 {
		t.Fatalf("companion under 512 got %d", got)
	}
	if got := m8core.MemoryTotalBudget(20000, true); got != 512 {
		t.Fatalf("companion cap 512 got %d", got)
	}
	unknown := token.CountTokensWithMode("", "hello world")
	if unknown.Mode != "estimated" || unknown.Tokens < 1 {
		t.Fatalf("unknown model must stay estimated: %+v", unknown)
	}
	exact := token.CountTokensWithMode("gpt-4", "hello world")
	if exact.Mode != "exact" || exact.Tokens != 2 {
		t.Fatalf("known model should be exact: %+v", exact)
	}
}

func adoptedText(res m8core.HybridRecallResult) string {
	var b strings.Builder
	for _, hit := range res.Hits {
		if hit.Adopted {
			b.WriteString(hit.Text)
			b.WriteByte(' ')
		}
	}
	return b.String()
}
