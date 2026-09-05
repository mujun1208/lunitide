package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
