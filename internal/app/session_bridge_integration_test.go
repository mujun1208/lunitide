package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	_ "modernc.org/sqlite"
)

func sessionEngine(t *testing.T) (*Engine, string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.db")
	store, e := storage.OpenTemplated(context.Background(), path)
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
	sessions := sessionapp.New(store, store)
	sessions.SetDeleter(store)
	engine := NewEngineWithSessions(providerapp.New(store, store), projects, sessions, "test", nil)
	engine.SetSessionExpertStore(store)
	return engine, created.ID, path
}
func structToProject(name string) (p project.Project) { p.Name = name; return }

func TestSessionBridgeCreateListReplayConflictAndStrictPayloads(t *testing.T) {
	e, parent, _ := sessionEngine(t)
	r := validRequest("session.create", `{"projectId":"`+parent+`","title":"  Alpha   Session  "}`)
	r.IdempotencyKey = "session-key"
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
	r.Payload = json.RawMessage(`{"projectId":"` + parent + `","title":"Different"}`)
	if x := e.Handle(context.Background(), r); x.OK || x.Error.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflict %#v", x)
	}
	if x := e.Handle(context.Background(), validRequest("session.list", `{"projectId":"`+parent+`"}`)); !x.OK {
		t.Fatalf("list %#v", x)
	}
	for _, raw := range []string{"null", `{"projectId":"` + parent + `","title":"Alpha","status":"active"}`, `{"projectId":"` + parent + `","title":"Alpha","unknown":1}`} {
		bad := validRequest("session.create", raw)
		bad.IdempotencyKey = "bad-key"
		if x := e.Handle(context.Background(), bad); x.OK || x.Error.Code != "BRIDGE_SCHEMA_INVALID" {
			t.Fatalf("accepted %s: %#v", raw, x)
		}
	}
}

func TestSessionBridgeDeletesSessionAndItsMessages(t *testing.T) {
	e, parent, _ := sessionEngine(t)
	create := validRequest("session.create", `{"projectId":"`+parent+`","title":"Delete me"}`)
	create.IdempotencyKey = "create-for-delete"
	created := e.Handle(context.Background(), create)
	body, _ := json.Marshal(created.Payload)
	var dto sessionDTO
	if !created.OK || json.Unmarshal(body, &dto) != nil {
		t.Fatalf("create: %#v", created)
	}

	remove := validRequest("session.delete", `{"id":"`+dto.ID+`"}`)
	remove.IdempotencyKey = "delete-session"
	deleted := e.Handle(context.Background(), remove)
	if !deleted.OK {
		t.Fatalf("delete: %#v", deleted)
	}
	listed := e.Handle(context.Background(), validRequest("session.list", `{"projectId":"`+parent+`"}`))
	encoded, _ := json.Marshal(listed.Payload)
	if !listed.OK || string(encoded) != `{"items":[]}` {
		t.Fatalf("list after delete: %#v (%s)", listed, encoded)
	}
}

type nilSessions struct{}

func (*nilSessions) Create(context.Context, string, string, any, session.Session) (session.Session, error) {
	panic("typed nil called")
}
func (*nilSessions) List(context.Context, session.Filter) ([]session.Session, error) {
	panic("typed nil called")
}
func (*nilSessions) Delete(context.Context, string) error {
	panic("typed nil called")
}
func (*nilSessions) Update(context.Context, string, string, any, string, int64, string, bool) (session.Session, error) {
	panic("typed nil called")
}
func TestSessionBridgeMissingTypedNilAndProjectNotFound(t *testing.T) {
	var nilService *nilSessions
	e := NewEngineWithSessions(nil, nil, nilService, "test", nil)
	r := validRequest("session.create", `{"projectId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","title":"Alpha"}`)
	r.IdempotencyKey = "safe-key"
	if x := e.Handle(context.Background(), r); x.OK || x.Error.Code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("typed nil %#v", x)
	}
	real, _, _ := sessionEngine(t)
	// Minted after the engine rather than reused from above. A request
	// carries a three-second deadline from the moment it is built, and
	// standing up a real store replays every migration; under -race that
	// setup outlives the request, and this call was answered
	// REQUEST_DEADLINE_EXCEEDED before it ever reached the project lookup.
	r = validRequest("session.create", `{"projectId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","title":"Alpha"}`)
	r.IdempotencyKey = "missing-parent"
	if x := real.Handle(context.Background(), r); x.OK || x.Error.Code != "PROJECT_NOT_FOUND" {
		t.Fatalf("missing parent: ok=%v error=%+v", x.OK, x.Error)
	}
}

func TestSessionBridgeCapacityReached(t *testing.T) {
	e, parent, _ := sessionEngine(t)
	for i := 0; i < 100; i++ {
		r := validRequest("session.create", `{"projectId":"`+parent+`","title":"Session"}`)
		r.IdempotencyKey = "capacity-" + fmt.Sprint(i)
		if response := e.Handle(context.Background(), r); !response.OK {
			t.Fatalf("create %d: %#v", i, response)
		}
	}
	r := validRequest("session.create", `{"projectId":"`+parent+`","title":"Overflow"}`)
	r.IdempotencyKey = "capacity-overflow"
	if response := e.Handle(context.Background(), r); response.OK || response.Error.Code != "SESSION_CAPACITY_REACHED" {
		t.Fatalf("overflow response: %#v", response)
	}
}

func TestSessionBridgeConcurrentSameKeyCreatesExactlyOneMutation(t *testing.T) {
	e, parent, path := sessionEngine(t)
	r := validRequest("session.create", `{"projectId":"`+parent+`","title":"Concurrent"}`)
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
		`SELECT count(*) FROM sessions WHERE project_id='` + parent + `'`:                                                     1,
		`SELECT count(*) FROM audit_events WHERE action='session.created'`:                                                    1,
		`SELECT count(*) FROM idempotency_records WHERE operation='session.create' AND idempotency_key='concurrent-same-key'`: 1,
	} {
		var got int
		if err = db.QueryRow(query).Scan(&got); err != nil || got != want {
			t.Fatalf("query %q got=%d want=%d err=%v", query, got, want, err)
		}
	}
}

func TestSessionBridgeUpdateRenamePinReplayAndVersionConflict(t *testing.T) {
	e, parent, _ := sessionEngine(t)
	create := validRequest("session.create", `{"projectId":"`+parent+`","title":"Before"}`)
	create.IdempotencyKey = "create-update"
	created := e.Handle(context.Background(), create)
	body, _ := json.Marshal(created.Payload)
	var dto sessionDTO
	if !created.OK || json.Unmarshal(body, &dto) != nil {
		t.Fatalf("create %#v", created)
	}
	update := validRequest("session.update", fmt.Sprintf(`{"id":"%s","title":"  After  ","pinned":true,"version":1}`, dto.ID))
	update.IdempotencyKey = "update-key"
	first, replay := e.Handle(context.Background(), update), e.Handle(context.Background(), update)
	if !first.OK || !replay.OK {
		t.Fatalf("update %#v %#v", first, replay)
	}
	body, _ = json.Marshal(first.Payload)
	if json.Unmarshal(body, &dto) != nil || dto.Title != "After" || !dto.Pinned || dto.Version != 2 {
		t.Fatalf("dto %s", body)
	}
	stale := validRequest("session.update", fmt.Sprintf(`{"id":"%s","title":"Stale","pinned":false,"version":1}`, dto.ID))
	stale.IdempotencyKey = "stale-key"
	if x := e.Handle(context.Background(), stale); x.OK || x.Error.Code != "VERSION_CONFLICT" {
		t.Fatalf("stale %#v", x)
	}
}

func TestSessionExpertsGetAndSetPersistForTheSession(t *testing.T) {
	e, parent, _ := sessionEngine(t)
	create := validRequest("session.create", `{"projectId":"`+parent+`","title":"With experts"}`)
	create.IdempotencyKey = "create-experts"
	created := e.Handle(context.Background(), create)
	body, _ := json.Marshal(created.Payload)
	var dto sessionDTO
	if !created.OK || json.Unmarshal(body, &dto) != nil {
		t.Fatalf("create %#v", created)
	}
	expertID := "01ARZ3NDEKTSV4RRFFQ69G5FAA"
	set := validRequest("session.experts.set", fmt.Sprintf(`{"sessionId":"%s","expertIds":["%s"]}`, dto.ID, expertID))
	set.IdempotencyKey = "set-experts"
	first := e.Handle(context.Background(), set)
	if !first.OK {
		t.Fatalf("set %#v", first)
	}
	got := e.Handle(context.Background(), validRequest("session.experts.get", fmt.Sprintf(`{"sessionId":"%s"}`, dto.ID)))
	if !got.OK {
		t.Fatalf("get %#v", got)
	}
	raw, _ := json.Marshal(got.Payload)
	if !strings.Contains(string(raw), expertID) {
		t.Fatalf("payload %s", raw)
	}
}
