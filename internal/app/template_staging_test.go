package app

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/asset"
	"github.com/oklog/ulid/v2"
)

type mockTemplateStore struct {
	created asset.AssetTemplate
}

func (m *mockTemplateStore) CreateAssetTemplate(_ context.Context, tpl asset.AssetTemplate) (asset.AssetTemplate, error) {
	if tpl.ID == "" {
		tpl.ID = ulid.Make().String()
	}
	if tpl.TemplateCode == "" {
		tpl.TemplateCode = "TPL00001"
	}
	tpl.Version = 1
	m.created = tpl
	return tpl, nil
}
func (m *mockTemplateStore) GetAssetTemplate(context.Context, string) (asset.AssetTemplate, error) {
	return asset.AssetTemplate{}, asset.ErrNotFound
}
func (m *mockTemplateStore) ListAssetTemplates(context.Context, asset.Filter) ([]asset.AssetTemplate, error) {
	return nil, nil
}
func (m *mockTemplateStore) UpdateAssetTemplateStatus(context.Context, string, int64, asset.Status) (asset.AssetTemplate, error) {
	return asset.AssetTemplate{}, nil
}
func (m *mockTemplateStore) DeleteAssetTemplate(context.Context, string, int64) error { return nil }

type memTemplateFiles struct {
	files map[string][]byte
}

func (m *memTemplateFiles) WriteFile(_ context.Context, name string, content []byte) error {
	m.files[name] = append([]byte(nil), content...)
	return nil
}
func (m *memTemplateFiles) ReadFile(_ context.Context, name string) ([]byte, error) {
	return m.files[name], nil
}
func (m *memTemplateFiles) DeleteFile(_ context.Context, name string) error {
	delete(m.files, name)
	return nil
}

func TestHandleTemplateFileStageAndCreate(t *testing.T) {
	t.Parallel()
	engine := &Engine{
		assets:        &mockTemplateStore{},
		templateFiles: &memTemplateFiles{files: map[string][]byte{}},
	}
	uploadID := ulid.Make().String()
	chunk := base64.StdEncoding.EncodeToString([]byte("hello-dot"))
	stageReq := bridge.Request{
		ID: ulid.Make().String(), TraceID: ulid.Make().String(),
		Method: string(bridge.MethodTemplateFileStage),
		Payload: mustJSON(map[string]any{
			"uploadId": uploadID, "fileName": "blueprint.dot", "last": true, "contentBase64": chunk,
		}),
	}
	stageResp := handleTemplateFileStage(engine, context.Background(), stageReq)
	if !stageResp.OK {
		t.Fatalf("stage failed: %+v", stageResp.Error)
	}
	createReq := bridge.Request{
		ID: ulid.Make().String(), TraceID: ulid.Make().String(),
		Method: string(bridge.MethodTemplateCreate),
		Payload: mustJSON(map[string]any{
			"name": "蓝图文档模板", "templateType": "document", "documentType": "业务蓝图文档",
			"description": "SAP业务蓝图文档模板", "fileName": "blueprint.dot", "uploadId": uploadID,
		}),
	}
	createResp := handleTemplateCreate(engine, context.Background(), createReq)
	if !createResp.OK {
		t.Fatalf("create failed: %+v", createResp.Error)
	}
	store := engine.assets.(*mockTemplateStore)
	if store.created.FileName != "blueprint.dot" {
		t.Fatalf("file name = %q", store.created.FileName)
	}
	if len(engine.templateFiles.(*memTemplateFiles).files) != 1 {
		t.Fatalf("expected one stored template file")
	}
	for _, content := range engine.templateFiles.(*memTemplateFiles).files {
		if string(content) != "hello-dot" {
			t.Fatalf("stored content = %q", content)
		}
	}
}

func TestHandleTemplateCreateRejectsMissingAttachment(t *testing.T) {
	t.Parallel()
	engine := &Engine{
		assets:        &mockTemplateStore{},
		templateFiles: &memTemplateFiles{files: map[string][]byte{}},
	}
	req := bridge.Request{
		ID: ulid.Make().String(), TraceID: ulid.Make().String(),
		Method: string(bridge.MethodTemplateCreate),
		Payload: mustJSON(map[string]any{
			"name": "x", "templateType": "document", "documentType": "业务蓝图文档",
			"description": "d", "fileName": "a.dot",
		}),
	}
	resp := handleTemplateCreate(engine, context.Background(), req)
	if resp.OK || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("expected schema invalid, got %+v", resp)
	}
	_ = attachmentapp.MaxFileSize
}

func TestHandleTemplateFileStageLastChunkRetryKeepsBytes(t *testing.T) {
	t.Parallel()
	engine := &Engine{
		assets:        &mockTemplateStore{},
		templateFiles: &memTemplateFiles{files: map[string][]byte{}},
	}
	uploadID := ulid.Make().String()
	first := base64.StdEncoding.EncodeToString([]byte("hello-"))
	last := base64.StdEncoding.EncodeToString([]byte("dot"))
	stage := func(chunk string, index int, lastChunk bool) {
		t.Helper()
		resp := handleTemplateFileStage(engine, context.Background(), bridge.Request{
			ID: ulid.Make().String(), TraceID: ulid.Make().String(),
			Method: string(bridge.MethodTemplateFileStage),
			Payload: mustJSON(map[string]any{
				"uploadId": uploadID, "fileName": "blueprint.dot", "index": index,
				"last": lastChunk, "contentBase64": chunk,
			}),
		})
		if !resp.OK {
			t.Fatalf("stage failed: %+v", resp.Error)
		}
	}
	stage(first, 0, false)
	stage(last, 1, true)
	stage(last, 1, true)
	firstRead, err := engine.consumeTemplateStage(uploadID)
	if err != nil {
		t.Fatal(err)
	}
	retryRead, err := engine.consumeTemplateStage(uploadID)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstRead) != "hello-dot" || string(retryRead) != "hello-dot" {
		t.Fatalf("staged = %q / %q", firstRead, retryRead)
	}
	createResp := handleTemplateCreate(engine, context.Background(), bridge.Request{
		ID: ulid.Make().String(), TraceID: ulid.Make().String(),
		Method: string(bridge.MethodTemplateCreate),
		Payload: mustJSON(map[string]any{
			"name": "蓝图文档模板", "templateType": "document", "documentType": "业务蓝图文档",
			"description": "SAP业务蓝图文档模板", "fileName": "blueprint.dot", "uploadId": uploadID,
		}),
	})
	if !createResp.OK {
		t.Fatalf("create failed: %+v", createResp.Error)
	}
}
