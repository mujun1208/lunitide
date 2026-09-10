package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/canonpath"
	"github.com/lunitide/lunitide/internal/conversationsapp"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

func TestConversationsRootSetMissingDirUsesChinese(t *testing.T) {
	e := NewEngine(nil, "test")
	e.SetConversationsStore(conversationsapp.New(filepath.Join(t.TempDir(), "conversations-root.json"), t.TempDir()))
	missing := filepath.Join(t.TempDir(), "missing-root")
	raw, err := json.Marshal(map[string]string{"path": missing})
	if err != nil {
		t.Fatal(err)
	}
	resp := e.Handle(context.Background(), validRequest("conversations.root.set", string(raw)))
	if resp.OK || resp.Error == nil || resp.Error.Code != "CONVERSATIONS_ROOT_INVALID" {
		t.Fatalf("missing conversations root %#v", resp)
	}
	if !strings.Contains(resp.Error.Message, "存在的本地目录") || strings.Contains(resp.Error.Message, "existing directory") {
		t.Fatalf("conversations root must stay Chinese: %q", resp.Error.Message)
	}
}

func TestSessionFolderGetUnwiredUsesChinese(t *testing.T) {
	e := NewEngine(nil, "test")
	resp := e.Handle(context.Background(), validRequest("session.folder.get", `{"sessionId":"`+ulid.Make().String()+`"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "SESSION_FOLDER_UNAVAILABLE" {
		t.Fatalf("unwired session folder %#v", resp)
	}
	if !strings.Contains(resp.Error.Message, "会话目录") || strings.Contains(resp.Error.Message, "unavailable") {
		t.Fatalf("session folder must stay Chinese: %q", resp.Error.Message)
	}
}

func TestResolveSessionArtifactTarget(t *testing.T) {
	root := t.TempDir()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	sessionDir := filepath.Join(root, session)
	if err := os.MkdirAll(sessionDir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sessionDir, "report.html")
	if err := os.WriteFile(file, []byte("<html></html>"), 0600); err != nil {
		t.Fatal(err)
	}
	e := &Engine{}
	rt, err := toolruntime.New(filepath.Join(root, "legacy"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	e.tools = rt
	e.tools.SetSessionStorageRoot(func() (string, error) { return root, nil })

	got, err := e.resolveSessionArtifactTarget(session, "report.html")
	if err != nil {
		t.Fatal(err)
	}
	// The resolver reports where the file really is, which on Windows means
	// short components like RUNNER~1 expanded to their real names. t.TempDir
	// reports whatever spelling TMP carries, so the expectation has to be put
	// in the same terms; otherwise this passes only where the profile name is
	// short enough to need no 8.3 alias.
	want, err := canonpath.Canonical(file)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}

	if _, err := e.resolveSessionArtifactTarget(session, `..\..\..\windows\system.ini`); err == nil {
		t.Fatal("expected traversal to fail")
	}
	if _, err := e.resolveSessionArtifactTarget(session, `C:\Windows\System.ini`); err == nil {
		t.Fatal("expected absolute path to fail")
	}
}
