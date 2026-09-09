package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/asset"
)

type openTemplateStore struct {
	mockTemplateStore
	tpl asset.AssetTemplate
}

func (s *openTemplateStore) GetAssetTemplate(context.Context, string) (asset.AssetTemplate, error) {
	return s.tpl, nil
}

func TestTemplateOpenUsesViewingCopy(t *testing.T) {
	for _, status := range []asset.Status{"draft", "enabled", "void"} {
		t.Run(string(status), func(t *testing.T) {
			store := &openTemplateStore{tpl: asset.AssetTemplate{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", FileName: "report.txt", FilePath: "stored.txt", TemplateType: "document", Status: status}}
			files := &memTemplateFiles{files: map[string][]byte{"stored.txt": []byte("original")}}
			e := &Engine{assets: store, templateFiles: files}
			old := openArtifactTarget
			t.Cleanup(func() { openArtifactTarget = old })
			var opened string
			openArtifactTarget = func(path string, file, reveal bool) error {
				opened = path
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "original" || !file || reveal || filepath.Base(path) != "report.txt" {
					t.Fatalf("incorrect viewing copy: %q %v", data, err)
				}
				return os.WriteFile(path, []byte("edited copy"), 0600)
			}
			out := handleTemplateOpen(e, context.Background(), validRequest("template.open", `{"id":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`))
			if !out.OK || opened == "" {
				t.Fatalf("open failed: %+v", out)
			}
			if string(files.files["stored.txt"]) != "original" {
				t.Fatal("asset original changed")
			}
			_ = os.Remove(opened)
			_ = os.Remove(filepath.Dir(opened))
		})
	}
}

func TestTemplateOpenDeniesOtherScopeAndUnsafeName(t *testing.T) {
	for _, tpl := range []asset.AssetTemplate{
		{OrgID: "other", FileName: "report.txt", TemplateType: "document"},
		{FileName: "../report.txt", TemplateType: "document"},
		{FileName: "run.exe", TemplateType: "document"},
		{FileName: "report:stream.txt", TemplateType: "document"},
	} {
		e := &Engine{assets: &openTemplateStore{tpl: tpl}, templateFiles: &memTemplateFiles{}}
		out := handleTemplateOpen(e, context.Background(), validRequest("template.open", `{"id":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`))
		if out.OK {
			t.Fatalf("accepted unsafe template: %+v", tpl)
		}
	}
}

func TestTemplateOpenReportsShellFailure(t *testing.T) {
	old := openArtifactTarget
	t.Cleanup(func() { openArtifactTarget = old })
	var opened string
	openArtifactTarget = func(path string, _, _ bool) error { opened = path; return errors.New("no app") }
	e := &Engine{assets: &openTemplateStore{tpl: asset.AssetTemplate{FileName: "report.txt", FilePath: "stored", TemplateType: "document"}}, templateFiles: &memTemplateFiles{files: map[string][]byte{"stored": []byte("text")}}}
	out := handleTemplateOpen(e, context.Background(), validRequest("template.open", `{"id":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`))
	if out.OK {
		t.Fatal("reported false success")
	}
	if _, err := os.Stat(opened); !os.IsNotExist(err) {
		t.Fatalf("failed view copy not cleaned: %v", err)
	}
}
