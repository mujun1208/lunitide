package artifactreview

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o600)
}

const sess = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

func TestAppendAndListBySessionAcceptState(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Append(sess, "call-1", "excel.gen", "xlsx", "out/a.xlsx", ActionComment, "表头字号太小"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Append(sess, "call-2", "excel.gen", "xlsx", "out/a.xlsx", ActionRevise, "改为 12 号"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Append(sess, "call-3", "docx.gen", "docx", "out/b.docx", ActionAccept, ""); err != nil {
		t.Fatal(err)
	}
	list, accepted, err := s.ListBySession(sess)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("list = %d", len(list))
	}
	if !accepted["out/b.docx"] || accepted["out/a.xlsx"] {
		t.Fatalf("accepted = %v", accepted)
	}
	// Revisions flip state: a later comment on b.docx un-accepts it.
	if _, err = s.Append(sess, "call-4", "docx.gen", "docx", "out/b.docx", ActionComment, "再补一节"); err != nil {
		t.Fatal(err)
	}
	_, accepted, _ = s.ListBySession(sess)
	if accepted["out/b.docx"] {
		t.Fatal("stale accept still wins after newer comment")
	}
	// Session isolation.
	other := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	if _, err = s.Append(other, "call-5", "pdf.gen", "pdf", "out/b.docx", ActionAccept, ""); err != nil {
		t.Fatal(err)
	}
	list, accepted, _ = s.ListBySession(sess)
	if len(list) != 4 || accepted["out/b.docx"] {
		t.Fatalf("cross-session leak: %d %v", len(list), accepted)
	}
}

func TestAppendValidationAndPersistence(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	if _, err := s.Append("short", "c", "t", "xlsx", "p", ActionAccept, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad session accepted: %v", err)
	}
	if _, err := s.Append(sess, "c", "t", "exe", "p", ActionAccept, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad kind accepted: %v", err)
	}
	if _, err := s.Append(sess, "c", "t", "xlsx", "p", "reject", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad action accepted: %v", err)
	}
	// Reload from disk (new store instance) answers the same rows.
	if _, err := s.Append(sess, "c1", "excel.gen", "xlsx", "a.xlsx", ActionAccept, "ok"); err != nil {
		t.Fatal(err)
	}
	s2, _ := NewStore(dir)
	list, accepted, err := s2.ListBySession(sess)
	if err != nil || len(list) != 1 || !accepted["a.xlsx"] {
		t.Fatalf("reload lost rows: %v %v %v", list, accepted, err)
	}
	// Corrupt file stays fail-closed.
	if err := writeFile(filepath.Join(dir, "artifact-reviews.json"), "not json"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s2.ListBySession(sess); err == nil {
		t.Fatal("corrupt log silently reset")
	}
}
