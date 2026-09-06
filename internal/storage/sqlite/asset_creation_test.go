package sqlite

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/asset"
	"github.com/lunitide/lunitide/internal/domain/project"
)

func creationTestTemplate() asset.AssetTemplate {
	return asset.AssetTemplate{Name: "Template", TemplateType: asset.TemplateTypeDocument, DocumentType: asset.DocumentTypeBusinessBlueprint, Description: "Evidence", MimeType: "application/msword", FileName: "template.dot", FilePath: "file-ref"}
}

func TestAssetCreationExpiredKeyDoesNotCreateAnotherTemplate(t *testing.T) {
	s, _, _ := phaseTestStore(t, project.TypeImplementation)
	ctx := context.Background()
	digest := strings.Repeat("a", 64)
	if _, err := s.CreateAssetTemplateIdempotent(ctx, "create-key", digest, creationTestTemplate()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE asset_template_creations SET expires_at='2000-01-01T00:00:00Z' WHERE request_key='create-key'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAssetTemplateIdempotent(ctx, "create-key", digest, creationTestTemplate()); !errors.Is(err, asset.ErrCreationExpired) {
		t.Fatalf("err=%v", err)
	}
	var count int
	_ = s.db.QueryRow(`SELECT count(*) FROM asset_templates`).Scan(&count)
	if count != 1 {
		t.Fatalf("templates=%d", count)
	}
}
func TestAssetCreationRecordsAndAuditRollbackTogether(t *testing.T) {
	for _, point := range []string{"asset_template_creations", "audit_events"} {
		t.Run(point, func(t *testing.T) {
			s, _, _ := phaseTestStore(t, project.TypeImplementation)
			if _, err := s.db.Exec(fmt.Sprintf("CREATE TRIGGER injected_template_failure BEFORE INSERT ON %s BEGIN SELECT RAISE(ABORT,'injected template failure'); END", point)); err != nil {
				t.Fatal(err)
			}
			if _, err := s.CreateAssetTemplateIdempotent(context.Background(), "create-key", strings.Repeat("a", 64), creationTestTemplate()); err == nil {
				t.Fatal("failure ignored")
			}
			for _, table := range []string{"asset_templates", "asset_template_creations"} {
				var count int
				_ = s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count)
				if count != 0 {
					t.Fatalf("partial %s=%d", table, count)
				}
			}
		})
	}
}
func TestAssetCreationConcurrentSameKeyHasOneIdentity(t *testing.T) {
	s, _, _ := phaseTestStore(t, project.TypeImplementation)
	ctx := context.Background()
	const workers = 8
	results := make(chan asset.AssetTemplate, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tpl, err := s.CreateAssetTemplateIdempotent(ctx, "create-key", strings.Repeat("a", 64), creationTestTemplate())
			results <- tpl
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	first := ""
	for tpl := range results {
		if first == "" {
			first = tpl.ID
		}
		if tpl.ID != first {
			t.Fatalf("duplicate identity=%s / %s", first, tpl.ID)
		}
	}
	for _, table := range []string{"asset_templates", "asset_template_creations"} {
		var count int
		_ = s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count)
		if count != 1 {
			t.Fatalf("%s=%d", table, count)
		}
	}
}
