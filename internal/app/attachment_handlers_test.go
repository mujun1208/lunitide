package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/domain/attachment"
)

type getJPEGStore struct {
	mu          sync.Mutex
	attachments map[string]*attachment.Attachment
}

func (s *getJPEGStore) CreateAttachment(_ context.Context, a attachment.Attachment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attachments == nil {
		s.attachments = map[string]*attachment.Attachment{}
	}
	cp := a
	s.attachments[a.ID] = &cp
	return nil
}

func (s *getJPEGStore) GetAttachment(_ context.Context, id string) (*attachment.Attachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.attachments[id]
	if a == nil {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (s *getJPEGStore) GetAttachmentForDeletion(ctx context.Context, id string) (*attachment.Attachment, error) {
	return s.GetAttachment(ctx, id)
}

func (s *getJPEGStore) ListAttachmentsByProject(context.Context, string, int) ([]attachment.Attachment, error) {
	return nil, nil
}

func (s *getJPEGStore) ListAttachmentsBySession(context.Context, string, int) ([]attachment.Attachment, error) {
	return nil, nil
}

func (s *getJPEGStore) UpdateParseResult(_ context.Context, id string, status attachment.ParseStatus, errCode string, parsedText string, parsedTextBytes int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.attachments[id]
	if a == nil {
		return nil
	}
	a.ParseStatus = status
	a.ParseErrorCode = errCode
	a.ParsedText = parsedText
	a.ParsedTextBytes = parsedTextBytes
	return nil
}

func (s *getJPEGStore) DeleteAttachment(context.Context, string) error { return nil }
func (s *getJPEGStore) ListPendingAttachmentFileCleanup(context.Context, int) ([]string, error) {
	return nil, nil
}
func (s *getJPEGStore) CompleteAttachmentFileCleanup(context.Context, string) error { return nil }

func TestAttachmentGetReturnsVisionJPEGHead(t *testing.T) {
	store := &getJPEGStore{}
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetAttachmentService(attachmentapp.NewService(store, attachmentapp.NewDirFileStorage(t.TempDir())))
	jpeg := make([]byte, 2048)
	jpeg[0], jpeg[1], jpeg[2] = 0xff, 0xd8, 0xff
	att, err := e.IngestAttachment(context.Background(), attachmentapp.IngestFileRequest{
		ProjectID:    "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		OriginalName: "BIRETURN.jpg",
		MIME:         "image/jpeg",
		Content:      jpeg,
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	resp := e.Handle(context.Background(), validRequest("attachment.get", `{"attachmentId":"`+att.ID+`"}`))
	if !resp.OK {
		t.Fatalf("attachment.get = %#v", resp)
	}
	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body["parseStatus"] != "succeeded" || body["parseErrorCode"] != "" {
		t.Fatalf("parse = %s", raw)
	}
	encoded, _ := body["contentBase64"].(string)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) < 3 || data[0] != 0xff || data[1] != 0xd8 || data[2] != 0xff {
		t.Fatalf("contentBase64 jpeg head missing: err=%v len=%d", err, len(data))
	}
}

func TestInternalImportPathCopiesLocalText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "需求.txt")
	if err := os.WriteFile(path, []byte("客户与商机"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetAttachmentService(attachmentapp.NewService(&getJPEGStore{}, attachmentapp.NewDirFileStorage(t.TempDir())))
	body, err := json.Marshal(map[string]string{
		"projectId":    "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		"sessionId":    "01ARZ3NDEKTSV4RRFFQ69G5FAW",
		"path":         path,
		"originalName": "需求.txt",
		"mime":         "text/plain",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := e.Handle(context.Background(), validRequest("internal.attachment.importPath", string(body)))
	if !resp.OK {
		t.Fatalf("import = %#v", resp.Error)
	}
	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var imported map[string]any
	if err := json.Unmarshal(raw, &imported); err != nil {
		t.Fatal(err)
	}
	id, _ := imported["attachmentId"].(string)
	got := e.Handle(context.Background(), validRequest("attachment.get", `{"attachmentId":"`+id+`"}`))
	if !got.OK {
		t.Fatalf("get = %#v", got.Error)
	}
	view, err := json.Marshal(got.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(view), "客户与商机") {
		t.Fatalf("parsed text missing: %s", view)
	}
}
