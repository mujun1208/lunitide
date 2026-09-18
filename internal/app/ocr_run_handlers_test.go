package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oklog/ulid/v2"
)

func TestOCRRunListIsEmptyUntilRepositoryExists(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	user := e.Handle(context.Background(), validRequest("ocr.run.list", `{"scopeKind":"user"}`))
	if !user.OK {
		t.Fatalf("user list %+v", user.Error)
	}
	var payload struct {
		Items      []any   `json:"items"`
		NextCursor *string `json:"nextCursor"`
	}
	raw, err := json.Marshal(user.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil || len(payload.Items) != 0 || payload.NextCursor != nil {
		t.Fatalf("empty run list required %+v err=%v", payload, err)
	}
	withID := e.Handle(context.Background(), validRequest("ocr.run.list", `{"scopeKind":"user","scopeId":"`+ulid.Make().String()+`"}`))
	if withID.OK || withID.Error == nil || withID.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("user+scopeId %+v", withID)
	}
	project := e.Handle(context.Background(), validRequest("ocr.run.list", `{"scopeKind":"project","scopeId":"`+ulid.Make().String()+`"}`))
	if project.OK || project.Error == nil || project.Error.Code != "OCR_SCOPE_FORBIDDEN" || project.Error.Retryable {
		t.Fatalf("project list %+v", project)
	}
}

func TestOCRRunGetAndArtifactReadDoNotLeakPaths(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	id := ulid.Make().String()
	got := e.Handle(context.Background(), validRequest("ocr.run.get", `{"runId":"`+id+`"}`))
	if got.OK || got.Error == nil || got.Error.Code != "OCR_RUN_NOT_FOUND" || got.Error.Retryable {
		t.Fatalf("run.get %+v", got)
	}
	read := e.Handle(context.Background(), validRequest("ocr.artifact.read", `{"artifactId":"`+id+`","offset":0,"limit":4096}`))
	if read.OK || read.Error == nil || read.Error.Code != "OCR_ARTIFACT_MISSING" || read.Error.Retryable {
		t.Fatalf("artifact.read %+v", read)
	}
	path := e.Handle(context.Background(), validRequest("ocr.artifact.read", `{"artifactId":"`+id+`","offset":0,"limit":4096,"path":"C:\\\\ocr\\\\out.txt"}`))
	if path.OK || path.Error == nil || path.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("path must be rejected %+v", path)
	}
}

func TestOCRResultCompatibility(t *testing.T) {
	TestOCRRunGetAndArtifactReadDoNotLeakPaths(t)
}

func TestOCRRunListGetAndArtifactFromStore(t *testing.T) {
	e, store := newMediaEngine(t, true)
	ctx := context.Background()
	owner := e.memorySubjectID()
	runID, artifactID, err := store.OCRPersistRecognition(ctx, owner, "user", owner, []byte("png"), []byte("识别正文"), "ppocr", 1, true, false)
	if err != nil {
		t.Fatal(err)
	}
	listed := e.Handle(ctx, validRequest("ocr.run.list", `{"scopeKind":"user"}`))
	if !listed.OK {
		t.Fatalf("list %+v", listed.Error)
	}
	raw, _ := json.Marshal(listed.Payload)
	var listPayload struct {
		Items []struct {
			RunID      string `json:"runId"`
			ArtifactID string `json:"artifactId"`
			Status     string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &listPayload); err != nil || len(listPayload.Items) != 1 || listPayload.Items[0].RunID != runID || listPayload.Items[0].ArtifactID != artifactID || listPayload.Items[0].Status != "succeeded" {
		t.Fatalf("list payload %s err=%v", raw, err)
	}
	got := e.Handle(ctx, validRequest("ocr.run.get", `{"runId":"`+runID+`"}`))
	if !got.OK {
		t.Fatalf("get %+v", got.Error)
	}
	read := e.Handle(ctx, validRequest("ocr.artifact.read", `{"artifactId":"`+artifactID+`","offset":0,"limit":4096}`))
	if !read.OK {
		t.Fatalf("read %+v", read.Error)
	}
	var chunk struct {
		Base64 string `json:"base64"`
		EOF    bool   `json:"eof"`
	}
	raw, _ = json.Marshal(read.Payload)
	if err := json.Unmarshal(raw, &chunk); err != nil || !chunk.EOF {
		t.Fatalf("chunk %s err=%v", raw, err)
	}
}

func TestOCRPackGetAndNoticeFromStore(t *testing.T) {
	e, store := newMediaEngine(t, true)
	ctx := context.Background()
	resp := e.Handle(ctx, validRequest("ocr.pack.get", `{"packId":"paddleocr-vl-1.6"}`))
	if !resp.OK {
		t.Fatalf("get %+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Payload)
	var snap struct {
		Pack struct {
			Availability string `json:"availability"`
			Revision     int64  `json:"revision"`
		} `json:"pack"`
		Gate struct {
			InstallAllowed bool    `json:"installAllowed"`
			ReasonCode     *string `json:"reasonCode"`
		} `json:"gate"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil || snap.Pack.Availability != "not_installed" || snap.Pack.Revision != 1 || snap.Gate.InstallAllowed || snap.Gate.ReasonCode == nil || *snap.Gate.ReasonCode != "NO_VERIFIED_RUNTIME_PROFILE" {
		t.Fatalf("pack get %s err=%v", raw, err)
	}
	manifest := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := store.OCRRetainNotice(ctx, "paddleocr-vl-1.6", "1.6.0", manifest, "NOTICE retained offline", "cas:notice"); err != nil {
		t.Fatal(err)
	}
	list := e.Handle(ctx, validRequest("ocr.pack.notice.list", `{"packId":"paddleocr-vl-1.6"}`))
	if !list.OK {
		t.Fatalf("notice list %+v", list.Error)
	}
	raw, _ = json.Marshal(list.Payload)
	var listed struct {
		Items []struct {
			Version string `json:"version"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil || len(listed.Items) != 1 || listed.Items[0].Version != "1.6.0" {
		t.Fatalf("notice list %s err=%v", raw, err)
	}
	read := e.Handle(ctx, validRequest("ocr.pack.notice.read", `{"packId":"paddleocr-vl-1.6","manifestDigest":"`+manifest+`","offset":0,"limit":4096}`))
	if !read.OK {
		t.Fatalf("notice read %+v", read.Error)
	}
}
