package memoryapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/memory"
)

type mockMemReader struct {
	mem     *memory.Memory
	memList []memory.Memory
	err     error
}

func (m *mockMemReader) GetMemory(_ context.Context, _ string) (*memory.Memory, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.mem, nil
}
func (m *mockMemReader) ListMemoriesByProject(_ context.Context, _ string, _ string, _ int) ([]memory.Memory, error) {
	return m.memList, m.err
}

type mockMemWriter struct {
	updatedContent string
	deletedID      string
	incrementedID  string
	err            error
}

func (m *mockMemWriter) UpdateMemory(_ context.Context, _, content string) error {
	m.updatedContent = content
	return m.err
}
func (m *mockMemWriter) DeleteMemory(_ context.Context, id string) error {
	m.deletedID = id
	return m.err
}
func (m *mockMemWriter) IncrementAccessCount(_ context.Context, id string) error {
	m.incrementedID = id
	return m.err
}

type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time { return f.t }

func memNow() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

func makeMem(id string, confidence memory.Confidence) *memory.Memory {
	return &memory.Memory{
		ID:         id,
		ProjectID:  "01ARZ3NDEKTSV4RRFFQ69G5FAA",
		Layer:      memory.LayerSemantic,
		Scope:      memory.ScopeProject,
		Key:        "fact-1",
		Content:    "The sky is blue",
		Confidence: confidence,
		CreatedAt:  memNow(),
		UpdatedAt:  memNow(),
	}
}

func newSvc(r *mockMemReader, w *mockMemWriter) *Service {
	s := New(r, w)
	s.clock = fixedClock{t: memNow()}
	return s
}

func TestGetNotFound(t *testing.T) {
	s := newSvc(&mockMemReader{mem: nil}, &mockMemWriter{})
	if _, err := s.Get(context.Background(), "missing"); err != ErrMemoryNotFound {
		t.Fatalf("expected ErrMemoryNotFound, got %v", err)
	}
}

func TestGetSuccessIncrementsAccess(t *testing.T) {
	r := &mockMemReader{mem: makeMem("01ARZ3NDEKTSV4RRFFQ69G5FAV", 0.9)}
	w := &mockMemWriter{}
	s := newSvc(r, w)
	m, err := s.Get(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("expected id, got %s", m.ID)
	}
	if w.incrementedID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("expected access count incremented, got %s", w.incrementedID)
	}
}

func TestGetDeletesExpiredMemory(t *testing.T) {
	expired := makeMem("01ARZ3NDEKTSV4RRFFQ69G5FAV", 0.5)
	past := memNow().Add(-1 * time.Hour)
	expired.ExpiresAt = &past
	r := &mockMemReader{mem: expired}
	w := &mockMemWriter{}
	s := newSvc(r, w)
	if _, err := s.Get(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != ErrMemoryNotFound {
		t.Fatalf("expected ErrMemoryNotFound for expired, got %v", err)
	}
	if w.deletedID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("expected expired memory deleted, got %s", w.deletedID)
	}
}

func TestListByProject(t *testing.T) {
	r := &mockMemReader{memList: []memory.Memory{*makeMem("01ARZ3NDEKTSV4RRFFQ69G5FAV", 0.9), *makeMem("01ARZ3NDEKTSV4RRFFQ69G5FAW", 0.8)}}
	s := newSvc(r, &mockMemWriter{})
	ms, err := s.ListByProject(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA", memory.LayerSemantic)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 {
		t.Fatalf("expected 2 memories, got %d", len(ms))
	}
}

func TestSearchRankByConfidence(t *testing.T) {
	r := &mockMemReader{memList: []memory.Memory{
		*makeMem("01ARZ3NDEKTSV4RRFFQ69G5FAV", 0.5), // lower confidence
		*makeMem("01ARZ3NDEKTSV4RRFFQ69G5FAW", 0.9), // higher confidence
	}}
	s := newSvc(r, &mockMemWriter{})
	results, err := s.Search(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA", "sky")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "01ARZ3NDEKTSV4RRFFQ69G5FAW" {
		t.Fatalf("expected higher confidence first, got %s", results[0].ID)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	s := newSvc(&mockMemReader{}, &mockMemWriter{})
	results, err := s.Search(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if results != nil {
		t.Fatalf("expected nil for empty query, got %v", results)
	}
}

func TestSearchSkipsExpired(t *testing.T) {
	expired := makeMem("01ARZ3NDEKTSV4RRFFQ69G5FAV", 0.9)
	past := memNow().Add(-1 * time.Hour)
	expired.ExpiresAt = &past
	active := makeMem("01ARZ3NDEKTSV4RRFFQ69G5FAW", 0.8)
	r := &mockMemReader{memList: []memory.Memory{*expired, *active}}
	s := newSvc(r, &mockMemWriter{})
	results, err := s.Search(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA", "sky")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 active result, got %d", len(results))
	}
	if results[0].ID != "01ARZ3NDEKTSV4RRFFQ69G5FAW" {
		t.Fatalf("expected active memory, got %s", results[0].ID)
	}
}

func TestUpdateContentNotFound(t *testing.T) {
	r := &mockMemReader{mem: nil}
	s := newSvc(r, &mockMemWriter{})
	if err := s.UpdateContent(context.Background(), "missing", "new content"); err != ErrMemoryNotFound {
		t.Fatalf("expected ErrMemoryNotFound, got %v", err)
	}
}

func TestUpdateContentRejectsEmpty(t *testing.T) {
	r := &mockMemReader{mem: makeMem("01ARZ3NDEKTSV4RRFFQ69G5FAV", 0.9)}
	s := newSvc(r, &mockMemWriter{})
	if err := s.UpdateContent(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", ""); err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestUpdateContentRejectsOversize(t *testing.T) {
	r := &mockMemReader{mem: makeMem("01ARZ3NDEKTSV4RRFFQ69G5FAV", 0.9)}
	s := newSvc(r, &mockMemWriter{})
	huge := make([]byte, 65537)
	for i := range huge {
		huge[i] = 'a'
	}
	if err := s.UpdateContent(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", string(huge)); err == nil {
		t.Fatal("expected error for oversize content")
	}
}

func TestUpdateContentSuccess(t *testing.T) {
	r := &mockMemReader{mem: makeMem("01ARZ3NDEKTSV4RRFFQ69G5FAV", 0.9)}
	w := &mockMemWriter{}
	s := newSvc(r, w)
	if err := s.UpdateContent(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", "updated content"); err != nil {
		t.Fatal(err)
	}
	if w.updatedContent != "updated content" {
		t.Fatalf("expected content updated, got %s", w.updatedContent)
	}
}

func TestDeleteNotFound(t *testing.T) {
	r := &mockMemReader{mem: nil}
	s := newSvc(r, &mockMemWriter{})
	if err := s.Delete(context.Background(), "missing"); err != ErrMemoryNotFound {
		t.Fatalf("expected ErrMemoryNotFound, got %v", err)
	}
}

func TestDeleteSuccess(t *testing.T) {
	r := &mockMemReader{mem: makeMem("01ARZ3NDEKTSV4RRFFQ69G5FAV", 0.9)}
	w := &mockMemWriter{}
	s := newSvc(r, w)
	if err := s.Delete(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != nil {
		t.Fatal(err)
	}
	if w.deletedID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("expected memory deleted, got %s", w.deletedID)
	}
}

func TestPurgeExpired(t *testing.T) {
	expired := makeMem("01ARZ3NDEKTSV4RRFFQ69G5FAV", 0.9)
	past := memNow().Add(-1 * time.Hour)
	expired.ExpiresAt = &past
	active := makeMem("01ARZ3NDEKTSV4RRFFQ69G5FAW", 0.8)
	r := &mockMemReader{memList: []memory.Memory{*expired, *active}}
	w := &mockMemWriter{}
	s := newSvc(r, w)
	count, err := s.PurgeExpired(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 purged, got %d", count)
	}
	if w.deletedID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("expected expired memory deleted, got %s", w.deletedID)
	}
}

func TestPurgeExpiredNone(t *testing.T) {
	r := &mockMemReader{memList: []memory.Memory{*makeMem("01ARZ3NDEKTSV4RRFFQ69G5FAV", 0.9)}}
	w := &mockMemWriter{}
	s := newSvc(r, w)
	count, err := s.PurgeExpired(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 purged, got %d", count)
	}
}

func TestGetReaderUnavailable(t *testing.T) {
	s := &Service{}
	if _, err := s.Get(context.Background(), "x"); err == nil {
		t.Fatal("expected error for nil reader")
	}
}

func TestUpdateContentWriterUnavailable(t *testing.T) {
	r := &mockMemReader{mem: makeMem("01ARZ3NDEKTSV4RRFFQ69G5FAV", 0.9)}
	s := New(r, nil)
	s.clock = fixedClock{t: memNow()}
	if err := s.UpdateContent(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", "content"); err == nil {
		t.Fatal("expected error for nil writer")
	}
}

func TestUpdateContentPropagatesError(t *testing.T) {
	r := &mockMemReader{mem: makeMem("01ARZ3NDEKTSV4RRFFQ69G5FAV", 0.9)}
	boom := errors.New("storage failure")
	w := &mockMemWriter{err: boom}
	s := newSvc(r, w)
	if err := s.UpdateContent(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", "content"); err != boom {
		t.Fatalf("expected propagated error, got %v", err)
	}
}
