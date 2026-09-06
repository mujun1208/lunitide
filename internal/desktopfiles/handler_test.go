package desktopfiles

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
)

func testReq(method, payload string) bridge.Request {
	return bridge.Request{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", TraceID: "01ARZ3NDEKTSV4RRFFQ69G5FAW",
		Method: method, Payload: json.RawMessage(payload),
	}
}

func TestPickCancelIsSuccess(t *testing.T) {
	h := New()
	h.Pick = func(folder, multiple bool) ([]Item, []string, error) {
		if folder || !multiple {
			t.Fatalf("unexpected pick args folder=%v multiple=%v", folder, multiple)
		}
		return nil, nil, ErrCanceled
	}
	r := h.HandleHost(context.Background(), testReq("desktop.files.pick", `{"multiple":true}`))
	if !r.OK {
		t.Fatalf("cancel must be ok: %#v", r)
	}
	raw, _ := json.Marshal(r.Payload)
	var out struct {
		Canceled bool  `json:"canceled"`
		Items    []any `json:"items"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || !out.Canceled || len(out.Items) != 0 {
		t.Fatalf("cancel payload = %s", raw)
	}
}

func TestReadChunkDeniesUnpickedPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(path, []byte("nope"), 0600); err != nil {
		t.Fatal(err)
	}
	h := New()
	r := h.HandleHost(context.Background(), testReq("desktop.files.readChunk", `{"path":`+jsonString(path)+`,"offset":0,"limit":16}`))
	if r.OK || r.Error == nil || r.Error.Code != codeDenied {
		t.Fatalf("unpicked read = %#v", r)
	}
}

func TestReadChunkAllowsPickedFileAndExpires(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("hello-world"), 0600); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	h := New()
	h.Now = func() time.Time { return now }
	h.Pick = func(bool, bool) ([]Item, []string, error) {
		return []Item{{Path: abs, FileName: "notes.txt", MIME: "text/plain", Size: 11}}, nil, nil
	}
	picked := h.HandleHost(context.Background(), testReq("desktop.files.pick", `{}`))
	if !picked.OK {
		t.Fatalf("pick = %#v", picked)
	}
	chunk := h.HandleHost(context.Background(), testReq("desktop.files.readChunk", `{"path":`+jsonString(abs)+`,"offset":0,"limit":16}`))
	if !chunk.OK {
		t.Fatalf("read = %#v", chunk)
	}
	chunkRaw, _ := json.Marshal(chunk.Payload)
	var out struct {
		ContentBase64 string `json:"contentBase64"`
		EOF           bool   `json:"eof"`
	}
	if err := json.Unmarshal(chunkRaw, &out); err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(out.ContentBase64)
	if string(raw) != "hello-world" || !out.EOF {
		t.Fatalf("chunk = %q eof=%v", raw, out.EOF)
	}
	h.Now = func() time.Time { return now.Add(11 * time.Minute) }
	expired := h.HandleHost(context.Background(), testReq("desktop.files.readChunk", `{"path":`+jsonString(abs)+`,"offset":0,"limit":16}`))
	if expired.OK || expired.Error == nil || expired.Error.Code != codeDenied {
		t.Fatalf("expired read = %#v", expired)
	}
}

func jsonString(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestPickFolderWithOnlyExeIsNotDialogFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "setup.exe"), []byte("MZ"), 0600); err != nil {
		t.Fatal(err)
	}
	h := New()
	h.Pick = func(folder, multiple bool) ([]Item, []string, error) {
		if !folder {
			t.Fatal("expected folder pick")
		}
		return listFolder(dir)
	}
	r := h.HandleHost(context.Background(), testReq("desktop.files.pick", `{"folder":true}`))
	if !r.OK {
		t.Fatalf("filtered-empty folder must be ok: %#v", r)
	}
	raw, _ := json.Marshal(r.Payload)
	var out struct {
		Canceled bool     `json:"canceled"`
		Items    []any    `json:"items"`
		Skipped  []string `json:"skipped"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Canceled || len(out.Items) != 0 {
		t.Fatalf("payload = %s", raw)
	}
	if len(out.Skipped) != 1 || out.Skipped[0] != "setup.exe" {
		t.Fatalf("skipped = %#v", out.Skipped)
	}
}

func TestPickFolderNamesSkippedExeAlongsideTxt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "setup.exe"), []byte("MZ"), 0600); err != nil {
		t.Fatal(err)
	}
	h := New()
	h.Pick = func(folder, _ bool) ([]Item, []string, error) {
		if !folder {
			t.Fatal("expected folder pick")
		}
		return listFolder(dir)
	}
	r := h.HandleHost(context.Background(), testReq("desktop.files.pick", `{"folder":true}`))
	if !r.OK {
		t.Fatalf("mixed folder must be ok: %#v", r)
	}
	raw, _ := json.Marshal(r.Payload)
	var out struct {
		Canceled bool `json:"canceled"`
		Items    []struct {
			FileName string `json:"fileName"`
		} `json:"items"`
		Skipped []string `json:"skipped"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Canceled || len(out.Items) != 1 || out.Items[0].FileName != "notes.txt" {
		t.Fatalf("items = %s", raw)
	}
	if len(out.Skipped) != 1 || out.Skipped[0] != "setup.exe" {
		t.Fatalf("skipped = %#v", out.Skipped)
	}
}

func TestPickFallsBackWhenFormsUnavailable(t *testing.T) {
	origForms, origNative := pickForms, pickNative
	t.Cleanup(func() {
		pickForms, pickNative = origForms, origNative
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("hi"), 0600); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	var usedNative bool
	pickForms = func(bool, bool) ([]Item, []string, error) {
		return nil, nil, ErrUnavailable
	}
	pickNative = func(bool, bool) ([]Item, []string, error) {
		usedNative = true
		return []Item{{Path: abs, FileName: "notes.txt", MIME: "text/plain", Size: 2}}, nil, nil
	}
	h := New()
	r := h.HandleHost(context.Background(), testReq("desktop.files.pick", `{"multiple":true}`))
	if !r.OK || !usedNative {
		t.Fatalf("native fallback not reached: %#v usedNative=%v", r, usedNative)
	}
	raw, _ := json.Marshal(r.Payload)
	var out struct {
		Canceled bool `json:"canceled"`
		Items    []struct {
			FileName string `json:"fileName"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Canceled || len(out.Items) != 1 || out.Items[0].FileName != "notes.txt" {
		t.Fatalf("fallback payload = %s", raw)
	}
}

func TestPickFormsCancelDoesNotCallNative(t *testing.T) {
	origForms, origNative := pickForms, pickNative
	t.Cleanup(func() {
		pickForms, pickNative = origForms, origNative
	})
	var usedNative bool
	pickForms = func(bool, bool) ([]Item, []string, error) {
		return nil, nil, ErrCanceled
	}
	pickNative = func(bool, bool) ([]Item, []string, error) {
		usedNative = true
		return nil, nil, ErrUnavailable
	}
	h := New()
	r := h.HandleHost(context.Background(), testReq("desktop.files.pick", `{"multiple":true}`))
	if !r.OK || usedNative {
		t.Fatalf("cancel must not fall back: %#v usedNative=%v", r, usedNative)
	}
}
