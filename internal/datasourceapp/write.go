package datasourceapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

var ErrWriteConflict = errors.New("datasourceapp: write confirmation changed or expired")
var ErrWriteUnknown = errors.New("datasourceapp: write outcome unknown; inspect database before creating another operation")

// WriteOperation is the durable receipt for an explicitly reviewed local write.
// Neither the database credential nor its fingerprint is exposed to the renderer.
type WriteOperation struct {
	ID             string       `json:"id"`
	ConnectionID   string       `json:"connectionId"`
	ConnectionName string       `json:"connectionName"`
	SQL            string       `json:"sql"`
	Digest         string       `json:"digest"`
	State          string       `json:"state"`
	CreatedAt      string       `json:"createdAt"`
	ExpiresAt      string       `json:"expiresAt"`
	Result         *QueryResult `json:"result,omitempty"`
	RequestKey     string       `json:"-"`
	TargetDigest   string       `json:"-"`
}

type WriteStore interface {
	PrepareDatasourceWrite(context.Context, WriteOperation) (WriteOperation, error)
	GetDatasourceWrite(context.Context, string) (WriteOperation, error)
	ListDatasourceWrites(context.Context, string) ([]WriteOperation, error)
	TransitionDatasourceWrite(context.Context, string, string, string, *QueryResult) (bool, error)
}

func writeHash(parts ...string) string {
	b, _ := json.Marshal(parts)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ValidateLocalWriteSQL deliberately accepts one data/table/index operation.
// Administrative commands, procedural bodies, comments and stacked statements
// are outside this review flow. Quoted values may contain punctuation safely.
func ValidateLocalWriteSQL(sql string) error {
	if len(sql) == 0 || len(sql) > 16384 || !utf8.ValidString(sql) || strings.ContainsRune(sql, 0) {
		return ErrStatementDenied
	}
	var tokens []string
	for i := 0; i < len(sql); {
		c := sql[i]
		if c == '\'' || c == '"' || c == '`' {
			quote := c
			i++
			closed := false
			for i < len(sql) {
				if sql[i] == '\\' {
					// Backslash quoting changes with connection/session SQL modes.
					// This review flow accepts standard doubled-quote literals only.
					return ErrStatementDenied
				}
				if sql[i] == quote {
					i++
					if i < len(sql) && sql[i] == quote {
						i++
						continue
					}
					closed = true
					break
				}
				i++
			}
			if !closed {
				return ErrStatementDenied
			}
			tokens = append(tokens, "<quoted>")
			continue
		}
		if c == ';' {
			if strings.TrimSpace(sql[i+1:]) != "" {
				return ErrStatementDenied
			}
			break
		}
		if c == '#' || c == '$' || (i+1 < len(sql) && (sql[i:i+2] == "--" || sql[i:i+2] == "/*")) {
			return ErrStatementDenied
		}
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' {
			start := i
			for i < len(sql) && (sql[i] >= 'a' && sql[i] <= 'z' || sql[i] >= 'A' && sql[i] <= 'Z' || sql[i] >= '0' && sql[i] <= '9' || sql[i] == '_') {
				i++
			}
			tokens = append(tokens, strings.ToUpper(sql[start:i]))
			continue
		}
		i++
	}
	if len(tokens) < 2 {
		return ErrStatementDenied
	}
	allowed := tokens[0] == "INSERT" && tokens[1] == "INTO" || tokens[0] == "UPDATE" || tokens[0] == "DELETE" && tokens[1] == "FROM"
	if tokens[0] == "CREATE" || tokens[0] == "ALTER" || tokens[0] == "DROP" {
		allowed = tokens[1] == "TABLE" || tokens[1] == "INDEX" || tokens[0] == "CREATE" && len(tokens) > 2 && tokens[1] == "UNIQUE" && tokens[2] == "INDEX"
	}
	if !allowed {
		return ErrStatementDenied
	}
	for _, token := range tokens {
		switch token {
		case "OUTFILE", "DUMPFILE", "LOAD_FILE", "PROGRAM", "DBLINK", "DBLINK_EXEC", "PG_READ_FILE", "PG_WRITE_FILE", "PG_READ_BINARY_FILE", "PG_LS_DIR", "COPY", "ATTACH", "DETACH", "SLEEP", "PG_SLEEP":
			return ErrStatementDenied
		}
	}
	return nil
}

func (s *Service) writeStore() (WriteStore, error) {
	if s == nil || s.store == nil {
		return nil, ErrServiceUnavailable
	}
	store, ok := s.store.(WriteStore)
	if !ok {
		return nil, ErrServiceUnavailable
	}
	return store, nil
}

func (s *Service) PrepareWrite(ctx context.Context, connectionID, sql, requestKey string) (WriteOperation, error) {
	store, err := s.writeStore()
	if err != nil {
		return WriteOperation{}, err
	}
	sql = strings.TrimSpace(sql)
	if len(requestKey) == 0 || len(requestKey) > 128 {
		return WriteOperation{}, ErrPayloadInvalid
	}
	if err := ValidateLocalWriteSQL(sql); err != nil {
		return WriteOperation{}, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	row, dsn, err := s.requireVerified(ctx, connectionID)
	if err != nil {
		return WriteOperation{}, err
	}
	if !IsLocalDSN(row.Kind, dsn) {
		return WriteOperation{}, ErrStatementDenied
	}
	if s.writeQuerier == nil {
		return WriteOperation{}, ErrServiceUnavailable
	}
	now := s.clock.Now().UTC()
	target := writeHash(row.ID, row.Kind, dsn)
	op := WriteOperation{ID: ulid.Make().String(), ConnectionID: row.ID, ConnectionName: row.Name, SQL: sql, Digest: writeHash(target, sql), TargetDigest: target, RequestKey: requestKey, State: "prepared", CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(10 * time.Minute).Format(time.RFC3339Nano)}
	return store.PrepareDatasourceWrite(ctx, op)
}

func (s *Service) GetWrite(ctx context.Context, id string) (WriteOperation, error) {
	store, err := s.writeStore()
	if err != nil {
		return WriteOperation{}, err
	}
	return store.GetDatasourceWrite(ctx, id)
}

func (s *Service) ListWrites(ctx context.Context, connectionID string) ([]WriteOperation, error) {
	store, err := s.writeStore()
	if err != nil {
		return nil, err
	}
	if len(connectionID) != 26 {
		return nil, ErrPayloadInvalid
	}
	return store.ListDatasourceWrites(ctx, connectionID)
}

// CommitWrite never retries an external effect once its durable intent exists.
// A caller losing the ACK can fetch the completed receipt with this same ID.
func (s *Service) CommitWrite(ctx context.Context, id, digest string) (WriteOperation, error) {
	store, err := s.writeStore()
	if err != nil {
		return WriteOperation{}, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	op, err := store.GetDatasourceWrite(ctx, id)
	if err != nil {
		return op, err
	}
	if op.Digest != digest {
		return op, ErrWriteConflict
	}
	if op.State == "completed" {
		return op, nil
	}
	if op.State != "prepared" {
		return op, ErrWriteUnknown
	}
	expires, err := time.Parse(time.RFC3339Nano, op.ExpiresAt)
	if err != nil || !s.clock.Now().Before(expires) {
		return op, ErrWriteConflict
	}
	row, dsn, err := s.requireVerified(ctx, op.ConnectionID)
	if err != nil {
		return op, err
	}
	if writeHash(row.ID, row.Kind, dsn) != op.TargetDigest || !IsLocalDSN(row.Kind, dsn) {
		return op, ErrWriteConflict
	}
	if err := ValidateLocalWriteSQL(op.SQL); err != nil {
		return op, err
	}
	if s.writeQuerier == nil {
		return op, ErrServiceUnavailable
	}
	claimed, err := store.TransitionDatasourceWrite(ctx, id, "prepared", "executing", nil)
	if err != nil {
		return op, err
	}
	if !claimed {
		return op, ErrWriteUnknown
	}
	op.State = "executing"
	cols, rows, truncated, execErr := s.runQuerier(ctx, s.writeQuerier, row.Kind, dsn, op.SQL, nil, MaxQueryRows)
	// Receipt persistence must survive renderer cancellation after the commit.
	receiptCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	next := "completed"
	var result *QueryResult
	if execErr != nil {
		next = "unknown"
	} else {
		result = &QueryResult{Columns: cols, Rows: rows, RowCount: len(rows), Truncated: truncated}
	}
	stored, err := store.TransitionDatasourceWrite(receiptCtx, id, "executing", next, result)
	if err != nil || !stored {
		return op, ErrWriteUnknown
	}
	op.State = next
	op.Result = result
	if execErr != nil {
		return op, ErrWriteUnknown
	}
	return op, nil
}
