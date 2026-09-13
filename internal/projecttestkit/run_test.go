package projecttestkit

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/projectschema"
	"github.com/lunitide/lunitide/internal/projecttask"
)

func TestCLIAndCompleteness(t *testing.T) {
	root := t.TempDir()
	schema := projectschema.Schema{Version: 1, Dialect: "sqlite", Tables: []projectschema.Table{{
		Name: "orders", Columns: []projectschema.Column{{Name: "id", Type: "TEXT", PrimaryKey: true}},
	}}}
	db := projectschema.DefaultDBPath(root)
	if err := projectschema.Materialize(db, schema); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), RunInput{Kind: KindCLI, RootPath: root, Command: "echo ok"})
	if err != nil || res.Status != "pass" {
		t.Fatalf("cli %+v %v", res, err)
	}
	res, err = Run(context.Background(), RunInput{Kind: KindCompleteness, DBPath: db, Schema: schema, Item: projecttask.Item{}})
	if err != nil || res.Status != "pass" {
		t.Fatalf("complete %+v %v", res, err)
	}
	missing := schema
	missing.Tables = append(missing.Tables, projectschema.Table{Name: "gone", Columns: []projectschema.Column{{Name: "id", Type: "TEXT"}}})
	res, err = Run(context.Background(), RunInput{Kind: KindCompleteness, DBPath: db, Schema: missing, Item: projecttask.Item{}})
	if err != nil || res.Status != "fail" {
		t.Fatalf("missing %+v %v", res, err)
	}
	if err := os.WriteFile(filepath.Join(root, "src.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = Run(context.Background(), RunInput{Kind: KindCompleteness, RootPath: root, Item: projecttask.Item{TargetRelPath: "src.txt"}})
	if err != nil || res.Status != "pass" {
		t.Fatalf("file %+v %v", res, err)
	}
}

func TestUnitKindNeedsEvidence(t *testing.T) {
	if _, err := Run(context.Background(), RunInput{Kind: KindUnit}); err != projectapp.ErrTestKindUnsupported {
		t.Fatalf("got %v", err)
	}
	res, err := Run(context.Background(), RunInput{Kind: KindUnit, Evidence: "登录单测已记"})
	if err != nil || res.Status != "pass" {
		t.Fatalf("pass %+v %v", res, err)
	}
	res, err = Run(context.Background(), RunInput{Kind: KindUnit, Evidence: "fail: 断言失败"})
	if err != nil || res.Status != "fail" {
		t.Fatalf("fail %+v %v", res, err)
	}
}

func TestManualKindNeedsEvidence(t *testing.T) {
	if _, err := Run(context.Background(), RunInput{Kind: KindStress}); err != projectapp.ErrTestKindUnsupported {
		t.Fatalf("got %v", err)
	}
	res, err := Run(context.Background(), RunInput{Kind: KindStress, Evidence: "压测记录已上传"})
	if err != nil || res.Status != "pass" {
		t.Fatalf("%+v %v", res, err)
	}
}
