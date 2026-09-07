package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/domain/attachment"
)

type sessionAttachmentListStore struct {
	*getJPEGStore
	sessionCalls int
	projectCalls int
}

func (s *sessionAttachmentListStore) ListAttachmentsByProject(context.Context, string, int) ([]attachment.Attachment, error) {
	s.projectCalls++
	return nil, nil
}
func (s *sessionAttachmentListStore) ListAttachmentsBySession(_ context.Context, id string, _ int) ([]attachment.Attachment, error) {
	s.sessionCalls++
	return []attachment.Attachment{{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAC", ProjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", SessionID: id}, {ID: "01ARZ3NDEKTSV4RRFFQ69G5FAD", ProjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAB", SessionID: id}}, nil
}
func TestAttachmentListFiltersSessionBeforeLimitAndPreservesProjectScope(t *testing.T) {
	store := &sessionAttachmentListStore{getJPEGStore: &getJPEGStore{}}
	engine := NewEngine(providerRepositoryStub{}, "test")
	engine.SetAttachmentService(attachmentapp.NewService(store, attachmentapp.NewDirFileStorage(t.TempDir())))
	result := engine.Handle(context.Background(), validRequest("attachment.list", `{"projectId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","sessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAA","limit":200}`))
	if !result.OK {
		t.Fatalf("list failed: %+v", result)
	}
	raw, _ := json.Marshal(result.Payload)
	var payload struct {
		Items []attachmentDTO `json:"items"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 1 || store.sessionCalls != 1 || store.projectCalls != 0 {
		t.Fatalf("wrong scope: payload=%s session=%d project=%d", raw, store.sessionCalls, store.projectCalls)
	}
}
