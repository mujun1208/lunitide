package projectapp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/providerapp"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type projectMemory struct {
	mu       sync.Mutex
	next     int
	projects map[string]project.Project
	records  map[string]providerapp.Record
	audits   []providerapp.Audit
}

func (m *projectMemory) ListProjects(context.Context, project.Filter) ([]project.Project, error) {
	return []project.Project{}, nil
}
func (m *projectMemory) DoProject(_ context.Context, fn func(Tx) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fn(m)
}
func (m *projectMemory) CreateProject(_ context.Context, p project.Project) (project.Project, error) {
	m.next++
	p.ID = []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW"}[m.next-1]
	p.Status, p.Version = project.StatusActive, 1
	p.CreatedAt, p.UpdatedAt = time.Unix(int64(m.next), 0).UTC(), time.Unix(int64(m.next), 0).UTC()
	if m.projects == nil {
		m.projects = map[string]project.Project{}
	}
	m.projects[p.ID] = p
	return p, nil
}
func (m *projectMemory) GetProject(_ context.Context, id string) (project.Project, error) {
	p, ok := m.projects[id]
	if !ok {
		return project.Project{}, project.ErrNotFound
	}
	return p, nil
}
func (m *projectMemory) UpdateProject(_ context.Context, id string, version int64, mutate func(*project.Project) error) (project.Project, error) {
	p, ok := m.projects[id]
	if !ok {
		return project.Project{}, project.ErrNotFound
	}
	if p.Version != version {
		return project.Project{}, ErrProjectVersionConflict
	}
	if err := mutate(&p); err != nil {
		return project.Project{}, err
	}
	p.Version, p.UpdatedAt = p.Version+1, p.UpdatedAt.Add(time.Second)
	m.projects[id] = p
	return p, nil
}
func (m *projectMemory) Idempotency(_ context.Context, op, key string, _ time.Time) (providerapp.Record, bool, error) {
	r, ok := m.records[op+"\x00"+key]
	return r, ok, nil
}
func (m *projectMemory) PutIdempotency(_ context.Context, r providerapp.Record) error {
	if m.records == nil {
		m.records = map[string]providerapp.Record{}
	}
	m.records[r.Operation+"\x00"+r.Key] = r
	return nil
}
func (m *projectMemory) PutAudit(_ context.Context, a providerapp.Audit) error {
	m.audits = append(m.audits, a)
	return nil
}

func TestFixedClockSamePayloadDifferentKeysHaveDistinctAuditIDs(t *testing.T) {
	mem := &projectMemory{records: map[string]providerapp.Record{}}
	svc := NewWithClock(mem, mem, fixedClock{now: time.Date(2026, 8, 10, 1, 2, 3, 4, time.UTC)})
	request := struct {
		Name string `json:"name"`
	}{Name: "Alpha"}
	for _, key := range []string{"key-one", "key-two"} {
		if _, err := svc.Create(context.Background(), key, "test", request, project.Project{Name: "Alpha"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(mem.audits) != 2 || mem.audits[0].ID == mem.audits[1].ID {
		t.Fatalf("audit IDs collided: %#v", mem.audits)
	}
	for _, audit := range mem.audits {
		if len(audit.ID) != 26 || len(audit.ID) > 64 || audit.AggregateID == "" {
			t.Fatalf("invalid audit identity: %#v", audit)
		}
	}
}

func TestConcurrentSameKeyReplaysOnce(t *testing.T) {
	mem := &projectMemory{records: map[string]providerapp.Record{}}
	svc := NewWithClock(mem, mem, fixedClock{now: time.Now().UTC()})
	request := struct {
		Name string `json:"name"`
	}{Name: "Alpha"}
	const workers = 12
	results := make(chan project.Project, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := svc.Create(context.Background(), "same-key", "test", request, project.Project{Name: "Alpha"})
			results <- p
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first string
	for p := range results {
		if first == "" {
			first = p.ID
		} else if p.ID != first {
			t.Fatalf("replay IDs differ: %q / %q", first, p.ID)
		}
	}
	if mem.next != 1 || len(mem.audits) != 1 || len(mem.records) != 1 {
		t.Fatalf("creates=%d audits=%d records=%d", mem.next, len(mem.audits), len(mem.records))
	}
	var replay project.Project
	for _, record := range mem.records {
		if err := json.Unmarshal(record.Response, &replay); err != nil {
			t.Fatal(err)
		}
	}
	if replay.ID != first {
		t.Fatalf("stored replay=%q result=%q", replay.ID, first)
	}
}

func TestPrintableASCIIIdempotencyKeys(t *testing.T) {
	mem := &projectMemory{records: map[string]providerapp.Record{}}
	svc := NewWithClock(mem, mem, fixedClock{now: time.Now().UTC()})
	for _, key := range []string{"null byte\x00", "contains space", "é", string(make([]byte, 129))} {
		if _, err := svc.Create(context.Background(), key, "test", struct{}{}, project.Project{Name: "Alpha"}); err != ErrIdempotencyKeyRequired {
			t.Fatalf("key %q: %v", key, err)
		}
	}
}
