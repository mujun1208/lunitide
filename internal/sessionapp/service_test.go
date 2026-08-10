package sessionapp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/providerapp"
)

type testClock struct{ now time.Time }

func (c testClock) Now() time.Time { return c.now }

type memory struct {
	mu      sync.Mutex
	next    int
	records map[string]providerapp.Record
	audits  []providerapp.Audit
}

func (m *memory) ListSessions(context.Context, session.Filter) ([]session.Session, error) {
	return nil, nil
}
func (m *memory) DoSession(_ context.Context, fn func(Tx) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fn(m)
}
func (m *memory) CreateSession(_ context.Context, v session.Session) (session.Session, error) {
	m.next++
	v.ID = []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW"}[m.next-1]
	v.Status = session.StatusActive
	v.Version = 1
	v.CreatedAt = time.Unix(int64(m.next), 0).UTC()
	v.UpdatedAt = v.CreatedAt
	return v, nil
}
func (m *memory) Idempotency(_ context.Context, op, key string, _ time.Time) (providerapp.Record, bool, error) {
	r, ok := m.records[op+"\x00"+key]
	return r, ok, nil
}
func (m *memory) PutIdempotency(_ context.Context, r providerapp.Record) error {
	m.records[r.Operation+"\x00"+r.Key] = r
	return nil
}
func (m *memory) PutAudit(_ context.Context, a providerapp.Audit) error {
	m.audits = append(m.audits, a)
	return nil
}

func TestCreateReplayConflictAndDistinctAuditIDs(t *testing.T) {
	m := &memory{records: map[string]providerapp.Record{}}
	s := New(m, m)
	s.clock = testClock{time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	req := struct{ ProjectID, Title string }{"01ARZ3NDEKTSV4RRFFQ69G5FAA", "Alpha"}
	value := session.Session{ProjectID: req.ProjectID, Title: req.Title}
	a, err := s.Create(context.Background(), "key-one", "test", req, value)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create(context.Background(), "key-one", "test", req, value)
	if err != nil || a.ID != b.ID || m.next != 1 || len(m.audits) != 1 {
		t.Fatalf("replay a=%#v b=%#v creates=%d audits=%d err=%v", a, b, m.next, len(m.audits), err)
	}
	changed := req
	changed.Title = "Beta"
	if _, err = s.Create(context.Background(), "key-one", "test", changed, session.Session{ProjectID: req.ProjectID, Title: "Beta"}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict=%v", err)
	}
	if _, err = s.Create(context.Background(), "key-two", "test", req, value); err != nil {
		t.Fatal(err)
	}
	if len(m.audits) != 2 || m.audits[0].ID == m.audits[1].ID {
		t.Fatalf("audit IDs=%#v", m.audits)
	}
}

func TestInvalidIdempotencyKeys(t *testing.T) {
	m := &memory{records: map[string]providerapp.Record{}}
	s := New(m, m)
	for _, key := range []string{"", "has space", "é", "nul\x00byte", string(make([]byte, 129))} {
		if _, err := s.Create(context.Background(), key, "test", struct{}{}, session.Session{}); !errors.Is(err, ErrIdempotencyKeyRequired) {
			t.Errorf("key %q: %v", key, err)
		}
	}
}
