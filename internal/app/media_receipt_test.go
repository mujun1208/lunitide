package app

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func mediaPNG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func mediaGenerateEngine(t *testing.T, adapter llmadapter.Adapter) *Engine {
	t.Helper()
	p := videoTestProvider()
	p.Models = []provider.Model{{ModelID: "image-model", Kind: provider.KindImage, KindDefault: true}}
	e := NewEngineWithGateway(videoTestProviders{items: []provider.Provider{p}}, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return adapter, nil
	})
	return e
}

func TestInvokeMediaGeneratePersistsSupplierIDAndDoesNotResubmit(t *testing.T) {
	a := &catalogImageAdapter{out: llmadapter.MediaResult{
		ID: "job-77", URL: "https://cdn.example/job-77.png", Data: mediaPNG(t), MIME: "image/png",
	}}
	e := mediaGenerateEngine(t, a)
	ops := &memToolOps{}
	e.SetToolOperationStore(ops)
	runtime, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e.tools = runtime

	args := json.RawMessage(`{"prompt":"square","path":"generated.png"}`)
	out, err := e.invokeMediaGenerate(context.Background(), chatAttachmentSessionID, "image.generate", args)
	if err != nil || out.Artifact == nil || a.calls != 1 {
		t.Fatalf("first generate: %+v %v calls=%d", out, err, a.calls)
	}
	listed, err := ops.ListToolOperations(context.Background(), chatAttachmentSessionID, 10)
	if err != nil || len(listed) != 1 || listed[0].ExternalID != "job-77" || listed[0].State != modelfit.OpSucceeded {
		t.Fatalf("supplier id must be persisted on the receipt: %+v %v", listed, err)
	}

	again, err := e.invokeMediaGenerate(context.Background(), chatAttachmentSessionID, "image.generate", args)
	if err != nil || a.calls != 1 {
		t.Fatalf("receipt-loss retry must not resubmit: %+v %v calls=%d", again, err, a.calls)
	}
	if !strings.Contains(again.Output, "job-77") || !strings.Contains(again.Output, "未重复生成") {
		t.Fatalf("retry must query the existing job: %q", again.Output)
	}
	listed, err = ops.ListToolOperations(context.Background(), chatAttachmentSessionID, 10)
	if err != nil || len(listed) != 1 {
		t.Fatalf("retry must not open a second paid operation: %+v %v", listed, err)
	}
}

func TestInvokeMediaGenerateSaveFailureKeepsIDAndDoesNotResubmit(t *testing.T) {
	a := &catalogImageAdapter{out: llmadapter.MediaResult{
		ID: "job-88", Data: mediaPNG(t), MIME: "image/png",
	}}
	e := mediaGenerateEngine(t, a)
	ops := &memToolOps{}
	e.SetToolOperationStore(ops)

	args := json.RawMessage(`{"prompt":"circle"}`)
	_, err := e.invokeMediaGenerate(context.Background(), chatAttachmentSessionID, "image.generate", args)
	if err == nil || a.calls != 1 {
		t.Fatalf("save failure should surface after submit: %v calls=%d", err, a.calls)
	}
	listed, listErr := ops.ListToolOperations(context.Background(), chatAttachmentSessionID, 10)
	if listErr != nil || len(listed) != 1 || listed[0].ExternalID != "job-88" {
		t.Fatalf("paid job id must survive a local save failure: %+v %v", listed, listErr)
	}

	again, err := e.invokeMediaGenerate(context.Background(), chatAttachmentSessionID, "image.generate", args)
	if a.calls != 1 {
		t.Fatalf("save-failure retry resubmitted a paid job: calls=%d err=%v out=%+v", a.calls, err, again)
	}
	if !strings.Contains(again.Output, "job-88") || !strings.Contains(again.Output, "未重复生成") {
		t.Fatalf("must point at the existing remote job: %q %v", again.Output, err)
	}
}

func TestInvokeMediaGenerateSubmittedUnknownDoesNotResubmit(t *testing.T) {
	a := &catalogImageAdapter{err: context.DeadlineExceeded}
	e := mediaGenerateEngine(t, a)
	ops := &memToolOps{}
	e.SetToolOperationStore(ops)

	args := json.RawMessage(`{"prompt":"triangle"}`)
	_, err := e.invokeMediaGenerate(context.Background(), chatAttachmentSessionID, "image.generate", args)
	if err == nil || a.calls != 1 {
		t.Fatalf("uncertain submit should fail closed: %v calls=%d", err, a.calls)
	}
	listed, listErr := ops.ListToolOperations(context.Background(), chatAttachmentSessionID, 10)
	if listErr != nil || len(listed) != 1 || listed[0].State != modelfit.OpUnknown {
		t.Fatalf("submitted-but-unread receipt must stay unknown: %+v %v", listed, listErr)
	}

	again, err := e.invokeMediaGenerate(context.Background(), chatAttachmentSessionID, "image.generate", args)
	if a.calls != 1 {
		t.Fatalf("unknown window must not guess a second submit: calls=%d err=%v out=%+v", a.calls, err, again)
	}
	if !strings.Contains(again.Output, "未重复生成") || !strings.Contains(again.Output, "核实") {
		t.Fatalf("unknown window must ask to verify, not regenerate: %q %v", again.Output, err)
	}
}

func TestInvokeMediaGenerateDifferentPromptIsANewJob(t *testing.T) {
	a := &catalogImageAdapter{out: llmadapter.MediaResult{
		ID: "job-1", Data: mediaPNG(t), MIME: "image/png",
	}}
	e := mediaGenerateEngine(t, a)
	ops := &memToolOps{}
	e.SetToolOperationStore(ops)
	runtime, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e.tools = runtime

	if _, err = e.invokeMediaGenerate(context.Background(), chatAttachmentSessionID, "image.generate", json.RawMessage(`{"prompt":"red","path":"a.png"}`)); err != nil {
		t.Fatal(err)
	}
	a.out.ID = "job-2"
	if _, err = e.invokeMediaGenerate(context.Background(), chatAttachmentSessionID, "image.generate", json.RawMessage(`{"prompt":"blue","path":"b.png"}`)); err != nil {
		t.Fatal(err)
	}
	if a.calls != 2 {
		t.Fatalf("a different prompt is a new user request, not receipt loss: calls=%d", a.calls)
	}
}
