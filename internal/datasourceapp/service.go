package datasourceapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/connector"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
)

var (
	ErrServiceUnavailable    = errors.New("datasourceapp: service unavailable")
	ErrPayloadInvalid        = errors.New("datasourceapp: payload invalid")
	ErrDuplicateName         = errors.New("datasourceapp: duplicate name")
	ErrDuplicateBinding      = errors.New("datasourceapp: duplicate binding")
	ErrNotFound              = errors.New("datasourceapp: not found")
	ErrNotProbed             = errors.New("连接未探测")
	ErrDatasourceNotVerified = errors.New("datasourceapp: connection not verified")
	ErrDisabled              = errors.New("datasourceapp: connection disabled")
	ErrStatementDenied       = errors.New("datasourceapp: statement denied")
	ErrProbeFailed           = errors.New("datasourceapp: probe failed")
	ErrProbeUnavailable      = errors.New("datasourceapp: probe driver unavailable")
	ErrProvisionFailed       = errors.New("datasourceapp: provision failed")
	ErrStockBindingRequired  = errors.New("datasourceapp: stock binding required")
)

const (
	MaxQueryRows     = 1000
	QueryTimeout     = 5 * time.Second
	MaxBrowseRows    = 200
	secretRefPrefix  = "dbconn:"
	WorkbenchOwnerID = "workbench"
)

var tableMapKeys = map[string]struct{}{
	"schema": {}, "table": {}, "pnColumn": {}, "stationColumn": {},
	"qtyColumn": {}, "woColumn": {}, "tailColumn": {},
}

var redactURI = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s]+`)
var redactUserinfo = regexp.MustCompile(`\b[\w.-]+:[^@\s/]+@[\w.-]+`)
var redactIPv4 = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?\b`)

type Connection struct {
	ID                 string
	Name               string
	Kind               string
	DSNSecretRef       string
	State              string
	ReadOnlyVerifiedAt *string
	CreatedAt          string
	CreatedBy          string
}

type ConnectionPublic struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	State            string `json:"state"`
	ReadonlyVerified bool   `json:"readonlyVerified"`
	CreatedAt        string `json:"createdAt"`
}

type Binding struct {
	BindingID    string `json:"bindingId"`
	OwnerType    string `json:"ownerType"`
	OwnerID      string `json:"ownerId"`
	ConnectionID string `json:"connectionId"`
	Purpose      string `json:"purpose"`
	TableMapJSON string `json:"tableMapJson"`
	CreatedAt    string `json:"createdAt"`
}

type CreateInput struct {
	Name, Kind, DSN, Actor string
}

type BindInput struct {
	OwnerType, OwnerID, ConnectionID, Purpose, TableMapJSON, Actor string
}

type BrowseInput struct {
	ConnectionID string
	Scope        string
	Schema       string
	Table        string
}

type BrowseItem struct {
	Name   string `json:"name"`
	Schema string `json:"schema,omitempty"`
}

type QueryInput struct {
	ConnectionID string
	BindingID    string
	SQL          string
	MaxRows      int
}

type QueryResult struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	RowCount  int      `json:"rowCount"`
	Truncated bool     `json:"truncated"`
}

type Store interface {
	PutConnection(ctx context.Context, row Connection) error
	GetConnection(ctx context.Context, id string) (Connection, error)
	ListConnections(ctx context.Context) ([]Connection, error)
	SetConnectionVerified(ctx context.Context, id, verifiedAt string) error
	DisableConnection(ctx context.Context, id string) error
	PutBinding(ctx context.Context, row Binding) error
	GetBinding(ctx context.Context, id string) (Binding, error)
	ListBindings(ctx context.Context, ownerType, ownerID string) ([]Binding, error)
}

type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type Pinger func(ctx context.Context, kind, dsn string) error

type Querier func(ctx context.Context, kind, dsn, statement string, args []any, maxRows int) (columns []string, rows [][]any, truncated bool, err error)

type Service struct {
	store     Store
	clock     Clock
	secretPut   func(ref, dsn string) error
	secretGet   func(ref string) (string, error)
	pinger       Pinger
	provisioner  Pinger
	querier      Querier
	writeQuerier Querier
}

func New(store Store) *Service {
	return &Service{store: store, clock: systemClock{}}
}

func (s *Service) SetSecrets(put func(ref, dsn string) error, get func(ref string) (string, error)) {
	s.secretPut = put
	s.secretGet = get
}

func (s *Service) SetPinger(p Pinger) { s.pinger = p }

// SetProvisioner installs the auto-create step run at the start of Probe. It has
// the same shape as a Pinger; a nil provisioner (tests, or a build without the
// drivers) simply skips auto-create.
func (s *Service) SetProvisioner(p Pinger) { s.provisioner = p }

func (s *Service) SetQuerier(q Querier) { s.querier = q }

// SetWriteQuerier installs the read-WRITE execution path. It is used only for
// local connections (IsLocalDSN); remote connections always run through the
// read-only querier behind ValidateReadOnlySQL. A nil write querier keeps every
// connection strictly read-only.
func (s *Service) SetWriteQuerier(q Querier) { s.writeQuerier = q }

func (s *Service) Create(ctx context.Context, in CreateInput) (ConnectionPublic, error) {
	if s == nil || s.store == nil {
		return ConnectionPublic{}, ErrServiceUnavailable
	}
	name := strings.TrimSpace(in.Name)
	kind := strings.TrimSpace(in.Kind)
	dsn := strings.TrimSpace(in.DSN)
	if name == "" || len(name) > 128 || (kind != "postgres" && kind != "mysql") {
		return ConnectionPublic{}, ErrPayloadInvalid
	}
	if dsn == "" || strings.ContainsAny(dsn, "\r\n") {
		return ConnectionPublic{}, ErrPayloadInvalid
	}
	id := ulid.Make().String()
	ref := secretRefPrefix + id
	if s.secretPut != nil {
		if err := s.secretPut(ref, dsn); err != nil {
			return ConnectionPublic{}, err
		}
	}
	actor := strings.TrimSpace(in.Actor)
	if actor == "" {
		actor = "local-user"
	}
	row := Connection{
		ID: id, Name: name, Kind: kind, DSNSecretRef: ref, State: "active",
		CreatedAt: s.clock.Now().UTC().Format(time.RFC3339Nano), CreatedBy: actor,
	}
	if err := s.store.PutConnection(ctx, row); err != nil {
		return ConnectionPublic{}, err
	}
	return publicConnection(row), nil
}

func (s *Service) List(ctx context.Context) ([]ConnectionPublic, error) {
	if s == nil || s.store == nil {
		return nil, ErrServiceUnavailable
	}
	rows, err := s.store.ListConnections(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ConnectionPublic, 0, len(rows))
	for _, row := range rows {
		out = append(out, publicConnection(row))
	}
	return out, nil
}

func (s *Service) Disable(ctx context.Context, id string) error {
	if s == nil || s.store == nil {
		return ErrServiceUnavailable
	}
	id = strings.TrimSpace(id)
	if len(id) != 26 {
		return ErrPayloadInvalid
	}
	if _, err := s.store.GetConnection(ctx, id); err != nil {
		return err
	}
	return s.store.DisableConnection(ctx, id)
}

func (s *Service) Probe(ctx context.Context, id string) error {
	if s == nil || s.store == nil {
		return ErrServiceUnavailable
	}
	row, dsn, err := s.liveDSN(ctx, id)
	if err != nil {
		return err
	}
	if row.State == "disabled" {
		return ErrDisabled
	}
	if s.pinger == nil {
		return ErrProbeUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	// Auto-create the target database first (local connections only; see
	// SQLProvisioner) so a first-time user only supplies an account + password.
	if s.provisioner != nil {
		if err := s.provisioner(ctx, row.Kind, dsn); err != nil {
			return fmtRedacted(ErrProvisionFailed, err)
		}
	}
	if err := s.pinger(ctx, row.Kind, dsn); err != nil {
		return fmtRedacted(ErrProbeFailed, err)
	}
	return s.store.SetConnectionVerified(ctx, row.ID, s.clock.Now().UTC().Format(time.RFC3339Nano))
}

func (s *Service) Bind(ctx context.Context, in BindInput) (Binding, error) {
	if s == nil || s.store == nil {
		return Binding{}, ErrServiceUnavailable
	}
	ownerType := strings.TrimSpace(in.OwnerType)
	ownerID := strings.TrimSpace(in.OwnerID)
	connID := strings.TrimSpace(in.ConnectionID)
	purpose := strings.TrimSpace(in.Purpose)
	if ownerType != "expert" && ownerType != "mro" {
		return Binding{}, ErrPayloadInvalid
	}
	if ownerID == "" || len(ownerID) > 64 || len(connID) != 26 {
		return Binding{}, ErrPayloadInvalid
	}
	if purpose != "stock" && purpose != "workorder" && purpose != "generic" {
		return Binding{}, ErrPayloadInvalid
	}
	if err := validateTableMap(in.TableMapJSON); err != nil {
		return Binding{}, err
	}
	row, err := s.store.GetConnection(ctx, connID)
	if err != nil {
		return Binding{}, err
	}
	if row.State != "active" || row.ReadOnlyVerifiedAt == nil || strings.TrimSpace(*row.ReadOnlyVerifiedAt) == "" {
		return Binding{}, ErrDatasourceNotVerified
	}
	out := Binding{
		BindingID: ulid.Make().String(), OwnerType: ownerType, OwnerID: ownerID,
		ConnectionID: connID, Purpose: purpose, TableMapJSON: strings.TrimSpace(in.TableMapJSON),
		CreatedAt: s.clock.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := s.store.PutBinding(ctx, out); err != nil {
		return Binding{}, err
	}
	return out, nil
}

func (s *Service) ListBindings(ctx context.Context, ownerType, ownerID string) ([]Binding, error) {
	if s == nil || s.store == nil {
		return nil, ErrServiceUnavailable
	}
	return s.store.ListBindings(ctx, strings.TrimSpace(ownerType), strings.TrimSpace(ownerID))
}

func (s *Service) Browse(ctx context.Context, in BrowseInput) ([]BrowseItem, error) {
	if s == nil || s.store == nil {
		return nil, ErrServiceUnavailable
	}
	row, dsn, err := s.requireVerified(ctx, strings.TrimSpace(in.ConnectionID))
	if err != nil {
		return nil, err
	}
	sqlText, args, err := browseSQL(row.Kind, in.Scope, in.Schema, in.Table)
	if err != nil {
		return nil, err
	}
	if err := connector.StatementAllowed(sqlText); err != nil {
		return nil, ErrStatementDenied
	}
	cols, rows, _, err := s.query(ctx, row.Kind, dsn, sqlText, args, MaxBrowseRows)
	if err != nil {
		return nil, err
	}
	return browseItems(cols, rows), nil
}

func (s *Service) Query(ctx context.Context, in QueryInput) (QueryResult, error) {
	if s == nil || s.store == nil {
		return QueryResult{}, ErrServiceUnavailable
	}
	connID := strings.TrimSpace(in.ConnectionID)
	if bid := strings.TrimSpace(in.BindingID); bid != "" {
		b, err := s.store.GetBinding(ctx, bid)
		if err != nil {
			return QueryResult{}, err
		}
		connID = b.ConnectionID
	}
	row, dsn, err := s.requireVerified(ctx, connID)
	if err != nil {
		return QueryResult{}, err
	}
	// A local connection may run read-write statements (the user opted in: fixed
	// local DB, auto-created). Remote connections stay strictly read-only so a
	// customer database can never be mutated through the panel or the AI tool.
	writable := s.writeQuerier != nil && IsLocalDSN(row.Kind, dsn)
	if !writable {
		if err := m7flow.ValidateReadOnlySQL(in.SQL); err != nil {
			return QueryResult{}, ErrStatementDenied
		}
	}
	maxRows := in.MaxRows
	if maxRows < 1 {
		maxRows = MaxBrowseRows
	}
	if maxRows > MaxQueryRows {
		maxRows = MaxQueryRows
	}
	exec := s.query
	if writable {
		exec = s.writeQuery
	}
	cols, rows, truncated, err := exec(ctx, row.Kind, dsn, in.SQL, nil, maxRows)
	if err != nil {
		return QueryResult{}, err
	}
	return QueryResult{Columns: cols, Rows: rows, RowCount: len(rows), Truncated: truncated}, nil
}

func (s *Service) QueryStock(ctx context.Context, ownerType, ownerID, sqlText string, maxRows int) (QueryResult, error) {
	if s == nil || s.store == nil {
		return QueryResult{}, ErrServiceUnavailable
	}
	bindings, err := s.store.ListBindings(ctx, strings.TrimSpace(ownerType), strings.TrimSpace(ownerID))
	if err != nil {
		return QueryResult{}, err
	}
	var stock *Binding
	for i := range bindings {
		if bindings[i].Purpose == "stock" {
			stock = &bindings[i]
			break
		}
	}
	if stock == nil {
		return QueryResult{}, ErrStockBindingRequired
	}
	return s.Query(ctx, QueryInput{BindingID: stock.BindingID, SQL: sqlText, MaxRows: maxRows})
}

func (s *Service) liveDSN(ctx context.Context, id string) (Connection, string, error) {
	id = strings.TrimSpace(id)
	if len(id) != 26 {
		return Connection{}, "", ErrPayloadInvalid
	}
	row, err := s.store.GetConnection(ctx, id)
	if err != nil {
		return Connection{}, "", err
	}
	if s.secretGet == nil {
		return Connection{}, "", ErrServiceUnavailable
	}
	dsn, err := s.secretGet(row.DSNSecretRef)
	if err != nil {
		return Connection{}, "", err
	}
	return row, dsn, nil
}

func (s *Service) requireVerified(ctx context.Context, id string) (Connection, string, error) {
	row, dsn, err := s.liveDSN(ctx, id)
	if err != nil {
		return Connection{}, "", err
	}
	if row.State == "disabled" {
		return Connection{}, "", ErrDisabled
	}
	if row.ReadOnlyVerifiedAt == nil || strings.TrimSpace(*row.ReadOnlyVerifiedAt) == "" {
		return Connection{}, "", ErrNotProbed
	}
	return row, dsn, nil
}

func (s *Service) query(ctx context.Context, kind, dsn, sqlText string, args []any, maxRows int) ([]string, [][]any, bool, error) {
	return s.runQuerier(ctx, s.querier, kind, dsn, sqlText, args, maxRows)
}

// writeQuery drives the read-write path (local connections only); see Query.
func (s *Service) writeQuery(ctx context.Context, kind, dsn, sqlText string, args []any, maxRows int) ([]string, [][]any, bool, error) {
	return s.runQuerier(ctx, s.writeQuerier, kind, dsn, sqlText, args, maxRows)
}

func (s *Service) runQuerier(ctx context.Context, q Querier, kind, dsn, sqlText string, args []any, maxRows int) ([]string, [][]any, bool, error) {
	if q == nil {
		return nil, nil, false, ErrServiceUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	cols, rows, truncated, err := q(ctx, kind, dsn, sqlText, args, maxRows)
	if err != nil {
		return nil, nil, false, fmtRedacted(err, err)
	}
	if len(rows) > maxRows {
		rows = rows[:maxRows]
		truncated = true
	}
	return cols, rows, truncated, nil
}

func publicConnection(row Connection) ConnectionPublic {
	return ConnectionPublic{
		ID: row.ID, Name: row.Name, Kind: row.Kind, State: row.State,
		ReadonlyVerified: row.ReadOnlyVerifiedAt != nil && strings.TrimSpace(*row.ReadOnlyVerifiedAt) != "",
		CreatedAt:        row.CreatedAt,
	}
}

func validateTableMap(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ErrPayloadInvalid
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil || obj == nil {
		return ErrPayloadInvalid
	}
	for k, v := range obj {
		if _, ok := tableMapKeys[k]; !ok {
			return ErrPayloadInvalid
		}
		s, ok := v.(string)
		if !ok {
			return ErrPayloadInvalid
		}
		s = strings.TrimSpace(s)
		if s == "" || len(s) > 128 {
			return ErrPayloadInvalid
		}
	}
	return nil
}

func browseSQL(kind, scope, schema, table string) (string, []any, error) {
	scope = strings.TrimSpace(scope)
	schema = strings.TrimSpace(schema)
	table = strings.TrimSpace(table)
	switch scope {
	case "catalog":
		if kind == "mysql" {
			return "SELECT schema_name FROM information_schema.schemata", nil, nil
		}
		return "SELECT catalog_name FROM information_schema.information_schema_catalog_name", nil, nil
	case "schema":
		return "SELECT schema_name FROM information_schema.schemata", nil, nil
	case "table":
		if schema == "" {
			return "", nil, ErrPayloadInvalid
		}
		ph := placeholders(kind, 1)
		return "SELECT table_schema, table_name FROM information_schema.tables WHERE table_schema = " + ph[0], []any{schema}, nil
	case "column":
		if schema == "" || table == "" {
			return "", nil, ErrPayloadInvalid
		}
		ph := placeholders(kind, 2)
		return "SELECT column_name FROM information_schema.columns WHERE table_schema = " + ph[0] + " AND table_name = " + ph[1], []any{schema, table}, nil
	default:
		return "", nil, ErrPayloadInvalid
	}
}

// placeholders emits bind markers in the dialect the wired driver expects:
// PostgreSQL (pgx) uses positional $1,$2…; MySQL uses ?. database/sql does not
// rewrite between them, so a mismatch fails at query time against a real server.
func placeholders(kind string, n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		if kind == "postgres" {
			out[i] = fmt.Sprintf("$%d", i+1)
		} else {
			out[i] = "?"
		}
	}
	return out
}

func browseItems(cols []string, rows [][]any) []BrowseItem {
	nameIdx, schemaIdx := 0, -1
	for i, c := range cols {
		switch strings.ToLower(c) {
		case "table_name", "column_name", "schema_name", "catalog_name", "name":
			nameIdx = i
		case "table_schema", "schema":
			schemaIdx = i
		}
	}
	out := make([]BrowseItem, 0, len(rows))
	for _, row := range rows {
		item := BrowseItem{Name: stringify(row, nameIdx)}
		if schemaIdx >= 0 {
			item.Schema = stringify(row, schemaIdx)
		}
		if item.Name == "" && len(row) > 0 {
			item.Name = stringify(row, 0)
		}
		out = append(out, item)
	}
	return out
}

func stringify(row []any, i int) string {
	if i < 0 || i >= len(row) || row[i] == nil {
		return ""
	}
	switch v := row[i].(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(fmtSprint(v), "\n", " "), "\r", " "))
	}
}

func fmtSprint(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return strings.Trim(string(b), `"`)
}

func RedactError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	s = redactURI.ReplaceAllString(s, "[redacted]")
	s = redactUserinfo.ReplaceAllString(s, "[redacted]")
	s = redactIPv4.ReplaceAllString(s, "[redacted]")
	return s
}

func fmtRedacted(wrap, err error) error {
	msg := RedactError(err)
	if wrap == nil {
		return errors.New(msg)
	}
	return fmt.Errorf("%w: %s", wrap, msg)
}
