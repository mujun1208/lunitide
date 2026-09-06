package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/m9app"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/mroapp"
	"github.com/lunitide/lunitide/internal/org"
	"github.com/lunitide/lunitide/internal/talk"
)

func TestOrgLifecycleStopsBusinessWritesKeepsReadsAndResumes(t *testing.T) {
	ctx := context.Background()
	e, _, store := agentRunEngine(t)
	e.SetDataScopeStore(store)
	e.SetMROService(mroapp.New(store))
	admin := m9app.NewOrgAdminService(org.NewService(org.NewGate(store.OrgStorage()), nil), m9app.NewFileBindingStore(filepath.Join(t.TempDir(), "binding.json")))
	e.SetM9OrgAdminService(admin)
	created, err := admin.CreateOrg(ctx, "Lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Switch(ctx, created.OrgID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Activate(ctx); err != nil {
		t.Fatal(err)
	}
	member, err := admin.Invite(ctx, "member", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	closedMember, err := admin.Invite(ctx, "closed member", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	identity := org.NewService(org.NewGate(store.OrgStorage()), nil)
	orgCtx := org.WithVerifiedOrg(ctx, created.OrgID)
	role, err := identity.BindRole(orgCtx, member.PrincipalID, "", org.RoleMember, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	request := mroMutationRequest("mro.tool.upsert", `{"toolNo":"first","calibDue":"2099-01-01"}`, "first")
	if r := e.Handle(ctx, request); !r.OK {
		t.Fatal(r.Error)
	}
	if r := e.Handle(ctx, validRequest("org.suspend", `{}`)); !r.OK {
		t.Fatal(r.Error)
	}
	request.IdempotencyKey = "after-suspend"
	if r := e.Handle(ctx, request); r.OK || r.Error.Code != "M9-002" {
		t.Fatalf("suspended org accepted new write: %+v", r)
	}
	if r := e.Handle(ctx, validRequest("org.member.revoke", fmt.Sprintf(`{"principalId":%q}`, member.PrincipalID))); !r.OK {
		t.Fatalf("suspended revoke: %+v", r.Error)
	}
	if err = identity.RevokeBinding(orgCtx, role.BindingID); err != nil {
		t.Fatalf("suspended role revoke: %v", err)
	}
	principal, err := store.OrgStorage().PrincipalByID(ctx, created.OrgID, member.PrincipalID)
	if err != nil || principal.State != org.PrincipalRevoked || principal.BindingVersion != 2 {
		t.Fatalf("revocation was not durable: %+v %v", principal, err)
	}
	for _, method := range []string{"project.create", "template.create", "chat.start", "talk.start", "plan.run.start", "workflow.publish", "devTask.create", "ontology.node.create", "memory.create", "automation.job.set", "worker.dispatch", "org.space.create", "org.member.invite"} {
		release, err := e.authorizeDataRequest(ctx, method, json.RawMessage(`{}`))
		release()
		if !errors.Is(err, org.ErrOrgSuspended) {
			t.Errorf("%s did not reject suspension: %v", method, err)
		}
	}
	if r := e.Handle(ctx, validRequest("mro.tool.list", `{}`)); !r.OK {
		t.Fatal(r.Error)
	}
	for _, method := range []string{"chat.persist", "chat.checkpoint", "talk.persist", "agent.run.cancel", "plan.run.cancel", "command.cancel", "run.queue.withdraw"} {
		release, err := e.authorizeDataRequest(ctx, method, json.RawMessage(`{}`))
		release()
		if err != nil {
			t.Errorf("%s blocked cleanup: %v", method, err)
		}
	}
	if r := e.Handle(ctx, validRequest("org.activate", `{}`)); !r.OK {
		t.Fatal(r.Error)
	}
	if r := e.Handle(ctx, request); !r.OK {
		t.Fatal(r.Error)
	}
	if err = store.OrgStorage().UpdateOrgState(ctx, created.OrgID, org.OrgClosed, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	request.IdempotencyKey = "after-close"
	if r := e.Handle(ctx, request); r.OK || r.Error.Code != "M9-002" {
		t.Fatalf("closed org accepted new write: %+v", r)
	}
	if r := e.Handle(ctx, validRequest("mro.tool.list", `{}`)); !r.OK {
		t.Fatal(r.Error)
	}
	if r := e.Handle(ctx, validRequest("org.activate", `{}`)); r.OK {
		t.Fatal("closed organization resumed")
	}
	if r := e.Handle(ctx, validRequest("org.member.revoke", fmt.Sprintf(`{"principalId":%q}`, closedMember.PrincipalID))); !r.OK {
		t.Fatalf("closed revoke: %+v", r.Error)
	}
	tools, err := e.mro.ListToolViews(mroapp.WithScope(ctx, created.OrgID))
	if err != nil || len(tools) != 2 {
		t.Fatalf("failed writes left rows=%+v %v", tools, err)
	}
}

func TestOrgLifecycleSuspensionCancelsBeforeFenceAndPreservesActualReceipt(t *testing.T) {
	ctx := context.Background()
	e, _, store := agentRunEngine(t)
	e.SetDataScopeStore(store)
	messages, err := messageapp.New(store, store, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	e.messages = messages
	admin := m9app.NewOrgAdminService(org.NewService(org.NewGate(store.OrgStorage()), nil), m9app.NewFileBindingStore(filepath.Join(t.TempDir(), "binding.json")))
	e.SetM9OrgAdminService(admin)
	a, err := admin.CreateOrg(ctx, "A")
	if err != nil {
		t.Fatal(err)
	}
	b, err := admin.CreateOrg(ctx, "B")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Switch(ctx, a.OrgID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Activate(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := e.projects.Create(ctx, "receipt-project", "test", map[string]string{"name": "P"}, project.Project{Name: "P", OrgID: a.OrgID})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := e.sessions.Create(ctx, "receipt-session", "test", map[string]string{"projectId": p.ID}, session.Session{ProjectID: p.ID, Title: "Receipt"})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := e.authorizeDataRequest(ctx, "chat.start", json.RawMessage(fmt.Sprintf(`{"sessionId":%q}`, sess.ID)))
	if err != nil {
		t.Fatal(err)
	}
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	workerCtx, cancelWorker := context.WithCancel(ctx)
	defer cancelWorker()
	e.streams["suspend-test"] = &streamState{sessionID: sess.ID, cancel: cancelStream, state: streamRunning}
	e.planExecutions.workers = map[string]*planExecutionWorker{"suspend-test": {cancel: cancelWorker}}
	done := make(chan error, 1)
	go func() {
		r := e.Handle(ctx, validRequest("org.suspend", `{}`))
		if !r.OK {
			done <- fmt.Errorf("suspend: %+v", r.Error)
		} else {
			done <- nil
		}
	}()
	for _, stopping := range []context.Context{streamCtx, workerCtx} {
		select {
		case <-stopping.Done():
		case <-time.After(time.Second):
			lease()
			t.Fatal("cancellation blocked behind request fence")
		}
	}
	select {
	case err := <-done:
		lease()
		t.Fatalf("lifecycle passed an in-flight request: %v", err)
	default:
	}
	lease()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("suspension deadlocked")
	}
	transcript := &talkSession{talkID: "finished-before-suspension", sessionID: sess.ID}
	event := talk.ServerEvent{Final: true, Role: "user", ItemID: "receipt", Transcript: "already received final"}
	if _, err = e.persistTalkTranscript(transcript, event); err != nil {
		t.Fatalf("suspension discarded actual receipt: %v", err)
	}
	if r := e.Handle(ctx, validRequest("org.switch", fmt.Sprintf(`{"orgId":%q}`, b.OrgID))); !r.OK {
		t.Fatal(r.Error)
	}
	event.ItemID = "late-after-switch"
	if _, err = e.persistTalkTranscript(transcript, event); !errors.Is(err, org.ErrCrossOrgAccess) {
		t.Fatalf("late transcript crossed binding: %v", err)
	}
	page, err := messages.List(ctx, messageapp.PageRequest{SessionID: sess.ID, Direction: messageapp.Forward, Limit: 64, ByteBudget: messageapp.MaxByteBudget})
	if err != nil || len(page.Items) != 1 || page.Items[0].Text != "already received final" {
		t.Fatalf("receipt rows %+v %v", page.Items, err)
	}
}
