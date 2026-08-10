package sqlite

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/oklog/ulid/v2"
)

type messageFixture struct {
	store     *Store
	app       *messageapp.Service
	projectID string
	sessionID string
}

var testCursorKey = []byte("0123456789abcdef0123456789abcdef")

func newMessageApp(t *testing.T, s *Store) *messageapp.Service {
	t.Helper()
	a, e := messageapp.New(s, s, testCursorKey)
	if e != nil {
		t.Fatal(e)
	}
	return a
}

func newMessageFixture(t *testing.T, name string) messageFixture {
	t.Helper()
	s := openAppStore(t, name)
	projectID := createSessionProject(t, s, name+"-project", "Message Project "+name)
	created, err := sessionapp.New(s, s).Create(context.Background(), name+"-session", "tester", struct{ ProjectID, Title string }{projectID, "Messages"}, session.Session{ProjectID: projectID, Title: "Messages"})
	if err != nil {
		t.Fatal(err)
	}
	return messageFixture{s, newMessageApp(t, s), projectID, created.ID}
}

func (f messageFixture) append(t *testing.T, key, text string) message.Message {
	t.Helper()
	v, err := f.app.Append(context.Background(), key, "tester", struct{ SessionID, Text string }{f.sessionID, text}, message.Message{SessionID: f.sessionID, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func appendN(t *testing.T, f messageFixture, start, n int, text func(int) string) []message.Message {
	t.Helper()
	out := make([]message.Message, 0, n)
	for i := start; i < start+n; i++ {
		out = append(out, f.append(t, fmt.Sprintf("message-%06d", i), text(i)))
	}
	return out
}

func collectPages(t *testing.T, app *messageapp.Service, sessionID string, direction messageapp.Direction, limit, budget int, firstCursor string) ([]int64, int64) {
	t.Helper()
	cursor := firstCursor
	var got []int64
	var snapshot int64
	for pages := 0; ; pages++ {
		if pages > 1000 {
			t.Fatal("pagination did not terminate")
		}
		p, err := app.List(context.Background(), messageapp.PageRequest{SessionID: sessionID, Cursor: cursor, Direction: direction, Limit: limit, ByteBudget: budget, RequestID: "01ARZ3NDEKTSV4RRFFQ69G5FAV"})
		if err != nil {
			t.Fatal(err)
		}
		if pages == 0 {
			snapshot = p.SnapshotSequence
		} else if p.SnapshotSequence != snapshot {
			t.Fatalf("snapshot changed from %d to %d", snapshot, p.SnapshotSequence)
		}
		for _, item := range p.Items {
			got = append(got, item.Sequence)
		}
		if !p.HasMore {
			if p.NextCursor != nil {
				t.Fatal("terminal page retained cursor")
			}
			return got, snapshot
		}
		if p.NextCursor == nil {
			t.Fatal("nonterminal page has nil cursor")
		}
		cursor = *p.NextCursor
	}
}

func assertExactSequences(t *testing.T, got []int64, n int, descending bool) {
	t.Helper()
	if len(got) != n {
		t.Fatalf("got %d sequences, want %d", len(got), n)
	}
	seen := make(map[int64]bool, n)
	for i, sequence := range got {
		want := int64(i + 1)
		if descending {
			want = int64(n - i)
		}
		if sequence != want || seen[sequence] {
			t.Fatalf("sequence[%d]=%d want=%d duplicate=%v", i, sequence, want, seen[sequence])
		}
		seen[sequence] = true
	}
}

func TestMessageAppend257And258AndBidirectionalPaginationIsComplete(t *testing.T) {
	f := newMessageFixture(t, "page-258")
	items := appendN(t, f, 1, 258, func(i int) string { return fmt.Sprintf("message %03d", i) })
	if items[256].Sequence != 257 || items[257].Sequence != 258 {
		t.Fatalf("actual boundary appends got sequences %d and %d", items[256].Sequence, items[257].Sequence)
	}
	forward, snapshot := collectPages(t, f.app, f.sessionID, messageapp.Forward, 17, messageapp.MaxByteBudget, "")
	if snapshot != 258 {
		t.Fatalf("forward snapshot=%d", snapshot)
	}
	assertExactSequences(t, forward, 258, false)
	backward, snapshot := collectPages(t, f.app, f.sessionID, messageapp.Backward, 19, messageapp.MaxByteBudget, "")
	if snapshot != 258 {
		t.Fatalf("backward snapshot=%d", snapshot)
	}
	assertExactSequences(t, backward, 258, true)
}

func TestMessagePaginationSnapshotExcludesLaterAppend(t *testing.T) {
	f := newMessageFixture(t, "snapshot")
	appendN(t, f, 1, 40, func(i int) string { return fmt.Sprintf("before %d", i) })
	first, err := f.app.List(context.Background(), messageapp.PageRequest{SessionID: f.sessionID, Direction: messageapp.Forward, Limit: 7, ByteBudget: messageapp.MaxByteBudget})
	if err != nil || first.NextCursor == nil || first.SnapshotSequence != 40 {
		t.Fatalf("first page=%#v err=%v", first, err)
	}
	later := f.append(t, "later-message", "after snapshot")
	got, snapshot := collectPages(t, f.app, f.sessionID, messageapp.Forward, 7, messageapp.MaxByteBudget, *first.NextCursor)
	all := make([]int64, 0, 40)
	for _, item := range first.Items {
		all = append(all, item.Sequence)
	}
	all = append(all, got...)
	assertExactSequences(t, all, 40, false)
	if snapshot != 40 || later.Sequence != 41 {
		t.Fatalf("continued snapshot=%d later=%d", snapshot, later.Sequence)
	}
}

func mutateCursor(t *testing.T, raw string, mutate func(map[string]any)) string {
	t.Helper()
	b, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err = json.Unmarshal(b, &value); err != nil {
		t.Fatal(err)
	}
	mutate(value)
	b, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func TestMessagePaginationRejectsCrossSessionDirectionAndTamperedCursor(t *testing.T) {
	f := newMessageFixture(t, "cursor-a")
	other := newMessageFixture(t, "cursor-b")
	appendN(t, f, 1, 3, func(i int) string { return fmt.Sprint(i) })
	p, err := f.app.List(context.Background(), messageapp.PageRequest{SessionID: f.sessionID, Direction: messageapp.Forward, Limit: 1, ByteBudget: messageapp.MaxByteBudget})
	if err != nil || p.NextCursor == nil {
		t.Fatalf("page=%#v err=%v", p, err)
	}
	cases := []struct {
		name, session string
		direction     messageapp.Direction
		cursor        string
	}{
		{"session", other.sessionID, messageapp.Forward, *p.NextCursor},
		{"direction", f.sessionID, messageapp.Backward, *p.NextCursor},
		{"content", f.sessionID, messageapp.Forward, mutateCursor(t, *p.NextCursor, func(v map[string]any) { v["b"] = float64(2) })},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.app.List(context.Background(), messageapp.PageRequest{SessionID: tc.session, Direction: tc.direction, Cursor: tc.cursor, Limit: 1, ByteBudget: messageapp.MaxByteBudget})
			if !errors.Is(err, messageapp.ErrCursorInvalid) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestMessagePaginationItemLimitAndByteBudgetBoundCompleteBridgeEnvelope(t *testing.T) {
	f := newMessageFixture(t, "budgets")
	appendN(t, f, 1, 12, func(i int) string { return strings.Repeat("x", 1800) })
	t.Run("item limit", func(t *testing.T) {
		p, err := f.app.List(context.Background(), messageapp.PageRequest{SessionID: f.sessionID, Direction: messageapp.Forward, Limit: 3, ByteBudget: messageapp.MaxByteBudget, RequestID: "request-item"})
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Items) != 3 || !p.HasMore {
			t.Fatalf("items=%d more=%v", len(p.Items), p.HasMore)
		}
	})
	t.Run("byte budget", func(t *testing.T) {
		const requested = 16384
		requestID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
		p, err := f.app.List(context.Background(), messageapp.PageRequest{SessionID: f.sessionID, Direction: messageapp.Forward, Limit: 12, ByteBudget: requested, RequestID: requestID})
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Items) >= 12 || !p.HasMore {
			t.Fatalf("byte budget did not truncate: items=%d more=%v", len(p.Items), p.HasMore)
		}
		raw, err := json.Marshal(bridge.Success(requestID, p))
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) >= requested || len(raw) >= 245760 {
			t.Fatalf("complete bridge envelope len=%d requested=%d", len(raw), requested)
		}
	})
}

func TestMessagePaginationGapAndMissingPartFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		corrupt func(*testing.T, messageFixture)
	}{
		{"gap", func(t *testing.T, f messageFixture) {
			if _, err := f.store.db.Exec(`DELETE FROM messages WHERE session_id=? AND sequence=2`, f.sessionID); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing part", func(t *testing.T, f messageFixture) {
			if _, err := f.store.db.Exec(`DELETE FROM message_parts WHERE message_id=(SELECT id FROM messages WHERE session_id=? AND sequence=2)`, f.sessionID); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMessageFixture(t, "closed-"+strings.ReplaceAll(tc.name, " ", "-"))
			appendN(t, f, 1, 3, func(i int) string { return fmt.Sprint(i) })
			tc.corrupt(t, f)
			_, err := f.app.List(context.Background(), messageapp.PageRequest{SessionID: f.sessionID, Direction: messageapp.Forward, Limit: 10, ByteBudget: messageapp.MaxByteBudget})
			if !errors.Is(err, messageapp.ErrDataInvariantViolation) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestMessageConcurrentAppendSequencesAreUniqueAndContinuous(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "concurrent.db")
	a, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	projectID := createSessionProject(t, a, "concurrent-project", "Concurrent")
	created, err := sessionapp.New(a, a).Create(ctx, "concurrent-session", "tester", struct{ ProjectID, Title string }{projectID, "Concurrent"}, session.Session{ProjectID: projectID, Title: "Concurrent"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	apps := []*messageapp.Service{newMessageApp(t, a), newMessageApp(t, b)}
	const workers = 64
	start := make(chan struct{})
	results := make(chan message.Message, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			v, e := apps[i%2].Append(ctx, fmt.Sprintf("concurrent-%d", i), "tester", struct{ N int }{i}, message.Message{SessionID: created.ID, Text: fmt.Sprintf("message %d", i)})
			results <- v
			errs <- e
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	sequences := make([]int, 0, workers)
	ids := map[string]bool{}
	for v := range results {
		if ids[v.ID] {
			t.Fatalf("duplicate ID %s", v.ID)
		}
		ids[v.ID] = true
		sequences = append(sequences, int(v.Sequence))
	}
	sort.Ints(sequences)
	for i, got := range sequences {
		if got != i+1 {
			t.Fatalf("sequence[%d]=%d", i, got)
		}
	}
}

func TestMessageConcurrentSameKeyPersistsOneMutation(t *testing.T) {
	f := newMessageFixture(t, "same-message-key")
	const workers = 64
	start := make(chan struct{})
	results := make(chan message.Message, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	request := struct{ SessionID, Text string }{f.sessionID, "same"}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			v, e := f.app.Append(context.Background(), "same-key", "tester", request, message.Message{SessionID: f.sessionID, Text: "same"})
			results <- v
			errs <- e
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	var id string
	for v := range results {
		if id == "" {
			id = v.ID
		}
		if v.ID != id || v.Sequence != 1 {
			t.Fatalf("non-identical replay %#v", v)
		}
	}
	for table, want := range map[string]int{"messages": 1, "message_parts": 1, "audit_events": 3, "idempotency_records": 3} { // project + session + message
		if got := tableCount(t, f.store, table); got != want {
			t.Fatalf("%s=%d want=%d", table, got, want)
		}
	}
}

func TestMessageQuotaFailureRollsBackMessagePartAuditIdempotencyAndSequence(t *testing.T) {
	for _, name := range []string{"project boundary", "workspace boundary"} {
		t.Run(name, func(t *testing.T) {
			f := newMessageFixture(t, "quota-"+strings.ReplaceAll(name, " ", "-"))
			if _, err := f.store.db.Exec(`PRAGMA ignore_check_constraints=ON`); err != nil {
				t.Fatal(err)
			}
			seed := func(sessionID string, sequence int64, bytes int64) {
				t.Helper()
				seedID := ulid.Make().String()
				if _, err := f.store.db.Exec(`INSERT INTO messages(id,session_id,role,status,sequence,created_at) VALUES(?,?,'user','completed',?,'2025-01-01T00:00:00Z')`, seedID, sessionID, sequence); err != nil {
					t.Fatal(err)
				}
				if _, err := f.store.db.Exec(`INSERT INTO message_parts(message_id,ordinal,type,text) VALUES(?,1,'text',CAST(zeroblob(?) AS TEXT))`, seedID, bytes); err != nil {
					t.Fatal(err)
				}
				if _, err := f.store.db.Exec(`UPDATE message_session_state SET last_sequence=?,message_count=?,text_bytes=text_bytes+? WHERE session_id=?`, sequence, sequence, bytes, sessionID); err != nil {
					t.Fatal(err)
				}
				if _, err := f.store.db.Exec(`UPDATE message_project_usage SET text_bytes=text_bytes+? WHERE project_id=(SELECT project_id FROM sessions WHERE id=?)`, bytes, sessionID); err != nil {
					t.Fatal(err)
				}
				if _, err := f.store.db.Exec(`UPDATE message_workspace_usage SET text_bytes=text_bytes+? WHERE singleton=1`, bytes); err != nil {
					t.Fatal(err)
				}
			}
			if name == "project boundary" {
				seed(f.sessionID, 1, message.ProjectTextQuotaBytes)
			} else {
				// Four other projects each remain at their project quota while their
				// aggregate exactly reaches the workspace quota. The target project
				// is empty, so only the workspace boundary can reject its append.
				for i := 0; i < 4; i++ {
					projectID := createSessionProject(t, f.store, fmt.Sprintf("workspace-project-%d", i), fmt.Sprintf("Workspace %d", i))
					created, err := sessionapp.New(f.store, f.store).Create(context.Background(), fmt.Sprintf("workspace-session-%d", i), "tester", struct{ ProjectID, Title string }{projectID, "Quota"}, session.Session{ProjectID: projectID, Title: "Quota"})
					if err != nil {
						t.Fatal(err)
					}
					seed(created.ID, 1, message.ProjectTextQuotaBytes)
				}
			}
			if _, err := f.store.db.Exec(`PRAGMA ignore_check_constraints=OFF`); err != nil {
				t.Fatal(err)
			}
			before := map[string]int{}
			for _, table := range []string{"messages", "message_parts", "audit_events", "idempotency_records"} {
				before[table] = tableCount(t, f.store, table)
			}
			var beforeSequence int64
			if err := f.store.db.QueryRow(`SELECT COALESCE(max(sequence),0) FROM messages WHERE session_id=?`, f.sessionID).Scan(&beforeSequence); err != nil {
				t.Fatal(err)
			}
			var beforeSessionBytes, beforeProjectBytes, beforeWorkspaceBytes int64
			if err := f.store.db.QueryRow(`SELECT text_bytes FROM message_session_state WHERE session_id=?`, f.sessionID).Scan(&beforeSessionBytes); err != nil {
				t.Fatal(err)
			}
			if err := f.store.db.QueryRow(`SELECT text_bytes FROM message_project_usage WHERE project_id=?`, f.projectID).Scan(&beforeProjectBytes); err != nil {
				t.Fatal(err)
			}
			if err := f.store.db.QueryRow(`SELECT text_bytes FROM message_workspace_usage WHERE singleton=1`).Scan(&beforeWorkspaceBytes); err != nil {
				t.Fatal(err)
			}
			_, err := f.app.Append(context.Background(), "quota-failure", "tester", struct{ Text string }{"x"}, message.Message{SessionID: f.sessionID, Text: "x"})
			if !errors.Is(err, messageapp.ErrMessageStorageQuotaReached) {
				t.Fatalf("got %v", err)
			}
			for table, want := range before {
				if got := tableCount(t, f.store, table); got != want {
					t.Fatalf("%s retained mutation: %d want %d", table, got, want)
				}
			}
			var afterSequence int64
			if err = f.store.db.QueryRow(`SELECT COALESCE(max(sequence),0) FROM messages WHERE session_id=?`, f.sessionID).Scan(&afterSequence); err != nil {
				t.Fatal(err)
			}
			if afterSequence != beforeSequence {
				t.Fatalf("sequence changed from %d to %d", beforeSequence, afterSequence)
			}
			var afterSessionBytes, afterProjectBytes, afterWorkspaceBytes int64
			if err = f.store.db.QueryRow(`SELECT text_bytes FROM message_session_state WHERE session_id=?`, f.sessionID).Scan(&afterSessionBytes); err != nil {
				t.Fatal(err)
			}
			if err = f.store.db.QueryRow(`SELECT text_bytes FROM message_project_usage WHERE project_id=?`, f.projectID).Scan(&afterProjectBytes); err != nil {
				t.Fatal(err)
			}
			if err = f.store.db.QueryRow(`SELECT text_bytes FROM message_workspace_usage WHERE singleton=1`).Scan(&afterWorkspaceBytes); err != nil {
				t.Fatal(err)
			}
			if afterSessionBytes != beforeSessionBytes || afterProjectBytes != beforeProjectBytes || afterWorkspaceBytes != beforeWorkspaceBytes {
				t.Fatalf("counter mutation: session %d/%d project %d/%d workspace %d/%d", beforeSessionBytes, afterSessionBytes, beforeProjectBytes, afterProjectBytes, beforeWorkspaceBytes, afterWorkspaceBytes)
			}
		})
	}
}

func TestMessagePaginationOldCursorFailsWhenRemainingTailIsDeleted(t *testing.T) {
	for _, direction := range []messageapp.Direction{messageapp.Forward, messageapp.Backward} {
		t.Run(string(direction), func(t *testing.T) {
			f := newMessageFixture(t, "deleted-tail-"+string(direction))
			appendN(t, f, 1, 5, func(i int) string { return fmt.Sprint(i) })
			first, err := f.app.List(context.Background(), messageapp.PageRequest{SessionID: f.sessionID, Direction: direction, Limit: 2, ByteBudget: messageapp.MaxByteBudget})
			if err != nil || first.NextCursor == nil {
				t.Fatalf("first=%#v err=%v", first, err)
			}
			op, boundary := ">", first.Items[len(first.Items)-1].Sequence
			if direction == messageapp.Backward {
				op = "<"
			}
			if _, err = f.store.db.Exec(`DELETE FROM messages WHERE session_id=? AND sequence `+op+` ?`, f.sessionID, boundary); err != nil {
				t.Fatal(err)
			}
			_, err = f.app.List(context.Background(), messageapp.PageRequest{SessionID: f.sessionID, Direction: direction, Cursor: *first.NextCursor, Limit: 2, ByteBudget: messageapp.MaxByteBudget})
			if !errors.Is(err, messageapp.ErrDataInvariantViolation) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestMessageDeletedTailMakesInitialListAndAppendFailClosedWithoutSequenceReuse(t *testing.T) {
	f := newMessageFixture(t, "deleted-last")
	appendN(t, f, 1, 3, func(i int) string { return fmt.Sprint(i) })
	if _, err := f.store.db.Exec(`DELETE FROM messages WHERE session_id=? AND sequence=3`, f.sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.List(context.Background(), messageapp.PageRequest{SessionID: f.sessionID, Direction: messageapp.Forward, Limit: 10, ByteBudget: messageapp.MaxByteBudget}); !errors.Is(err, messageapp.ErrDataInvariantViolation) {
		t.Fatalf("initial list got %v", err)
	}
	if _, err := f.app.Append(context.Background(), "after-tail-delete", "tester", map[string]string{"text": "new"}, message.Message{SessionID: f.sessionID, Text: "new"}); !errors.Is(err, messageapp.ErrDataInvariantViolation) {
		t.Fatalf("append got %v", err)
	}
	var maxSequence, stateSequence int64
	if err := f.store.db.QueryRow(`SELECT COALESCE(max(sequence),0) FROM messages WHERE session_id=?`, f.sessionID).Scan(&maxSequence); err != nil {
		t.Fatal(err)
	}
	if err := f.store.db.QueryRow(`SELECT last_sequence FROM message_session_state WHERE session_id=?`, f.sessionID).Scan(&stateSequence); err != nil {
		t.Fatal(err)
	}
	if maxSequence != 2 || stateSequence != 3 {
		t.Fatalf("sequence reused or state changed: max=%d state=%d", maxSequence, stateSequence)
	}
}

func TestMessageCounterRowsMissingFailClosedOnReopen(t *testing.T) {
	for _, tc := range []struct{ name, statement string }{
		{"project usage", `DELETE FROM message_project_usage`},
		{"session state", `DELETE FROM message_session_state`},
		{"workspace singleton", `DELETE FROM message_workspace_usage`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "missing-counter.db")
			s, err := Open(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			projectID := createSessionProject(t, s, "counter-project", "Counter")
			if _, err = sessionapp.New(s, s).Create(context.Background(), "counter-session", "tester", struct{ ProjectID, Title string }{projectID, "Counter"}, session.Session{ProjectID: projectID, Title: "Counter"}); err != nil {
				t.Fatal(err)
			}
			if _, err = s.db.Exec(tc.statement); err != nil {
				t.Fatal(err)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			if reopened, openErr := Open(context.Background(), path); openErr == nil {
				reopened.Close()
				t.Fatal("missing counter row accepted")
			}
		})
	}
}

func TestMessageAppendFailsInvariantWhenUsageCounterDisappearsAtRuntime(t *testing.T) {
	for _, tc := range []struct {
		name      string
		statement string
	}{
		{"project usage", `DELETE FROM message_project_usage`},
		{"workspace singleton", `DELETE FROM message_workspace_usage`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMessageFixture(t, strings.ReplaceAll(tc.name, " ", "-"))
			if _, err := f.store.db.Exec(tc.statement); err != nil {
				t.Fatal(err)
			}
			_, err := f.app.Append(context.Background(), "missing-usage-counter", "tester", map[string]string{"text": "x"}, message.Message{SessionID: f.sessionID, Text: "x"})
			if !errors.Is(err, messageapp.ErrDataInvariantViolation) {
				t.Fatalf("got %v, want ErrDataInvariantViolation", err)
			}
			if got := tableCount(t, f.store, "messages"); got != 0 {
				t.Fatalf("messages=%d want=0", got)
			}
		})
	}
}

func TestMessageAppendFailsInvariantWhenUsageCounterAlreadyExceedsQuota(t *testing.T) {
	for _, tc := range []struct {
		name      string
		statement string
		quota     int64
	}{
		{"project", `UPDATE message_project_usage SET text_bytes=? WHERE project_id=?`, message.ProjectTextQuotaBytes},
		{"workspace", `UPDATE message_workspace_usage SET text_bytes=? WHERE singleton=1`, message.WorkspaceTextQuotaBytes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMessageFixture(t, "over-quota-"+tc.name)
			if _, err := f.store.db.Exec(`PRAGMA ignore_check_constraints=ON`); err != nil {
				t.Fatal(err)
			}
			args := []any{tc.quota + 1}
			if tc.name == "project" {
				args = append(args, f.projectID)
			}
			if _, err := f.store.db.Exec(tc.statement, args...); err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.db.Exec(`PRAGMA ignore_check_constraints=OFF`); err != nil {
				t.Fatal(err)
			}

			before := map[string]int{}
			for _, table := range []string{"messages", "message_parts", "audit_events", "idempotency_records"} {
				before[table] = tableCount(t, f.store, table)
			}
			var beforeSession, beforeProject, beforeWorkspace int64
			if err := f.store.db.QueryRow(`SELECT text_bytes FROM message_session_state WHERE session_id=?`, f.sessionID).Scan(&beforeSession); err != nil {
				t.Fatal(err)
			}
			if err := f.store.db.QueryRow(`SELECT text_bytes FROM message_project_usage WHERE project_id=?`, f.projectID).Scan(&beforeProject); err != nil {
				t.Fatal(err)
			}
			if err := f.store.db.QueryRow(`SELECT text_bytes FROM message_workspace_usage WHERE singleton=1`).Scan(&beforeWorkspace); err != nil {
				t.Fatal(err)
			}

			_, err := f.app.Append(context.Background(), "over-quota", "tester", map[string]string{"text": "x"}, message.Message{SessionID: f.sessionID, Text: "x"})
			if !errors.Is(err, messageapp.ErrDataInvariantViolation) {
				t.Fatalf("got %v, want ErrDataInvariantViolation", err)
			}
			for table, want := range before {
				if got := tableCount(t, f.store, table); got != want {
					t.Fatalf("%s=%d want=%d", table, got, want)
				}
			}
			var afterSession, afterProject, afterWorkspace int64
			if err = f.store.db.QueryRow(`SELECT text_bytes FROM message_session_state WHERE session_id=?`, f.sessionID).Scan(&afterSession); err != nil {
				t.Fatal(err)
			}
			if err = f.store.db.QueryRow(`SELECT text_bytes FROM message_project_usage WHERE project_id=?`, f.projectID).Scan(&afterProject); err != nil {
				t.Fatal(err)
			}
			if err = f.store.db.QueryRow(`SELECT text_bytes FROM message_workspace_usage WHERE singleton=1`).Scan(&afterWorkspace); err != nil {
				t.Fatal(err)
			}
			if afterSession != beforeSession || afterProject != beforeProject || afterWorkspace != beforeWorkspace {
				t.Fatalf("counter mutation: session %d/%d project %d/%d workspace %d/%d", beforeSession, afterSession, beforeProject, afterProject, beforeWorkspace, afterWorkspace)
			}
		})
	}
}

func TestMessageOpenFailsClosedWhenConsistentProjectUsageExceedsQuota(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "consistent-over-quota.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	projectID := createSessionProject(t, s, "consistent-over-quota-project", "Over Quota")
	created, err := sessionapp.New(s, s).Create(ctx, "consistent-over-quota-session", "tester", struct{ ProjectID, Title string }{projectID, "Over Quota"}, session.Session{ProjectID: projectID, Title: "Over Quota"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`PRAGMA ignore_check_constraints=ON`); err != nil {
		t.Fatal(err)
	}
	used := message.ProjectTextQuotaBytes + 8192
	text := strings.Repeat("😀", message.MaxRunes)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for sequence := int64(1); sequence <= 8193; sequence++ {
		messageID := ulid.Make().String()
		if _, err = tx.Exec(`INSERT INTO messages(id,session_id,role,status,sequence,created_at) VALUES(?,?,'user','completed',?,'2025-01-01T00:00:00Z')`, messageID, created.ID, sequence); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`INSERT INTO message_parts(message_id,ordinal,type,text) VALUES(?,1,'text',?)`, messageID, text); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec(`UPDATE message_session_state SET last_sequence=8193,message_count=8193,text_bytes=? WHERE session_id=?;
		UPDATE message_project_usage SET text_bytes=? WHERE project_id=?;
		UPDATE message_workspace_usage SET text_bytes=? WHERE singleton=1`, used, created.ID, used, projectID, used); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = validateDataInvariants(ctx, s.db); err == nil || !strings.Contains(err.Error(), "message project usage invariant violation") {
		t.Fatalf("startup invariant logic accepted consistent over-quota usage: %v", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, openErr := Open(ctx, path); openErr == nil {
		reopened.Close()
		t.Fatal("consistent over-quota usage accepted on reopen")
	}
}
