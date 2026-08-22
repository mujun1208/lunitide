package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

const validProjectCreateJSON = `{"name":"Alpha Project","type":"implementation","description":"desc","client":"客户A","planStart":"2026-01-01","planEnd":"2026-12-31"}`

func TestProjectBridgeCreateReplayConflictAndList(t *testing.T) {
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := NewEngineWithProjects(providerapp.New(store, store), projectapp.New(store, store), "test", nil)
	r := validRequest("project.create", validProjectCreateJSON)
	r.IdempotencyKey = "project-key"
	first := e.Handle(context.Background(), r)
	if !first.OK {
		t.Fatalf("create: %#v", first)
	}
	second := e.Handle(context.Background(), r)
	if !second.OK {
		t.Fatalf("replay: %#v", second)
	}
	a, _ := json.Marshal(first.Payload)
	b, _ := json.Marshal(second.Payload)
	if string(a) != string(b) {
		t.Fatalf("replay differs: %s / %s", a, b)
	}
	r.Payload = json.RawMessage(`{"name":"Different","type":"implementation","description":"desc","client":"客户B","planStart":"2026-01-01","planEnd":"2026-12-31"}`)
	conflict := e.Handle(context.Background(), r)
	if conflict.OK || conflict.Error.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflict: %#v", conflict)
	}
	listed := e.Handle(context.Background(), validRequest("project.list", `{}`))
	if !listed.OK {
		t.Fatalf("list: %#v", listed)
	}
}

func TestProjectBridgeRejectsNullAndOversizedPayloads(t *testing.T) {
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "project-invalid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := NewEngineWithProjects(providerapp.New(store, store), projectapp.New(store, store), "test", nil)
	for _, tc := range []struct {
		name, method, payload string
	}{
		{"create-null", "project.create", `null`},
		{"list-null", "project.list", `null`},
		{"raw-name-over-200", "project.create", `{"name":"` + strings.Repeat(" ", 200) + `A"}`},
		{"normalized-name-over-200", "project.create", `{"name":"` + strings.Repeat("A", 201) + `"}`},
		{"forged-create-status", "project.create", `{"name":"Alpha","status":"archived"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := validRequest(tc.method, tc.payload)
			if tc.method == "project.create" {
				r.IdempotencyKey = "valid-key"
			}
			response := e.Handle(context.Background(), r)
			if response.OK || response.Error == nil || response.Error.Code != "BRIDGE_SCHEMA_INVALID" {
				t.Fatalf("invalid payload accepted: %#v", response)
			}
		})
	}
}

func TestProjectHandlersFailSafelyWithoutService(t *testing.T) {
	e := NewEngine(nil, "test")
	create := validRequest("project.create", `{"name":"Alpha"}`)
	create.IdempotencyKey = "safe-key"
	for _, request := range []struct {
		name string
		r    bridge.Request
	}{{"create", create}, {"list", validRequest("project.list", `{}`)}} {
		t.Run(request.name, func(t *testing.T) {
			response := e.Handle(context.Background(), request.r)
			if response.OK || response.Error == nil || response.Error.Code != "STORAGE_UNAVAILABLE" {
				t.Fatalf("unsafe missing wiring response: %#v", response)
			}
		})
	}
}

func TestProjectBridgeConcurrentSameKeyReplay(t *testing.T) {
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "project-concurrent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := NewEngineWithProjects(providerapp.New(store, store), projectapp.New(store, store), "test", nil)
	r := validRequest("project.create", `{"name":"Concurrent","type":"implementation","description":"desc","client":"客户A","planStart":"2026-01-01","planEnd":"2026-12-31"}`)
	r.IdempotencyKey = "concurrent-key"
	const workers = 12
	responses := make(chan bridge.Response, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			responses <- e.Handle(context.Background(), r)
		}()
	}
	wg.Wait()
	close(responses)
	var first string
	for response := range responses {
		if !response.OK {
			t.Fatalf("concurrent replay failed: %#v", response)
		}
		encoded, _ := json.Marshal(response.Payload)
		if first == "" {
			first = string(encoded)
		} else if string(encoded) != first {
			t.Fatalf("concurrent responses differ: %s / %s", first, encoded)
		}
	}
	listed := e.Handle(context.Background(), validRequest("project.list", `{}`))
	encoded, _ := json.Marshal(listed.Payload)
	var result struct {
		Items []projectDTO `json:"items"`
	}
	if !listed.OK || json.Unmarshal(encoded, &result) != nil || len(result.Items) != 1 {
		t.Fatalf("concurrent duplicate created more than one project: %#v (%s)", listed, encoded)
	}
}

func TestProjectCapacityMapsToStableNonRetryableError(t *testing.T) {
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "project-capacity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := NewEngineWithProjects(providerapp.New(store, store), projectapp.New(store, store), "test", nil)
	for i := 0; i < 101; i++ {
		r := validRequest("project.create", fmt.Sprintf(`{"name":"Capacity %d","type":"implementation","description":"desc","client":"客户A","planStart":"2026-01-01","planEnd":"2026-12-31"}`, i))
		r.IdempotencyKey = "capacity-" + strings.Repeat("x", i%20) + string(rune('A'+i/20))
		response := e.Handle(context.Background(), r)
		if i < 100 && !response.OK {
			t.Fatalf("create %d: %#v", i, response)
		}
		if i == 100 && (response.OK || response.Error == nil || response.Error.Code != "PROJECT_CAPACITY_REACHED" || response.Error.Retryable) {
			t.Fatalf("capacity response: %#v", response)
		}
	}
}

func TestProjectBridgeUpdatePublishCloseReopen(t *testing.T) {
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "project-lifecycle.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := NewEngineWithProjects(providerapp.New(store, store), projectapp.New(store, store), "test", nil)

	create := validRequest("project.create", `{"name":"电商","type":"implementation","description":"大范德萨","summary":"范德萨发","objective":"范德萨","client":"范德萨","contractNo":"ht-222-06","amount":11,"budget":0,"planStart":"2026-08-18","planEnd":"2026-12-18"}`)
	create.IdempotencyKey = "lifecycle-create"
	created := e.Handle(context.Background(), create)
	if !created.OK {
		t.Fatalf("create: %#v", created)
	}
	raw, _ := json.Marshal(created.Payload)
	var dto projectDTO
	if err := json.Unmarshal(raw, &dto); err != nil || dto.ID == "" || dto.Version != 1 {
		t.Fatalf("create dto: %s (%v)", raw, err)
	}

	update := validRequest("project.update", fmt.Sprintf(`{"id":%q,"version":%d,"name":"在线电商","type":"implementation","description":"大范德萨","summary":"范德萨发","objective":"范德萨","client":"范德萨","contractNo":"ht-222-06","amount":11,"budget":0,"planStart":"2026-08-18","planEnd":"2026-12-18","remark":""}`, dto.ID, dto.Version))
	update.IdempotencyKey = "lifecycle-update"
	updated := e.Handle(context.Background(), update)
	if !updated.OK {
		t.Fatalf("update: %#v", updated)
	}
	raw, _ = json.Marshal(updated.Payload)
	if err := json.Unmarshal(raw, &dto); err != nil || dto.Name != "在线电商" || dto.Version != 2 {
		t.Fatalf("update dto: %s (%v)", raw, err)
	}

	publish := validRequest("project.publish", fmt.Sprintf(`{"id":%q,"version":%d}`, dto.ID, dto.Version))
	publish.IdempotencyKey = "lifecycle-publish"
	published := e.Handle(context.Background(), publish)
	if !published.OK {
		t.Fatalf("publish: %#v", published)
	}
	raw, _ = json.Marshal(published.Payload)
	if err := json.Unmarshal(raw, &dto); err != nil || dto.Status != "chartered" || dto.Version != 3 {
		t.Fatalf("publish dto: %s (%v)", raw, err)
	}

	advance := validRequest("project.advanceStatus", fmt.Sprintf(`{"id":%q,"version":%d,"phase":1}`, dto.ID, dto.Version))
	advance.IdempotencyKey = "lifecycle-advance"
	advanced := e.Handle(context.Background(), advance)
	if !advanced.OK {
		t.Fatalf("advance: %#v", advanced)
	}
	raw, _ = json.Marshal(advanced.Payload)
	if err := json.Unmarshal(raw, &dto); err != nil || dto.Status != "req_architecture" {
		t.Fatalf("advance dto: %s (%v)", raw, err)
	}

	closeReq := validRequest("project.close", fmt.Sprintf(`{"id":%q,"version":%d,"reason":"验收完成"}`, dto.ID, dto.Version))
	closeReq.IdempotencyKey = "lifecycle-close"
	closed := e.Handle(context.Background(), closeReq)
	if !closed.OK {
		t.Fatalf("close: %#v", closed)
	}
	raw, _ = json.Marshal(closed.Payload)
	if err := json.Unmarshal(raw, &dto); err != nil || dto.Status != "closed" || dto.StatusBeforeClose != "req_architecture" {
		t.Fatalf("close dto: %s (%v)", raw, err)
	}

	reopen := validRequest("project.reopen", fmt.Sprintf(`{"id":%q,"version":%d,"reason":"恢复实施"}`, dto.ID, dto.Version))
	reopen.IdempotencyKey = "lifecycle-reopen"
	reopened := e.Handle(context.Background(), reopen)
	if !reopened.OK {
		t.Fatalf("reopen: %#v", reopened)
	}
	raw, _ = json.Marshal(reopened.Payload)
	if err := json.Unmarshal(raw, &dto); err != nil || dto.Status != "req_architecture" {
		t.Fatalf("reopen dto: %s (%v)", raw, err)
	}
}

func TestProjectListOmitsEmptyCloseFieldsAndKeepsRFC3339NanoTimes(t *testing.T) {
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "project-list-shape.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := NewEngineWithProjects(providerapp.New(store, store), projectapp.New(store, store), "test", nil)
	create := validRequest("project.create", validProjectCreateJSON)
	create.IdempotencyKey = "shape-create"
	created := e.Handle(context.Background(), create)
	if !created.OK {
		t.Fatalf("create: %#v", created)
	}
	listed := e.Handle(context.Background(), validRequest("project.list", `{}`))
	if !listed.OK {
		t.Fatalf("list: %#v", listed)
	}
	encoded, err := json.Marshal(listed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	items, _ := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("want 1 project, got %s", encoded)
	}
	item, _ := items[0].(map[string]any)
	for _, key := range []string{"closeReason", "statusBeforeClose", "reopenReason"} {
		if _, ok := item[key]; ok {
			t.Fatalf("open project must omit empty %s: %s", key, encoded)
		}
	}
	createdAt, _ := item["createdAt"].(string)
	if !strings.HasSuffix(createdAt, "Z") || !strings.Contains(createdAt, "T") {
		t.Fatalf("createdAt must be UTC RFC3339Nano, got %q", createdAt)
	}
}

