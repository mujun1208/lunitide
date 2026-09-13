package projectschema

import (
	"path/filepath"
	"testing"
)

func sample() Schema {
	return Schema{Version: 1, Dialect: "sqlite", Tables: []Table{{
		Name: "orders",
		Columns: []Column{
			{Name: "id", Type: "TEXT", NotNull: true, PrimaryKey: true},
			{Name: "title", Type: "TEXT", NotNull: true},
		},
	}}}
}

func TestParseRejectsDropAndBadNames(t *testing.T) {
	if _, err := Parse([]byte(`{"version":1,"tables":[{"name":"orders;drop","columns":[{"name":"id","type":"TEXT"}]}]}`)); err != ErrSchemaInvalid {
		t.Fatalf("bad name: %v", err)
	}
	if _, err := BindPath(`C:\work\mall`, `C:\other\app.sqlite`); err != ErrDBBindInvalid {
		t.Fatalf("outside root: %v", err)
	}
}

func TestMaterializeAndVerifyIdempotent(t *testing.T) {
	root := t.TempDir()
	s := sample()
	if err := WriteFile(root, s); err != nil {
		t.Fatal(err)
	}
	path := DefaultDBPath(root)
	if err := Materialize(path, s); err != nil {
		t.Fatal(err)
	}
	if err := Verify(path, s); err != nil {
		t.Fatal(err)
	}
	if err := Materialize(path, s); err != nil {
		t.Fatal(err)
	}
	s.Tables[0].Columns = append(s.Tables[0].Columns, Column{Name: "note", Type: "TEXT"})
	if err := Materialize(path, s); err != nil {
		t.Fatal(err)
	}
	if err := Verify(path, s); err != nil {
		t.Fatal(err)
	}
	missing := sample()
	missing.Tables = append(missing.Tables, Table{Name: "missing", Columns: []Column{{Name: "id", Type: "TEXT", PrimaryKey: true}}})
	if err := Verify(path, missing); err != ErrDBIncomplete {
		t.Fatalf("missing table: %v", err)
	}
	bound, err := BindPath(root, filepath.Join(".lunitide", "data", "app.sqlite"))
	if err != nil || bound != path && filepath.Clean(bound) != filepath.Clean(path) {
		t.Fatalf("bind %s %v want %s", bound, err, path)
	}
}

func TestExtractJSON(t *testing.T) {
	doc := "说明\n```json\n{\"version\":1,\"tables\":[{\"name\":\"orders\",\"columns\":[{\"name\":\"id\",\"type\":\"TEXT\"}]}]}\n```\n"
	if _, ok := ExtractJSON(doc); !ok {
		t.Fatal("extract")
	}
}
