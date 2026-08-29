package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/asset"
)

func TestCreateAssetTemplateWritesAudit(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "asset-audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	got, err := store.CreateAssetTemplate(ctx, asset.AssetTemplate{
		Name:         "vvvv",
		TemplateType: asset.TemplateTypeDocument,
		DocumentType: asset.DocumentTypeFeatureDesign,
		Description:  "vvvv",
		Client:       "vvvv",
		MimeType:     "application/vnd.ms-excel",
		FileName:     "项目名称-项目-第1周工作计划.xls",
		FilePath:     "templates/week-plan.xls",
	})
	if err != nil {
		t.Fatalf("CreateAssetTemplate: %v", err)
	}
	if got.ID == "" || got.TemplateCode == "" {
		t.Fatalf("expected persisted template, got %#v", got)
	}
	var action string
	if err := store.db.QueryRowContext(ctx, `SELECT action FROM audit_events WHERE aggregate_id=?`, got.ID).Scan(&action); err != nil {
		t.Fatal(err)
	}
	if action != "asset_template.created" {
		t.Fatalf("audit action = %q", action)
	}
}
