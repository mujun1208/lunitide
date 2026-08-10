package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/stage"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/lunitide/lunitide/internal/stageapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	_ "modernc.org/sqlite"
)

func stageEngine(t *testing.T) (*Engine, string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stage.db")
	store, e := storage.Open(context.Background(), path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	projects := projectapp.New(store, store)
	created, e := projects.Create(context.Background(), "parent-key", "test", struct {
		Name string `json:"name"`
	}{"Parent"}, structToProject("Parent"))
	if e != nil {
		t.Fatal(e)
	}
	engine := NewEngineWithSessions(providerapp.New(store, store), projects, sessionapp.New(store, store), "test", nil)
	engine.stages = stageapp.New(store, store)
	return engine, created.ID, path
}

func TestStageBridgeCreateListReplayConflictAndStrictPayloads(t *testing.T) {
	e, parent, _ := stageEngine(t)
	r := validRequest("stage.create", `{"projectId":"`+parent+`","phase":1,"title":"  Requirements   Phase  "}`)
	r.IdempotencyKey = "stage-key"
	first := e.Handle(context.Background(), r)
	second := e.Handle(context.Background(), r)
	if !first.OK || !second.OK {
		t.Fatalf("create/replay %#v %#v", first, second)
	}
	a, _ := json.Marshal(first.Payload)
	b, _ := json.Marshal(second.Payload)
	if string(a) != string(b) {
		t.Fatalf("replay differs %s %s", a, b)
	}
	var dto stageDTO
	if err := json.Unmarshal(a, &dto); err != nil {
		t.Fatal(err)
	}
	if dto.Phase != 1 || dto.Title != "Requirements Phase" || dto.Status != stage.StatusNotStarted || dto.Version != 1 {
		t.Fatalf("unexpected DTO %#v", dto)
	}
	r.Payload = json.RawMessage(`{"projectId":"` + parent + `","phase":1,"title":"Different"}`)
	if x := e.Handle(context.Background(), r); x.OK || x.Error.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflict %#v", x)
	}
	if x := e.Handle(context.Background(), validRequest("stage.list", `{"projectId":"`+parent+`"}`)); !x.OK {
		t.Fatalf("list %#v", x)
	}
	for _, raw := range []string{
		"null",
		`{"projectId":"` + parent + `","phase":1,"title":"Alpha","status":"not_started"}`,
		`{"projectId":"` + parent + `","phase":1,"title":"Alpha","unknown":1}`,
		`{"projectId":"` + parent + `","phase":0,"title":"Alpha"}`,
		`{"projectId":"` + parent + `","phase":10,"title":"Alpha"}`,
		`{"projectId":"bad","phase":1,"title":"Alpha"}`,
	} {
		bad := validRequest("stage.create", raw)
		bad.IdempotencyKey = "bad-key"
		if x := e.Handle(context.Background(), bad); x.OK || x.Error.Code != "BRIDGE_SCHEMA_INVALID" {
			t.Fatalf("accepted %s: %#v", raw, x)
		}
	}
}

type nilStages struct{}

func (*nilStages) Create(context.Context, string, string, any, stage.Stage) (stage.Stage, error) {
	panic("typed nil called")
}
func (*nilStages) List(context.Context, stage.Filter) ([]stage.Stage, error) {
	panic("typed nil called")
}

func TestStageBridgeMissingTypedNilAndProjectNotFound(t *testing.T) {
	var nilService *nilStages
	e := NewEngineWithSessions(nil, nil, nil, "test", nil)
	e.stages = nilService
	r := validRequest("stage.create", `{"projectId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","phase":1,"title":"Alpha"}`)
	r.IdempotencyKey = "safe-key"
	if x := e.Handle(context.Background(), r); x.OK || x.Error.Code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("typed nil %#v", x)
	}
	real, _, _ := stageEngine(t)
	r.IdempotencyKey = "missing-parent"
	r.Payload = json.RawMessage(`{"projectId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","phase":1,"title":"Alpha"}`)
	if x := real.Handle(context.Background(), r); x.OK || x.Error.Code != "PROJECT_NOT_FOUND" {
		t.Fatalf("missing parent %#v", x)
	}
}

func TestStageBridgePhaseConflict(t *testing.T) {
	e, parent, _ := stageEngine(t)
	r := validRequest("stage.create", `{"projectId":"`+parent+`","phase":1,"title":"Requirements"}`)
	r.IdempotencyKey = "phase-conflict-1"
	if x := e.Handle(context.Background(), r); !x.OK {
		t.Fatalf("first create %#v", x)
	}
	r2 := validRequest("stage.create", `{"projectId":"`+parent+`","phase":1,"title":"Requirements Again"}`)
	r2.IdempotencyKey = "phase-conflict-2"
	if x := e.Handle(context.Background(), r2); x.OK || x.Error.Code != "STAGE_PHASE_CONFLICT" {
		t.Fatalf("phase conflict %#v", x)
	}
	r3 := validRequest("stage.create", `{"projectId":"`+parent+`","phase":2,"title":"Architecture"}`)
	r3.IdempotencyKey = "phase-conflict-3"
	if x := e.Handle(context.Background(), r3); !x.OK {
		t.Fatalf("second phase create %#v", x)
	}
}

func TestStageBridgeConcurrentSameKeyCreatesExactlyOneMutation(t *testing.T) {
	e, parent, path := stageEngine(t)
	r := validRequest("stage.create", `{"projectId":"`+parent+`","phase":1,"title":"Concurrent"}`)
	r.IdempotencyKey = "concurrent-same-key"
	const workers = 20
	responses := make(chan bridge.Response, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			responses <- e.Handle(context.Background(), r)
		}()
	}
	close(start)
	wg.Wait()
	close(responses)
	var encoded string
	for response := range responses {
		if !response.OK {
			t.Fatalf("response: %#v", response)
		}
		body, _ := json.Marshal(response.Payload)
		if encoded == "" {
			encoded = string(body)
		} else if encoded != string(body) {
			t.Fatalf("different replay result: %s != %s", encoded, body)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for query, want := range map[string]int{
		`SELECT count(*) FROM stages WHERE project_id='` + parent + `'`:                                                1,
		`SELECT count(*) FROM audit_events WHERE action='stage.created'`:                                               1,
		`SELECT count(*) FROM idempotency_records WHERE operation='stage.create' AND idempotency_key='concurrent-same-key'`: 1,
	} {
		var got int
		if err = db.QueryRow(query).Scan(&got); err != nil || got != want {
			t.Fatalf("query %q got=%d want=%d err=%v", query, got, want, err)
		}
	}
}

func TestStageBridgeAllNinePhases(t *testing.T) {
	e, parent, _ := stageEngine(t)
	for phase := 1; phase <= 9; phase++ {
		r := validRequest("stage.create", fmt.Sprintf(`{"projectId":"%s","phase":%d,"title":"Phase %d"}`, parent, phase, phase))
		r.IdempotencyKey = fmt.Sprintf("phase-%d", phase)
		if x := e.Handle(context.Background(), r); !x.OK {
			t.Fatalf("create phase %d: %#v", phase, x)
		}
	}
	listed := e.Handle(context.Background(), validRequest("stage.list", `{"projectId":"`+parent+`"}`))
	if !listed.OK {
		t.Fatalf("list %#v", listed)
	}
	body, _ := json.Marshal(listed.Payload)
	var result struct {
		Items []stageDTO `json:"items"`
	}
	if err := json.Unmarshal(body, &result); err != nil || len(result.Items) != 9 {
		t.Fatalf("expected 9 stages, got %d: %s", len(result.Items), body)
	}
	for i, item := range result.Items {
		if item.Phase != i+1 {
			t.Fatalf("expected phase %d at index %d, got %d", i+1, i, item.Phase)
		}
	}
}
