package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/asset"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

func assetUpgradeStage(t *testing.T, e *Engine, id string, index int, last bool, text string) bridge.Response {
	t.Helper()
	return handleTemplateFileStage(e, context.Background(), bridge.Request{ID: ulid.Make().String(), TraceID: ulid.Make().String(), Method: "template.file.stage", Payload: mustJSON(map[string]any{"uploadId": id, "fileName": "template.dot", "index": index, "last": last, "contentBase64": base64.StdEncoding.EncodeToString([]byte(text))})})
}
func assetUpgradeCreateRequest(uploadID string) bridge.Request {
	p := map[string]any{"name": "Template", "templateType": "document", "documentType": "业务蓝图文档", "description": "Template evidence", "fileName": "template.dot"}
	if uploadID != "" {
		p["uploadId"] = uploadID
	} else {
		p["contentBase64"] = base64.StdEncoding.EncodeToString([]byte("template content"))
	}
	return bridge.Request{ID: ulid.Make().String(), TraceID: ulid.Make().String(), Method: "template.create", IdempotencyKey: "asset-create", Payload: mustJSON(p)}
}
func TestAssetUpgradeNonLastRetryPreservesContentAndReceipt(t *testing.T) {
	e := &Engine{}
	id := ulid.Make().String()
	defer e.finishTemplateStage(id)
	first := assetUpgradeStage(t, e, id, 0, false, "FIRST-")
	retry := assetUpgradeStage(t, e, id, 0, false, "FIRST-")
	if !first.OK || !retry.OK || !reflect.DeepEqual(first.Payload, retry.Payload) {
		t.Fatalf("first=%+v retry=%+v", first, retry)
	}
	if last := assetUpgradeStage(t, e, id, 1, true, "LAST"); !last.OK {
		t.Fatal(last.Error)
	}
	data, err := e.consumeTemplateStage(id)
	if err != nil || string(data) != "FIRST-LAST" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}
func TestAssetUpgradeRejectsIncompleteOutOfOrderAndChangedChunks(t *testing.T) {
	e := &Engine{}
	id := ulid.Make().String()
	defer e.finishTemplateStage(id)
	if out := assetUpgradeStage(t, e, id, 1, true, "LAST"); out.OK {
		t.Fatal("out-of-order first chunk accepted")
	}
	if out := assetUpgradeStage(t, e, id, 0, false, "PARTIAL"); !out.OK {
		t.Fatal(out.Error)
	}
	if _, err := e.consumeTemplateStage(id); err == nil {
		t.Fatal("incomplete upload consumed")
	}
	for _, last := range []bool{false, true} {
		if out := assetUpgradeStage(t, e, id, 0, last, "CHANGED"); out.OK || out.Error.Code != "IDEMPOTENCY_CONFLICT" {
			t.Fatalf("changed chunk=%+v", out)
		}
	}
	if out := assetUpgradeStage(t, e, id, 2, true, "GAP"); out.OK {
		t.Fatal("gap accepted")
	}
}
func TestAssetUpgradeDetectsStagedFileTampering(t *testing.T) {
	e := &Engine{}
	id := ulid.Make().String()
	defer e.finishTemplateStage(id)
	if out := assetUpgradeStage(t, e, id, 0, true, "original"); !out.OK {
		t.Fatal(out.Error)
	}
	up := e.templateStage().uploads[id]
	if err := os.WriteFile(up.path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.consumeTemplateStage(id); err == nil {
		t.Fatal("tampered stage consumed")
	}
}
func TestAssetUpgradeSameCreateReplaysOneFile(t *testing.T) {
	e := &Engine{assets: &mockTemplateStore{}, templateFiles: &memTemplateFiles{files: map[string][]byte{}}}
	r := assetUpgradeCreateRequest("")
	first := handleTemplateCreate(e, context.Background(), r)
	second := handleTemplateCreate(e, context.Background(), r)
	if !first.OK || !second.OK || !reflect.DeepEqual(first.Payload, second.Payload) {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if len(e.templateFiles.(*memTemplateFiles).files) != 1 {
		t.Fatal("duplicate file")
	}
	var payload map[string]any
	_ = json.Unmarshal(r.Payload, &payload)
	payload["name"] = "Changed"
	r.Payload = mustJSON(payload)
	if out := handleTemplateCreate(e, context.Background(), r); out.OK || out.Error.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("out=%+v", out)
	}
}
func TestAssetUpgradeReplayAfterDatabaseReopenAndStageCleanup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "assets.db")
	store, err := storage.OpenTemplated(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	files := &memTemplateFiles{files: map[string][]byte{}}
	e := &Engine{assets: store, templateFiles: files}
	id := ulid.Make().String()
	defer e.finishTemplateStage(id)
	if out := assetUpgradeStage(t, e, id, 0, true, "content"); !out.OK {
		t.Fatal(out.Error)
	}
	r := assetUpgradeCreateRequest(id)
	first := handleTemplateCreate(e, ctx, r)
	if !first.OK {
		t.Fatal(first.Error)
	}
	if _, err := e.consumeTemplateStage(id); err == nil {
		t.Fatal("stage not cleaned")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.OpenTemplated(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	second := handleTemplateCreate(&Engine{assets: reopened, templateFiles: files}, ctx, r)
	if !second.OK || !reflect.DeepEqual(first.Payload, second.Payload) || len(files.files) != 1 {
		t.Fatalf("first=%+v second=%+v files=%d", first, second, len(files.files))
	}
}

type assetUpgradeFailureStore struct {
	mockTemplateStore
	commitThenFail bool
}

func (s *assetUpgradeFailureStore) CreateAssetTemplateIdempotent(ctx context.Context, key, digest string, tpl asset.AssetTemplate) (asset.AssetTemplate, error) {
	if s.commitThenFail {
		_, _ = s.mockTemplateStore.CreateAssetTemplateIdempotent(ctx, key, digest, tpl)
	}
	return asset.AssetTemplate{}, errors.New("injected commit acknowledgement failure")
}
func TestAssetUpgradeDatabaseFailureCleanupAndLostAcknowledgement(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit-ack-lost"}[committed], func(t *testing.T) {
			files := &memTemplateFiles{files: map[string][]byte{}}
			e := &Engine{assets: &assetUpgradeFailureStore{commitThenFail: committed}, templateFiles: files}
			out := handleTemplateCreate(e, context.Background(), assetUpgradeCreateRequest(""))
			want := 0
			if committed {
				want = 1
			}
			if out.OK != committed || len(files.files) != want {
				t.Fatalf("committed=%v out=%+v files=%d", committed, out, len(files.files))
			}
		})
	}
}
