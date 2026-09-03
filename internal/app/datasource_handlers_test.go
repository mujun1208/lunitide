package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/datasourceapp"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func datasourceEngine(t *testing.T) (*Engine, *datasourceapp.Service) {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "ds.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	projects := projectapp.New(store, store)
	sessions := sessionapp.New(store, store)
	e := NewEngineWithSessions(providerapp.New(store, store), projects, sessions, "test", nil)
	svc := datasourceapp.New(store)
	secrets := datasourceapp.NewMemorySecrets()
	svc.SetSecrets(secrets.Put, secrets.Get)
	svc.SetPinger(func(_ context.Context, kind, dsn string) error {
		if strings.Contains(dsn, "readonly") {
			return nil
		}
		return errProbe("dial tcp 10.0.0.8:5432 " + dsn)
	})
	svc.SetQuerier(func(_ context.Context, _, _, sqlText string, _ []any, _ int) ([]string, [][]any, bool, error) {
		return []string{"n"}, [][]any{{1}}, false, nil
	})
	e.SetDatasourceService(svc)
	return e, svc
}

type probeErr string

func (e probeErr) Error() string { return string(e) }

func errProbe(s string) error { return probeErr(s) }

func TestDatasourceListStripsSecretAndCreateRejectsEmptyDSN(t *testing.T) {
	e, _ := datasourceEngine(t)
	create := validRequest("datasource.create", `{"name":"airline-ro","kind":"postgres","dsn":"postgres://ro:readonly@h/db"}`)
	create.IdempotencyKey = "ds-create"
	got := e.Handle(context.Background(), create)
	if !got.OK {
		t.Fatalf("create %#v", got)
	}
	raw, _ := json.Marshal(got.Payload)
	if strings.Contains(string(raw), "postgres://") || strings.Contains(string(raw), "dsnSecretRef") || strings.Contains(string(raw), `"dsn"`) {
		t.Fatalf("create leaked dsn: %s", raw)
	}
	listed := e.Handle(context.Background(), validRequest("datasource.list", `{}`))
	if !listed.OK {
		t.Fatalf("list %#v", listed)
	}
	body, _ := json.Marshal(listed.Payload)
	if strings.Contains(string(body), "postgres://") || strings.Contains(string(body), "dsnSecretRef") || strings.Contains(string(body), `"dsn"`) {
		t.Fatalf("list leaked secret: %s", body)
	}
	bad := validRequest("datasource.create", `{"name":"x","kind":"postgres","dsn":""}`)
	bad.IdempotencyKey = "ds-empty"
	rej := e.Handle(context.Background(), bad)
	if rej.OK || rej.Error == nil || rej.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("empty dsn %#v", rej)
	}
}

func TestDatasourceQueryRejectsUnverifiedAndDelete(t *testing.T) {
	e, svc := datasourceEngine(t)
	create := validRequest("datasource.create", `{"name":"lab","kind":"postgres","dsn":"postgres://rw:x@10.0.0.8/db"}`)
	create.IdempotencyKey = "ds-q-create"
	created := e.Handle(context.Background(), create)
	var dto struct {
		ID string `json:"id"`
	}
	body, _ := json.Marshal(created.Payload)
	if err := json.Unmarshal(body, &dto); err != nil || dto.ID == "" {
		t.Fatalf("dto %s", body)
	}
	q := e.Handle(context.Background(), validRequest("datasource.query", `{"connectionId":"`+dto.ID+`","sql":"SELECT 1"}`))
	if q.OK || q.Error == nil || q.Error.Code != "FORBIDDEN" || q.Error.Message != "连接未探测" {
		t.Fatalf("unverified %#v", q)
	}
	del := e.Handle(context.Background(), validRequest("datasource.query", `{"connectionId":"`+dto.ID+`","sql":"DELETE FROM x"}`))
	if del.OK {
		t.Fatalf("delete accepted %#v", del)
	}
	probe := validRequest("datasource.probe", `{"id":"`+dto.ID+`"}`)
	probe.IdempotencyKey = "ds-probe-fail"
	failed := e.Handle(context.Background(), probe)
	if failed.OK || strings.Contains(failed.Error.Message, "10.0.0.8") || strings.Contains(failed.Error.Message, "postgres://") {
		t.Fatalf("probe error leaked: %#v", failed)
	}
	okCreate := validRequest("datasource.create", `{"name":"ro","kind":"postgres","dsn":"postgres://ro:readonly@h/db"}`)
	okCreate.IdempotencyKey = "ds-ro"
	okRow := e.Handle(context.Background(), okCreate)
	okBody, _ := json.Marshal(okRow.Payload)
	_ = json.Unmarshal(okBody, &dto)
	okProbe := validRequest("datasource.probe", `{"id":"`+dto.ID+`"}`)
	okProbe.IdempotencyKey = "ds-probe-ok"
	if got := e.Handle(context.Background(), okProbe); !got.OK {
		t.Fatalf("probe %#v", got)
	}
	if got := e.Handle(context.Background(), validRequest("datasource.query", `{"connectionId":"`+dto.ID+`","sql":"SELECT 1"}`)); !got.OK {
		t.Fatalf("query %#v", got)
	}
	_ = svc
}

