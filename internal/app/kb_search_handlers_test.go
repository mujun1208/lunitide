package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/ocrapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func newKBSearchEngine(t *testing.T) *Engine {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "kb-search.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo := store.AgentRuntimeRepository()
	e := NewEngine(nil, "test")
	kb := m8app.NewKBService(repo, "local-user")
	e.SetM8SliceServices(kb, nil, nil)
	e.SetExpertGrowthService(m8app.NewGrowthService(repo))
	return e
}

func TestKBSearchHandlerEmptyQueryFails(t *testing.T) {
	e := newKBSearchEngine(t)
	resp := e.Handle(context.Background(), nominationRequest("kb.search", `{"expertId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","query":""}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("empty query = %+v", resp)
	}
}

func TestKBSearchHandlerMissingCollection(t *testing.T) {
	e := newKBSearchEngine(t)
	id := ulid.Make().String()
	resp := e.Handle(context.Background(), nominationRequest("kb.search", `{"expertId":"`+id+`","query":"retraction"}`))
	if !resp.OK {
		t.Fatalf("missing collection should be 200: %+v", resp.Error)
	}
	var body struct {
		Explanation struct {
			Missing bool `json:"missing"`
		} `json:"explanation"`
	}
	if err := json.Unmarshal(mustJSON(resp.Payload), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Explanation.Missing {
		t.Fatalf("want missing: %s", resp.Payload)
	}
}

func TestExpertKnowledgeGetLocalizesStoredSourceError(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "kb-get-zh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo := store.AgentRuntimeRepository()
	e := NewEngine(nil, "test")
	kb := m8app.NewKBService(repo, "local-user")
	e.SetM8SliceServices(kb, nil, nil)
	expert := ulid.Make().String()
	coll, err := kb.EnsureExpertCollection(ctx, expert)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "note.md")
	if err = os.WriteFile(path, []byte("周报正文"), 0600); err != nil {
		t.Fatal(err)
	}
	ingested, err := kb.IngestLocalSource(ctx, m8app.KBLocalSourceInput{CollectionID: coll.CollectionID, Path: path, MediaType: "text/markdown"})
	if err != nil {
		t.Fatal(err)
	}
	src := ingested.Source
	src.State = "failed"
	src.Error = "parse function not configured"
	src.Revision++
	if err = repo.TransactKB(ctx, func(tx m8app.KBTx) error {
		return tx.(m8app.KBSourceTx).PutKBSource(src, ingested.Source.Revision)
	}); err != nil {
		t.Fatal(err)
	}
	resp := e.Handle(ctx, nominationRequest("expert.knowledge.get", `{"expertId":"`+expert+`"}`))
	if !resp.OK {
		t.Fatalf("knowledge.get = %+v", resp.Error)
	}
	var body struct {
		Sources []struct {
			Error    string `json:"error"`
			Versions []struct {
				Error string `json:"error"`
			} `json:"versions"`
		} `json:"sources"`
	}
	if err = json.Unmarshal(mustJSON(resp.Payload), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Sources) != 1 {
		t.Fatalf("sources=%d payload=%s", len(body.Sources), resp.Payload)
	}
	got := body.Sources[0].Error
	if strings.Contains(got, "parse function") || !strings.Contains(got, "未配置正文解析") {
		t.Fatalf("source error must stay Chinese, got %q", got)
	}
}

func TestExpertKnowledgeGetMissingIsOK(t *testing.T) {
	e := newKBSearchEngine(t)
	id := ulid.Make().String()
	resp := e.Handle(context.Background(), nominationRequest("expert.knowledge.get", `{"expertId":"`+id+`"}`))
	if !resp.OK {
		t.Fatalf("knowledge.get = %+v", resp.Error)
	}
	var body struct {
		CollectionID string `json:"collectionId"`
		Missing      bool   `json:"missing"`
	}
	if err := json.Unmarshal(mustJSON(resp.Payload), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Missing || body.CollectionID != "" {
		t.Fatalf("empty expert must be missing without collectionId: %+v", body)
	}
}

func TestProjectKBDocumentRejectsIncompleteOCRCoverage(t *testing.T) {
	raw := mixedLayerScanPDF(t)
	path := filepath.Join(t.TempDir(), "mixed.pdf")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(nil, "test")
	svc := ocrapp.New(ocrapp.NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	svc.SetRenderPDF(nil)
	e.SetOCR(svc)
	_, err := projectKBDocument(context.Background(), e, m8core.KBDocument{
		ContentRef: path, SHA256: m8app.SourceDigest(raw), MediaType: "application/pdf",
	})
	if err == nil || !errors.Is(err, m8app.ErrKBIndexFailed) {
		t.Fatalf("incomplete coverage must fail ingest, got %v", err)
	}
	if !strings.Contains(err.Error(), "文档识别覆盖不完整") {
		t.Fatalf("ingest must stay Chinese and honest: %v", err)
	}
}

func TestProjectKBDocumentRejectsUnwiredMixedPDF(t *testing.T) {
	raw := mixedLayerScanPDF(t)
	path := filepath.Join(t.TempDir(), "mixed.pdf")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := projectKBDocument(context.Background(), nil, m8core.KBDocument{
		ContentRef: path, SHA256: m8app.SourceDigest(raw), MediaType: "application/pdf",
	})
	if err == nil || !errors.Is(err, m8app.ErrKBIndexFailed) {
		t.Fatalf("unwired mixed PDF must not index as complete, got %v", err)
	}
	if !strings.Contains(err.Error(), "文档识别覆盖不完整") {
		t.Fatalf("unwired ingest must stay Chinese and honest: %v", err)
	}
}

func TestProjectKBDocumentRejectsRelativePathInChinese(t *testing.T) {
	_, err := projectKBDocument(context.Background(), nil, m8core.KBDocument{
		ContentRef: "notes.md", SHA256: "deadbeef", MediaType: "text/plain",
	})
	if err == nil || !errors.Is(err, m8app.ErrKBIndexFailed) {
		t.Fatalf("relative content_ref must fail ingest, got %v", err)
	}
	if !strings.Contains(err.Error(), "绝对路径") || strings.Contains(err.Error(), "content_ref must") {
		t.Fatalf("relative ingest path must stay Chinese: %v", err)
	}
}

func TestProjectKBDocumentRejectsChangedDigestInChinese(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("周报正文"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := projectKBDocument(context.Background(), nil, m8core.KBDocument{
		ContentRef: path, SHA256: "not-the-current-digest", MediaType: "text/markdown",
	})
	if err == nil || !errors.Is(err, m8app.ErrKBIndexFailed) {
		t.Fatalf("changed digest must fail ingest, got %v", err)
	}
	if !strings.Contains(err.Error(), "已被修改") || strings.Contains(err.Error(), "source digest changed") {
		t.Fatalf("changed digest must stay Chinese: %v", err)
	}
}

func TestIngestFailReasonMapsParserEnglish(t *testing.T) {
	got := ingestFailReason(fmt.Errorf("document parser is busy; retry this document"))
	if !strings.Contains(got, "文档解析正忙") || strings.Contains(got, "parser is busy") {
		t.Fatalf("busy parser must stay Chinese: %q", got)
	}
	got = ingestFailReason(doctext.ErrBudgetExceeded)
	if !strings.Contains(got, "超过解析上限") || strings.Contains(got, "budget exceeded") {
		t.Fatalf("budget ingest must stay Chinese: %q", got)
	}
}

func TestLocalizeStoredKBFailReasonMapsLegacyEnglish(t *testing.T) {
	got := localizeStoredKBFailReason("无法抽出正文：parse function not configured")
	if !strings.Contains(got, "未配置正文解析") || strings.Contains(got, "parse function") {
		t.Fatalf("stored KB fail must stay Chinese: %q", got)
	}
}

func TestLocalizeKBSourceDropsEnglish(t *testing.T) {
	src := localizeKBSource(m8app.KBSource{
		Error: "source changed during parsing",
		Versions: []m8app.KBSourceVersion{{
			Error: "parse function not configured",
		}},
	})
	if strings.Contains(src.Error, "source changed") || !strings.Contains(src.Error, "已被修改") {
		t.Fatalf("source error must stay Chinese: %q", src.Error)
	}
	if strings.Contains(src.Versions[0].Error, "parse function") || !strings.Contains(src.Versions[0].Error, "未配置正文解析") {
		t.Fatalf("version error must stay Chinese: %q", src.Versions[0].Error)
	}
}

func TestKBIndexFailMessageStripsEnglishSuffix(t *testing.T) {
	err := fmt.Errorf("%w: source digest changed", m8app.ErrKBIndexFailed)
	msg := kbIndexFailMessage(err)
	if strings.Contains(msg, "source digest") || !strings.Contains(msg, "无法抽出正文") || !strings.Contains(msg, "已被修改") {
		t.Fatalf("bridge KB fail must stay Chinese: %q", msg)
	}
}

func TestProjectKBDocumentRejectsEmptyBodyInChinese(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.md")
	if err := os.WriteFile(path, []byte("   \n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := projectKBDocument(context.Background(), nil, m8core.KBDocument{
		ContentRef: path, SHA256: m8app.SourceDigest([]byte("   \n")), MediaType: "text/markdown",
	})
	if err == nil || !errors.Is(err, m8app.ErrKBIndexFailed) {
		t.Fatalf("empty body must fail ingest, got %v", err)
	}
	if !strings.Contains(err.Error(), "无法抽取正文") || strings.Contains(err.Error(), "no extractable") || strings.Contains(err.Error(), "no non-empty") {
		t.Fatalf("empty ingest must stay Chinese: %v", err)
	}
}

func TestExpertKnowledgeDeleteStopsSearch(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "kb-delete.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	kb := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	e := NewEngine(nil, "test")
	e.SetM8SliceServices(kb, nil, nil)
	expert := ulid.Make().String()
	coll, err := kb.EnsureExpertCollection(ctx, expert)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pneumatic.md")
	if err := os.WriteFile(path, []byte("pneumatic retraction procedure"), 0600); err != nil {
		t.Fatal(err)
	}
	ingested, err := kb.IngestLocalSource(ctx, m8app.KBLocalSourceInput{CollectionID: coll.CollectionID, Path: path, MediaType: "text/markdown"})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"expertId": expert, "sourceId": ingested.Source.SourceID, "expectedRevision": ingested.Source.Revision,
	})
	resp := e.Handle(ctx, nominationRequest("expert.knowledge.delete", string(body)))
	if !resp.OK {
		t.Fatalf("delete %#v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Payload)
	if strings.Contains(string(raw), "tombstone:deleted") {
		t.Fatalf("tombstone must stay Chinese: %s", raw)
	}
	after, err := kb.Search(ctx, m8app.KBSearchInput{ExpertID: expert, Query: "pneumatic"})
	if err != nil || len(after.Hits) != 0 {
		t.Fatalf("deleted source leaked search %+v %v", after, err)
	}
}
