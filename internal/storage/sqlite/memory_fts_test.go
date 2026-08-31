package sqlite

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/migrations"
	"github.com/oklog/ulid/v2"
)

func TestMemoryFactFTSMigrationIsLF(t *testing.T) {
	body, err := migrations.Files.ReadFile("0107_memory_fact_fts.sql")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte{'\r'}) {
		t.Fatal("0107 must be LF; CRLF changes the checksum")
	}
}

func TestSearchMemoryFactFTSIndexesConfirmedAndSummary(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "memory-fts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user")
	svc.SetFTS(store)
	prop, err := svc.ProposeCandidate(ctx, m8app.ProposeInput{
		SubjectID: "local-user",
		Doc: m8core.PayloadDoc{
			Content: "代码注释默认使用中文", ScopeID: m8app.LearningScope, Sensitivity: m8core.SensPrivate,
			Leaves: []m8core.SourceLeafClaim{{JSONPointer: "/content", EvidenceRef: "artifact://run-1/evidence-a", Digest: strings.Repeat("a", 64)}},
		},
		Trust: m8core.TrustUntrusted, Actor: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmCandidate(ctx, m8app.ConfirmInput{
		CandidateID: prop.Candidate.CandidateID, Token: prop.ConfirmToken, Action: "confirm", RequestID: "fts-" + prop.Candidate.CandidateID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO memory_fact_fts(source_id, kind, body) VALUES(?, 'summary', ?)`,
		ulid.Make().String(), "确认过的周会结论"); err != nil {
		t.Fatal(err)
	}
	hits, err := store.SearchMemoryFactFTS(ctx, "中文 注释", 8)
	if err != nil {
		t.Fatal(err)
	}
	foundCand := false
	for _, hit := range hits {
		if hit.Kind == "candidate" && strings.Contains(hit.Body, "代码注释默认使用中文") {
			foundCand = true
		}
	}
	if !foundCand {
		t.Fatalf("confirmed candidate missing from FTS: %#v", hits)
	}
	sumHits, err := store.SearchMemoryFactFTS(ctx, "周会结论", 8)
	if err != nil {
		t.Fatal(err)
	}
	foundSum := false
	for _, hit := range sumHits {
		if hit.Kind == "summary" && strings.Contains(hit.Body, "周会结论") {
			foundSum = true
		}
	}
	if !foundSum {
		t.Fatalf("compaction summary missing from FTS: %#v", sumHits)
	}
	res, err := svc.RecallForInject(ctx, m8app.RecallInput{ScopeID: m8app.LearningScope, Query: "中文注释", TopK: 6})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("RecallForInject must use FTS hits")
	}
}
