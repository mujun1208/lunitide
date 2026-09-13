package projectschema

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "modernc.org/sqlite"
)

var (
	ErrSchemaInvalid   = errors.New("project database schema is invalid")
	ErrDBBindInvalid   = errors.New("project database path is invalid")
	ErrDBFailed        = errors.New("project database materialize failed")
	ErrDBIncomplete    = errors.New("project database tables are incomplete")
)

var ident = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Column struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	NotNull    bool   `json:"notNull,omitempty"`
	PrimaryKey bool   `json:"primaryKey,omitempty"`
}

type Table struct {
	Name    string   `json:"name"`
	Columns []Column `json:"columns"`
}

type Schema struct {
	Version int     `json:"version"`
	Dialect string  `json:"dialect"`
	Tables  []Table `json:"tables"`
}

func Parse(raw []byte) (Schema, error) {
	var s Schema
	if json.Unmarshal(raw, &s) != nil || s.Version != 1 {
		return Schema{}, ErrSchemaInvalid
	}
	if s.Dialect == "" {
		s.Dialect = "sqlite"
	}
	if s.Dialect != "sqlite" {
		return Schema{}, ErrSchemaInvalid
	}
	if err := Validate(s); err != nil {
		return Schema{}, err
	}
	return s, nil
}

func Validate(s Schema) error {
	if s.Version != 1 || len(s.Tables) == 0 {
		return ErrSchemaInvalid
	}
	seen := map[string]bool{}
	for _, table := range s.Tables {
		if !ident.MatchString(table.Name) || len(table.Name) > 64 || seen[table.Name] || len(table.Columns) == 0 {
			return ErrSchemaInvalid
		}
		seen[table.Name] = true
		cols := map[string]bool{}
		for _, col := range table.Columns {
			if !ident.MatchString(col.Name) || len(col.Name) > 64 || cols[col.Name] || !validType(col.Type) {
				return ErrSchemaInvalid
			}
			cols[col.Name] = true
		}
	}
	return nil
}

func validType(t string) bool {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "TEXT", "INTEGER", "REAL", "BLOB", "NUMERIC":
		return true
	default:
		return false
	}
}

func DefaultDBPath(root string) string {
	return filepath.Join(root, ".lunitide", "data", "app.sqlite")
}

func BindPath(root, candidate string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", ErrDBBindInvalid
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", ErrDBBindInvalid
	}
	path := strings.TrimSpace(candidate)
	if path == "" {
		return DefaultDBPath(absRoot), nil
	}
	if filepath.IsAbs(path) {
		// ok
	} else {
		path = filepath.Join(absRoot, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", ErrDBBindInvalid
	}
	rel, err := filepath.Rel(absRoot, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", ErrDBBindInvalid
	}
	ext := strings.ToLower(filepath.Ext(abs))
	if ext != ".sqlite" && ext != ".db" {
		return "", ErrDBBindInvalid
	}
	return abs, nil
}

func WriteFile(root string, s Schema) error {
	if err := Validate(s); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, ".lunitide"), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, ".lunitide", "schema.json"), body, 0o644)
}

func ReadFile(root string) (Schema, error) {
	raw, err := os.ReadFile(filepath.Join(root, ".lunitide", "schema.json"))
	if err != nil {
		return Schema{}, err
	}
	return Parse(raw)
}

func Materialize(dbPath string, s Schema) error {
	if err := Validate(s); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return ErrDBFailed
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return ErrDBFailed
	}
	defer db.Close()
	for _, table := range s.Tables {
		if _, err = db.Exec(createSQL(table)); err != nil {
			return fmt.Errorf("%w: %s", ErrDBFailed, err)
		}
		if err = addMissingColumns(db, table); err != nil {
			return err
		}
	}
	return nil
}

func Verify(dbPath string, s Schema) error {
	if err := Validate(s); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return ErrDBIncomplete
	}
	defer db.Close()
	for _, table := range s.Tables {
		rows, err := db.Query(`PRAGMA table_info(` + quoteIdent(table.Name) + `)`)
		if err != nil {
			return ErrDBIncomplete
		}
		have := map[string]bool{}
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt sql.NullString
			if err = rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				rows.Close()
				return ErrDBIncomplete
			}
			have[name] = true
		}
		rows.Close()
		if len(have) == 0 {
			return ErrDBIncomplete
		}
		for _, col := range table.Columns {
			if !have[col.Name] {
				return ErrDBIncomplete
			}
		}
	}
	return nil
}

func createSQL(table Table) string {
	parts := make([]string, 0, len(table.Columns))
	var pks []string
	for _, col := range table.Columns {
		decl := quoteIdent(col.Name) + " " + strings.ToUpper(col.Type)
		if col.NotNull {
			decl += " NOT NULL"
		}
		if col.PrimaryKey {
			pks = append(pks, quoteIdent(col.Name))
		}
		parts = append(parts, decl)
	}
	if len(pks) > 0 {
		parts = append(parts, "PRIMARY KEY ("+strings.Join(pks, ",")+")")
	}
	return "CREATE TABLE IF NOT EXISTS " + quoteIdent(table.Name) + " (" + strings.Join(parts, ", ") + ")"
}

func addMissingColumns(db *sql.DB, table Table) error {
	rows, err := db.Query(`PRAGMA table_info(` + quoteIdent(table.Name) + `)`)
	if err != nil {
		return ErrDBFailed
	}
	have := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err = rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return ErrDBFailed
		}
		have[name] = true
	}
	rows.Close()
	for _, col := range table.Columns {
		if have[col.Name] {
			continue
		}
		sqlText := "ALTER TABLE " + quoteIdent(table.Name) + " ADD COLUMN " + quoteIdent(col.Name) + " " + strings.ToUpper(col.Type)
		if _, err = db.Exec(sqlText); err != nil {
			return fmt.Errorf("%w: %s", ErrDBFailed, err)
		}
	}
	return nil
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, ``) + `"`
}

func ExtractJSON(doc string) (Schema, bool) {
	start := strings.Index(doc, "```json")
	if start < 0 {
		return Schema{}, false
	}
	rest := doc[start+7:]
	end := strings.Index(rest, "```")
	if end < 0 {
		return Schema{}, false
	}
	s, err := Parse([]byte(strings.TrimSpace(rest[:end])))
	if err != nil {
		return Schema{}, false
	}
	return s, true
}
