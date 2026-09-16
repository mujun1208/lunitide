package capabilitypack

import (
	"context"
	"errors"
	"testing"
)

type memStore struct {
	rec  Record
	refs []Component
}

func (m *memStore) TransactPack(_ context.Context, fn func(Tx) error) error { return fn(m) }
func (m *memStore) GetPack(id string) (Record, error) {
	if m.rec.Spec.ID == "" || m.rec.Spec.ID != id {
		return Record{}, ErrNotFound
	}
	return m.rec, nil
}
func (m *memStore) ListPacks() ([]Record, error) {
	if m.rec.Spec.ID == "" {
		return nil, nil
	}
	return []Record{m.rec}, nil
}
func (m *memStore) SavePack(r Record, expected int64) error {
	if m.rec.Spec.ID != "" && m.rec.Version != expected {
		return ErrConflict
	}
	m.rec = r
	return nil
}
func (m *memStore) PlanResource(r Resource) (Resource, error) { return r, nil }
func (m *memStore) PutReference(_ string, c Component) error {
	for i, item := range m.refs {
		if item.Kind == c.Kind && item.Key == c.Key {
			m.refs[i] = c
			return nil
		}
	}
	m.refs = append(m.refs, c)
	return nil
}
func (m *memStore) ReplaceReferences(_ string, components []Component) error {
	m.refs = append([]Component(nil), components...)
	return nil
}
func (m *memStore) References(string) ([]Component, error) {
	out := make([]Component, len(m.refs))
	copy(out, m.refs)
	return out, nil
}
func (m *memStore) SetReferenceState(_, kind, key, state string) error {
	for i, item := range m.refs {
		if item.Kind == kind && item.Key == key {
			m.refs[i].State = state
			return nil
		}
	}
	return ErrNotFound
}
func (m *memStore) SetResourceTarget(kind, key, target string) error {
	for i, item := range m.refs {
		if item.Kind == kind && item.Key == key {
			m.refs[i].TargetID = target
			return nil
		}
	}
	return ErrNotFound
}
func (m *memStore) MayRelease(string, string, string) (bool, error) { return true, nil }

type skipMCPExecutor struct{}

func (skipMCPExecutor) Describe(_ context.Context, kind, key string) (Resource, error) {
	return Resource{Kind: kind, Key: key, TargetID: key, Managed: true}, nil
}
func (skipMCPExecutor) Ensure(_ context.Context, r Resource) (string, error) {
	if r.Kind == "mcp" {
		return "", errors.New("uvx missing")
	}
	return r.TargetID, nil
}
func (skipMCPExecutor) Release(context.Context, Resource) error { return nil }
func (skipMCPExecutor) Mount(context.Context, Spec) error       { return nil }
func (skipMCPExecutor) Unmount(context.Context, string) error   { return nil }

func TestInstallSkipsFailedMCPAndStillSucceeds(t *testing.T) {
	store := &memStore{}
	svc := New(store, skipMCPExecutor{})
	got, err := svc.Install(context.Background(), Spec{
		ID:           "pack-report",
		Name:         "报告写作包",
		Description:  "gates plus optional mcp",
		Skills:       []string{"docx-writer"},
		McpPresetIDs: []string{"fetch"},
		ToolGates:    []string{"web-search"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "installed" {
		t.Fatalf("state=%s error=%s", got.State, got.Error)
	}
	var skipped bool
	for _, item := range got.Components {
		if item.Kind == "mcp" && item.Key == "fetch" {
			skipped = item.State == "skipped"
		}
	}
	if !skipped {
		t.Fatalf("mcp was not skipped: %+v", got.Components)
	}
}
