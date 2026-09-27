package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenPageFileEditLandsOutsideTheSession(t *testing.T) {
	r := openHooksRuntime(t)
	dir := t.TempDir()
	page := filepath.Join(dir, "index.html")
	other := filepath.Join(dir, "other.html")
	if err := os.WriteFile(page, []byte("<nav>商机</nav>\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	edit, _ := json.Marshal(map[string]string{"path": page, "oldText": "<nav>商机</nav>", "newText": "<nav>商机</nav><nav>客户管理</nav>"})
	if _, err := r.Execute(context.Background(), AutoEdit, session, "workspace.edit", edit, false); err == nil {
		t.Fatal("an absolute page edit must stay blocked until that page is the one on screen")
	}
	ctx := WithOpenPageFile(context.Background(), page)
	if _, err := r.Execute(ctx, Approval, session, "workspace.edit", edit, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(page)
	if err != nil || !strings.Contains(string(got), "客户管理") {
		t.Fatalf("page was not updated: %s %v", got, err)
	}
	otherEdit, _ := json.Marshal(map[string]string{"path": other, "oldText": "keep", "newText": "changed"})
	if _, err := r.Execute(ctx, AutoEdit, session, "workspace.edit", otherEdit, false); err == nil {
		t.Fatal("a different file must stay blocked")
	}
	kept, _ := os.ReadFile(other)
	if string(kept) != "keep" {
		t.Fatalf("other file changed: %s", kept)
	}
}
