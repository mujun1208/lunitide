package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	projectdomain "github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/m9app"
	"github.com/lunitide/lunitide/internal/org"
)

func TestOfficeGenerateFromPersonalChatWhileOrgBound(t *testing.T) {
	e, store := officeEngineFixture(t)
	ctx := context.Background()
	parent, err := e.projects.Create(ctx, "personal-work-chat", "test", map[string]string{"name": "普通对话"}, projectdomain.Project{
		Name: "普通对话", Type: projectdomain.TypeImplementation, Status: projectdomain.StatusCreated,
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := e.sessions.Create(ctx, "personal-work-session", "test", map[string]string{"projectId": parent.ID, "title": "成绩表"}, session.Session{ProjectID: parent.ID, Title: "成绩表"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.officeStudio.Store.CreateOfficeTask(ctx, domain.Task{SessionID: sess.ID, Title: "办公文件", Status: "draft"}, "office-chat-"+sess.ID); err != nil {
		t.Fatal(err)
	}
	admin := m9app.NewOrgAdminService(
		org.NewService(org.NewGate(store.OrgStorage()), nil),
		m9app.NewFileBindingStore(filepath.Join(t.TempDir(), "binding.json")),
	)
	e.SetM9OrgAdminService(admin)
	e.SetDataScopeStore(store)
	if err := m9app.EnsureDefaultOrgBinding(ctx, admin); err != nil {
		t.Fatal(err)
	}
	summary, err := admin.Summary(ctx)
	if err != nil || summary.BoundOrgID == "" {
		t.Fatalf("organization binding: %+v %v", summary, err)
	}
	cases := []struct {
		kind, ext string
		args      string
	}{
		{"xlsx", ".xlsx", `{"path":"成绩.xlsx","title":"成绩","sheets":[{"name":"录入","headers":["姓名"],"rows":[["学生1"]]}]}`},
		{"docx", ".docx", `{"path":"说明.docx","title":"说明","kind":"report","blocks":[{"type":"paragraph","text":"成绩表说明"}]}`},
		{"pptx", ".pptx", `{"path":"汇报.pptx","title":"汇报","slides":[{"title":"成绩","bullets":["学生1"]}]}`},
	}
	for _, tc := range cases {
		data, name, output, err := e.executeOfficeTool(ctx, sess.ID, "office.generate", json.RawMessage(tc.args))
		if err != nil || len(data) == 0 {
			t.Fatalf("%s generate from personal Work chat while org-bound: name=%q output=%q err=%v", tc.kind, name, output, err)
		}
		if !strings.Contains(name, tc.ext) || !strings.Contains(output, "文件已存档") {
			t.Fatalf("%s delivery: name=%q output=%q", tc.kind, name, output)
		}
	}
}

func TestOfficeGenerateInBoundOrgChatStillWorks(t *testing.T) {
	e, store := officeEngineFixture(t)
	ctx := context.Background()
	admin := m9app.NewOrgAdminService(
		org.NewService(org.NewGate(store.OrgStorage()), nil),
		m9app.NewFileBindingStore(filepath.Join(t.TempDir(), "binding.json")),
	)
	e.SetM9OrgAdminService(admin)
	e.SetDataScopeStore(store)
	if err := m9app.EnsureDefaultOrgBinding(ctx, admin); err != nil {
		t.Fatal(err)
	}
	pid, err := e.ensurePersonalChatProject(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := e.sessions.Create(ctx, "bound-org-session", "test", map[string]string{"projectId": pid, "title": "周报"}, session.Session{ProjectID: pid, Title: "周报"})
	if err != nil {
		t.Fatal(err)
	}
	data, name, output, err := e.executeOfficeTool(ctx, sess.ID, "office.generate", json.RawMessage(`{"path":"周报.xlsx","title":"周报","sheets":[{"name":"事项","headers":["项"],"rows":[["完成"]]}]}`))
	if err != nil || len(data) == 0 || !strings.Contains(name, ".xlsx") || !strings.Contains(output, "文件已存档") {
		t.Fatalf("bound org chat generate: name=%q output=%q err=%v", name, output, err)
	}
}

func TestOfficeGenerateStillDeniesForeignOrgSession(t *testing.T) {
	e, store := officeEngineFixture(t)
	ctx := context.Background()
	admin := m9app.NewOrgAdminService(
		org.NewService(org.NewGate(store.OrgStorage()), nil),
		m9app.NewFileBindingStore(filepath.Join(t.TempDir(), "binding.json")),
	)
	e.SetM9OrgAdminService(admin)
	e.SetDataScopeStore(store)
	a, err := admin.CreateOrg(ctx, "A")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Switch(ctx, a.OrgID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Activate(ctx); err != nil {
		t.Fatal(err)
	}
	pid, err := e.ensurePersonalChatProject(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := e.sessions.Create(ctx, "foreign-org-session", "test", map[string]string{"projectId": pid, "title": "外组织"}, session.Session{ProjectID: pid, Title: "外组织"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := admin.CreateOrg(ctx, "B")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Switch(ctx, b.OrgID); err != nil {
		t.Fatal(err)
	}
	_, _, output, err := e.executeOfficeTool(ctx, sess.ID, "office.generate", json.RawMessage(`{"path":"x.xlsx","title":"x","sheets":[{"name":"s","headers":["a"],"rows":[["1"]]}]}`))
	if !errors.Is(err, org.ErrCrossOrgAccess) {
		t.Fatalf("foreign session must stay M9-003: output=%q err=%v", output, err)
	}
}
