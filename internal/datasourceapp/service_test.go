package datasourceapp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/connector"
)

type fakeStore struct {
	mu          sync.Mutex
	connections []Connection
	bindings    []Binding
}

func (f *fakeStore) PutConnection(_ context.Context, row Connection) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.connections {
		if c.Name == row.Name {
			return ErrDuplicateName
		}
	}
	f.connections = append(f.connections, row)
	return nil
}

func (f *fakeStore) GetConnection(_ context.Context, id string) (Connection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.connections {
		if c.ID == id {
			return c, nil
		}
	}
	return Connection{}, ErrNotFound
}

func (f *fakeStore) ListConnections(context.Context) ([]Connection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Connection, len(f.connections))
	copy(out, f.connections)
	return out, nil
}

func (f *fakeStore) SetConnectionVerified(_ context.Context, id, verifiedAt string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, c := range f.connections {
		if c.ID == id {
			at := verifiedAt
			f.connections[i].ReadOnlyVerifiedAt = &at
			return nil
		}
	}
	return ErrNotFound
}

func (f *fakeStore) DisableConnection(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, c := range f.connections {
		if c.ID == id {
			f.connections[i].State = "disabled"
			f.connections[i].ReadOnlyVerifiedAt = nil
			return nil
		}
	}
	return ErrNotFound
}

func (f *fakeStore) PutBinding(_ context.Context, row Binding) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range f.bindings {
		if b.OwnerType == row.OwnerType && b.OwnerID == row.OwnerID && b.Purpose == row.Purpose {
			return ErrDuplicateBinding
		}
	}
	f.bindings = append(f.bindings, row)
	return nil
}

func (f *fakeStore) GetBinding(_ context.Context, id string) (Binding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range f.bindings {
		if b.BindingID == id {
			return b, nil
		}
	}
	return Binding{}, ErrNotFound
}

func (f *fakeStore) ListBindings(_ context.Context, ownerType, ownerID string) ([]Binding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Binding
	for _, b := range f.bindings {
		if b.OwnerType == ownerType && b.OwnerID == ownerID {
			out = append(out, b)
		}
	}
	return out, nil
}

func newTestService() (*Service, *fakeStore, map[string]string) {
	store := &fakeStore{}
	secrets := map[string]string{}
	svc := New(store)
	svc.SetSecrets(func(ref, dsn string) error {
		secrets[ref] = dsn
		return nil
	}, func(ref string) (string, error) {
		dsn, ok := secrets[ref]
		if !ok {
			return "", ErrNotFound
		}
		return dsn, nil
	})
	return svc, store, secrets
}

func TestCreateStoresDSNOnlyInSecretRef(t *testing.T) {
	svc, store, secrets := newTestService()
	got, err := svc.Create(context.Background(), CreateInput{
		Name: "航司只读副本", Kind: "postgres", DSN: "postgres://ro:s3cret@10.0.0.8:5432/ops",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "航司只读副本" || got.Kind != "postgres" || got.State != "active" || got.ReadonlyVerified {
		t.Fatalf("public = %+v", got)
	}
	if strings.Contains(got.ID, "s3cret") {
		t.Fatal("id leaked dsn")
	}
	rows, err := store.ListConnections(context.Background())
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %+v err=%v", rows, err)
	}
	if rows[0].DSNSecretRef != "dbconn:"+got.ID {
		t.Fatalf("ref = %q", rows[0].DSNSecretRef)
	}
	if secrets[rows[0].DSNSecretRef] != "postgres://ro:s3cret@10.0.0.8:5432/ops" {
		t.Fatalf("secret = %q", secrets[rows[0].DSNSecretRef])
	}
	listed, err := svc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw := listed[0]
	if raw.ReadonlyVerified || strings.Contains(raw.Name+raw.Kind+raw.State+raw.CreatedAt, "s3cret") {
		t.Fatalf("list leaked secret: %+v", raw)
	}
}

func TestCreateRejectsNewlineDSNAndBadKind(t *testing.T) {
	svc, _, _ := newTestService()
	if _, err := svc.Create(context.Background(), CreateInput{Name: "x", Kind: "oracle", DSN: "x"}); !errors.Is(err, ErrPayloadInvalid) {
		t.Fatalf("kind err = %v", err)
	}
	if _, err := svc.Create(context.Background(), CreateInput{Name: "x", Kind: "mysql", DSN: "a\nb"}); !errors.Is(err, ErrPayloadInvalid) {
		t.Fatalf("newline err = %v", err)
	}
}

func TestDisableClearsVerifiedAndKeepsRow(t *testing.T) {
	svc, store, _ := newTestService()
	got, err := svc.Create(context.Background(), CreateInput{Name: "lab", Kind: "mysql", DSN: "user:pw@tcp(127.0.0.1:3306)/lab"})
	if err != nil {
		t.Fatal(err)
	}
	at := "2026-09-03T00:00:00Z"
	if err := store.SetConnectionVerified(context.Background(), got.ID, at); err != nil {
		t.Fatal(err)
	}
	if err := svc.Disable(context.Background(), got.ID); err != nil {
		t.Fatal(err)
	}
	row, err := store.GetConnection(context.Background(), got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.State != "disabled" || row.ReadOnlyVerifiedAt != nil {
		t.Fatalf("row = %+v", row)
	}
}

func TestProbeStampsVerifiedAndFailureKeepsNull(t *testing.T) {
	svc, store, _ := newTestService()
	got, err := svc.Create(context.Background(), CreateInput{Name: "pg", Kind: "postgres", DSN: "postgres://ro:readonly@db.internal/ops"})
	if err != nil {
		t.Fatal(err)
	}
	svc.SetPinger(func(_ context.Context, kind, dsn string) error {
		if kind == "postgres" && strings.Contains(dsn, "readonly") {
			return nil
		}
		return errors.New("dial tcp 10.1.2.3:5432: connect refused postgres://ro:readonly@db.internal/ops")
	})
	if err := svc.Probe(context.Background(), got.ID); err != nil {
		t.Fatal(err)
	}
	row, _ := store.GetConnection(context.Background(), got.ID)
	if row.ReadOnlyVerifiedAt == nil {
		t.Fatal("expected verified")
	}
	bad, err := svc.Create(context.Background(), CreateInput{Name: "bad", Kind: "postgres", DSN: "postgres://rw:x@10.1.2.3/ops"})
	if err != nil {
		t.Fatal(err)
	}
	err = svc.Probe(context.Background(), bad.ID)
	if err == nil {
		t.Fatal("expected probe fail")
	}
	if strings.Contains(err.Error(), "postgres://") || strings.Contains(err.Error(), "10.1.2.3") {
		t.Fatalf("error leaked dsn/host: %v", err)
	}
	row, _ = store.GetConnection(context.Background(), bad.ID)
	if row.ReadOnlyVerifiedAt != nil {
		t.Fatal("failed probe must keep null")
	}
}

func TestBindRequiresVerifiedAndRejectsDuplicatePurpose(t *testing.T) {
	svc, store, _ := newTestService()
	got, err := svc.Create(context.Background(), CreateInput{Name: "pg", Kind: "postgres", DSN: "postgres://ro:x@h/db"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Bind(context.Background(), BindInput{
		OwnerType: "mro", OwnerID: WorkbenchOwnerID, ConnectionID: got.ID, Purpose: "stock",
		TableMapJSON: `{"schema":"inv","table":"stock","pnColumn":"part_no"}`,
	})
	if !errors.Is(err, ErrDatasourceNotVerified) {
		t.Fatalf("unverified bind = %v", err)
	}
	at := "2026-09-03T00:00:00Z"
	if err := store.SetConnectionVerified(context.Background(), got.ID, at); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Bind(context.Background(), BindInput{
		OwnerType: "mro", OwnerID: WorkbenchOwnerID, ConnectionID: got.ID, Purpose: "stock",
		TableMapJSON: `{"schema":"inv","table":"stock","pnColumn":"part_no"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Purpose != "stock" {
		t.Fatalf("binding = %+v", first)
	}
	_, err = svc.Bind(context.Background(), BindInput{
		OwnerType: "mro", OwnerID: WorkbenchOwnerID, ConnectionID: got.ID, Purpose: "stock",
		TableMapJSON: `{"schema":"inv","table":"stock","pnColumn":"pn"}`,
	})
	if !errors.Is(err, ErrDuplicateBinding) {
		t.Fatalf("dup = %v", err)
	}
	_, err = svc.Bind(context.Background(), BindInput{
		OwnerType: "mro", OwnerID: WorkbenchOwnerID, ConnectionID: got.ID, Purpose: "generic",
		TableMapJSON: `{"extra":"nope"}`,
	})
	if !errors.Is(err, ErrPayloadInvalid) {
		t.Fatalf("bad map = %v", err)
	}
}

func TestBrowseRejectsBusinessTableAndCapsRows(t *testing.T) {
	svc, store, _ := newTestService()
	got, err := svc.Create(context.Background(), CreateInput{Name: "pg", Kind: "postgres", DSN: "postgres://ro:x@h/db"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Browse(context.Background(), BrowseInput{ConnectionID: got.ID, Scope: "schema"}); !errors.Is(err, ErrNotProbed) {
		t.Fatalf("unverified browse = %v", err)
	}
	at := "2026-09-03T00:00:00Z"
	_ = store.SetConnectionVerified(context.Background(), got.ID, at)
	if err := connector.StatementAllowed("SELECT * FROM inventory"); err == nil {
		t.Fatal("business table must be denied")
	}
	svc.SetQuerier(func(_ context.Context, _, _, statement string, _ []any, maxRows int) ([]string, [][]any, bool, error) {
		if strings.Contains(strings.ToLower(statement), "inventory") {
			t.Fatal("querier must not see business sql")
		}
		rows := [][]any{{"public"}, {"inv"}}
		return []string{"schema_name"}, rows, false, nil
	})
	items, err := svc.Browse(context.Background(), BrowseInput{ConnectionID: got.ID, Scope: "schema"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Name != "public" {
		t.Fatalf("items = %+v", items)
	}
}

func TestBrowseSQLUsesDialectPlaceholders(t *testing.T) {
	pgTable, args, err := browseSQL("postgres", "table", "public", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pgTable, "table_schema = $1") || len(args) != 1 {
		t.Fatalf("postgres table sql = %q args=%v", pgTable, args)
	}
	if err := connector.StatementAllowed(pgTable); err != nil {
		t.Fatalf("postgres browse sql must pass the metadata allowlist: %v", err)
	}
	pgCol, pgArgs, err := browseSQL("postgres", "column", "inv", "stock")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pgCol, "$1") || !strings.Contains(pgCol, "$2") || len(pgArgs) != 2 {
		t.Fatalf("postgres column sql = %q args=%v", pgCol, pgArgs)
	}
	if err := connector.StatementAllowed(pgCol); err != nil {
		t.Fatalf("postgres column sql must pass the metadata allowlist: %v", err)
	}
	myCol, myArgs, err := browseSQL("mysql", "column", "inv", "stock")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(myCol, "table_schema = ?") || !strings.Contains(myCol, "table_name = ?") || len(myArgs) != 2 {
		t.Fatalf("mysql column sql = %q args=%v", myCol, myArgs)
	}
	if strings.Contains(myCol, "$1") {
		t.Fatalf("mysql must not use postgres placeholders: %q", myCol)
	}
}

func TestQueryRejectsWriteAndUnverifiedAndRequiresStockBinding(t *testing.T) {
	svc, store, _ := newTestService()
	got, err := svc.Create(context.Background(), CreateInput{Name: "pg", Kind: "postgres", DSN: "postgres://ro:x@h/db"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Query(context.Background(), QueryInput{ConnectionID: got.ID, SQL: "SELECT 1"})
	if !errors.Is(err, ErrNotProbed) {
		t.Fatalf("unverified query = %v", err)
	}
	at := "2026-09-03T00:00:00Z"
	_ = store.SetConnectionVerified(context.Background(), got.ID, at)
	svc.SetQuerier(func(_ context.Context, _, _, sqlText string, _ []any, maxRows int) ([]string, [][]any, bool, error) {
		if maxRows > MaxQueryRows {
			t.Fatalf("maxRows %d", maxRows)
		}
		return []string{"n"}, [][]any{{1}}, false, nil
	})
	_, err = svc.Query(context.Background(), QueryInput{ConnectionID: got.ID, SQL: "DELETE FROM x"})
	if !errors.Is(err, ErrStatementDenied) {
		t.Fatalf("delete = %v", err)
	}
	res, err := svc.Query(context.Background(), QueryInput{ConnectionID: got.ID, SQL: "SELECT 1", MaxRows: 5000})
	if err != nil || res.RowCount != 1 {
		t.Fatalf("select = %+v err=%v", res, err)
	}
	_, err = svc.QueryStock(context.Background(), "mro", WorkbenchOwnerID, "SELECT 1", 10)
	if !errors.Is(err, ErrStockBindingRequired) {
		t.Fatalf("stock = %v", err)
	}
	if _, err := svc.Bind(context.Background(), BindInput{
		OwnerType: "mro", OwnerID: WorkbenchOwnerID, ConnectionID: got.ID, Purpose: "stock",
		TableMapJSON: `{"schema":"inv","table":"stock","pnColumn":"part_no"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.QueryStock(context.Background(), "mro", WorkbenchOwnerID, "SELECT 1", 10); err != nil {
		t.Fatal(err)
	}
}

func TestProbeRunsProvisionerBeforePing(t *testing.T) {
	svc, store, _ := newTestService()
	got, err := svc.Create(context.Background(), CreateInput{Name: "local", Kind: "mysql", DSN: "root:pw@tcp(127.0.0.1:3306)/lunitide"})
	if err != nil {
		t.Fatal(err)
	}
	order := ""
	svc.SetProvisioner(func(_ context.Context, _, _ string) error { order += "P"; return nil })
	svc.SetPinger(func(_ context.Context, _, _ string) error { order += "K"; return nil })
	if err := svc.Probe(context.Background(), got.ID); err != nil {
		t.Fatal(err)
	}
	if order != "PK" {
		t.Fatalf("expected provision-then-ping order, got %q", order)
	}
	row, _ := store.GetConnection(context.Background(), got.ID)
	if row.ReadOnlyVerifiedAt == nil {
		t.Fatal("successful probe must stamp verified")
	}
}

func TestProbeProvisionFailureSkipsPingAndVerify(t *testing.T) {
	svc, store, _ := newTestService()
	got, err := svc.Create(context.Background(), CreateInput{Name: "local", Kind: "mysql", DSN: "root:pw@tcp(127.0.0.1:3306)/lunitide"})
	if err != nil {
		t.Fatal(err)
	}
	svc.SetProvisioner(func(_ context.Context, _, _ string) error {
		return errors.New("access denied creating database at 127.0.0.1:3306")
	})
	pinged := false
	svc.SetPinger(func(_ context.Context, _, _ string) error { pinged = true; return nil })
	err = svc.Probe(context.Background(), got.ID)
	if !errors.Is(err, ErrProvisionFailed) {
		t.Fatalf("probe err = %v want ErrProvisionFailed", err)
	}
	if pinged {
		t.Fatal("ping must not run after a provision failure")
	}
	if strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("provision error leaked host: %v", err)
	}
	row, _ := store.GetConnection(context.Background(), got.ID)
	if row.ReadOnlyVerifiedAt != nil {
		t.Fatal("failed provision must keep verified null")
	}
}

func TestQueryAllowsWriteOnLocalConnection(t *testing.T) {
	svc, store, _ := newTestService()
	conn, err := svc.Create(context.Background(), CreateInput{Name: "local", Kind: "mysql", DSN: "root:pw@tcp(127.0.0.1:3306)/lunitide"})
	if err != nil {
		t.Fatal(err)
	}
	_ = store.SetConnectionVerified(context.Background(), conn.ID, "2026-09-03T00:00:00Z")
	svc.SetQuerier(func(context.Context, string, string, string, []any, int) ([]string, [][]any, bool, error) {
		t.Fatal("read-only querier must not run for a local connection")
		return nil, nil, false, nil
	})
	gotSQL := ""
	svc.SetWriteQuerier(func(_ context.Context, _, _, statement string, _ []any, _ int) ([]string, [][]any, bool, error) {
		gotSQL = statement
		return []string{"rows_affected"}, [][]any{{int64(1)}}, false, nil
	})
	res, err := svc.Query(context.Background(), QueryInput{ConnectionID: conn.ID, SQL: "CREATE TABLE stock(id INT)"})
	if err != nil {
		t.Fatal(err)
	}
	if gotSQL != "CREATE TABLE stock(id INT)" {
		t.Fatalf("write querier saw %q", gotSQL)
	}
	if res.RowCount != 1 || len(res.Columns) != 1 || res.Columns[0] != "rows_affected" {
		t.Fatalf("write result = %+v", res)
	}
}

func TestQueryKeepsRemoteReadOnlyEvenWithWriteQuerier(t *testing.T) {
	svc, store, _ := newTestService()
	conn, err := svc.Create(context.Background(), CreateInput{Name: "remote", Kind: "postgres", DSN: "postgres://ro:x@db.internal:5432/ops"})
	if err != nil {
		t.Fatal(err)
	}
	_ = store.SetConnectionVerified(context.Background(), conn.ID, "2026-09-03T00:00:00Z")
	svc.SetWriteQuerier(func(context.Context, string, string, string, []any, int) ([]string, [][]any, bool, error) {
		t.Fatal("write querier must never run for a remote connection")
		return nil, nil, false, nil
	})
	svc.SetQuerier(func(context.Context, string, string, string, []any, int) ([]string, [][]any, bool, error) {
		return []string{"n"}, [][]any{{1}}, false, nil
	})
	if _, err := svc.Query(context.Background(), QueryInput{ConnectionID: conn.ID, SQL: "DROP TABLE ops"}); !errors.Is(err, ErrStatementDenied) {
		t.Fatalf("remote write must be denied, got %v", err)
	}
	if _, err := svc.Query(context.Background(), QueryInput{ConnectionID: conn.ID, SQL: "SELECT 1"}); err != nil {
		t.Fatalf("remote read must pass: %v", err)
	}
}

func TestRedactErrorStripsDSNAndHost(t *testing.T) {
	msg := RedactError(errors.New("dial tcp 10.0.0.8:5432: postgres://ro:s3cret@db.internal/ops failed"))
	if strings.Contains(msg, "s3cret") || strings.Contains(msg, "10.0.0.8") || strings.Contains(msg, "postgres://") {
		t.Fatalf("not redacted: %s", msg)
	}
}

func TestCreateRejectsEmptyName(t *testing.T) {
	svc, _, _ := newTestService()
	if _, err := svc.Create(context.Background(), CreateInput{Name: "  ", Kind: "mysql", DSN: "x"}); !errors.Is(err, ErrPayloadInvalid) {
		t.Fatalf("err = %v", err)
	}
	_ = ulid.Make()
}
