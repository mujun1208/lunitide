package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/oklog/ulid/v2"
)

func TestOfficeLegacyStaleEvidenceSurvivesRepeatedUnrelatedEdits(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	source := officePublish(t, s, task, ulid.Make().String(), "source")
	original := officeVersion(task.ID, ulid.Make().String())
	original.Index = json.RawMessage(fmt.Sprintf(`{"nodes":[{"id":"value","digest":%q}]}`, strings.Repeat("a", 64)))
	previous, err := s.PublishOfficeVersion(ctx, domain.PublishRequest{Version: original, IdempotencyKey: "target"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate evidence saved before persistent target digest binding existed.
	if err = s.AddOfficeEvidenceEdge(ctx, domain.EvidenceEdge{TaskID: task.ID, SourceVersionID: source.ID, TargetVersionID: previous.ID, TargetNode: "value", Metric: json.RawMessage(`{"rawValue":"100"}`)}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		next := officeVersion(task.ID, previous.ArtifactID)
		next.BaseVersionID = previous.ID
		next.SHA256 = fmt.Sprintf("%064x", i)
		next.Index = json.RawMessage(fmt.Sprintf(`{"nodes":[{"id":"value","digest":%q},{"id":"title","digest":%q}]}`, strings.Repeat("b", 64), fmt.Sprintf("%064x", i)))
		previous, err = s.PublishOfficeVersion(ctx, domain.PublishRequest{Version: next, ExpectedHeadRevision: int64(i), IdempotencyKey: fmt.Sprintf("revision-%d", i)})
		if err != nil || previous.Quality != "stale" {
			t.Fatalf("edit %d washed legacy evidence: quality=%s error=%v", i, previous.Quality, err)
		}
	}
}

func TestOfficePublicationRejectsSourceWhoseUpstreamAlreadyBecameStale(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	upstream := officePublish(t, s, task, ulid.Make().String(), "upstream")
	source := officePublish(t, s, task, ulid.Make().String(), "source")
	if err := s.AddOfficeEvidenceEdge(ctx, domain.EvidenceEdge{TaskID: task.ID, SourceVersionID: upstream.ID, TargetVersionID: source.ID}); err != nil {
		t.Fatal(err)
	}
	next := officeVersion(task.ID, upstream.ArtifactID)
	next.BaseVersionID = upstream.ID
	next.SHA256 = strings.Repeat("b", 64)
	if _, err := s.PublishOfficeVersion(ctx, domain.PublishRequest{Version: next, ExpectedHeadRevision: 1, IdempotencyKey: "new-upstream"}); err != nil {
		t.Fatal(err)
	}
	source, err := s.GetOfficeVersion(ctx, source.ID)
	if err != nil || source.Quality != "stale" {
		t.Fatalf("source did not become stale: %+v %v", source, err)
	}
	// The source remains its artifact's latest head, but that alone is not
	// enough: its dependency became stale after a caller's prior preflight.
	target := officeVersion(task.ID, ulid.Make().String())
	_, err = s.PublishOfficeVersion(ctx, domain.PublishRequest{Version: target, IdempotencyKey: "reject-stale-source", Evidence: []domain.EvidenceEdge{{SourceVersionID: source.ID, Metric: json.RawMessage(`{}`)}}})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("published stale source: %v", err)
	}
	versions, err := s.ListOfficeVersions(ctx, task.ID, target.ArtifactID)
	if err != nil || len(versions) != 0 {
		t.Fatalf("failed evidence left a version: %v %v", versions, err)
	}
}
