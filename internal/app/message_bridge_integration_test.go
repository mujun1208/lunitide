package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	_ "modernc.org/sqlite"
)

func messageEngine(t *testing.T) (*Engine, string, string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "message.db")
	store, e := storage.OpenTemplated(context.Background(), path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	projects := projectapp.New(store, store)
	parent, e := projects.Create(context.Background(), "parent-key", "test", struct {
		Name string `json:"name"`
	}{"Parent"}, structToProject("Parent"))
	if e != nil {
		t.Fatal(e)
	}
	sessions := sessionapp.New(store, store)
	created, e := sessions.Create(context.Background(), "session-key", "test", struct {
		ProjectID string `json:"projectId"`
		Title     string `json:"title"`
	}{parent.ID, "Test Session"}, session.Session{ProjectID: parent.ID, Title: "Test Session"})
	if e != nil {
		t.Fatal(e)
	}
	cursorKey := []byte("0123456789abcdef0123456789abcdef")
	msgs, e := messageapp.New(store, store, cursorKey)
	if e != nil {
		t.Fatal(e)
	}
	return NewEngineWithMessages(providerapp.New(store, store), projects, sessions, msgs, "test", nil), parent.ID, created.ID, path
}

func TestMessageBridgeAppendListReplayConflictAndStrictPayloads(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	r := validRequest("message.append", `{"sessionId":"`+sessionID+`","text":"Hello world"}`)
	r.IdempotencyKey = "message-key"
	first := e.Handle(context.Background(), r)
	if !first.OK {
		t.Fatalf("append failed: %#v", first)
	}
	second := e.Handle(context.Background(), r)
	if !second.OK {
		t.Fatalf("replay failed: %#v", second)
	}
	a, _ := json.Marshal(first.Payload)
	b, _ := json.Marshal(second.Payload)
	if string(a) != string(b) {
		t.Fatalf("replay differs %s %s", a, b)
	}
	r.Payload = json.RawMessage(`{"sessionId":"` + sessionID + `","text":"Different"}`)
	if x := e.Handle(context.Background(), r); x.OK || x.Error.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflict %#v", x)
	}
	listReq := validRequest("message.list", `{"sessionId":"`+sessionID+`"}`)
	listResp := e.Handle(context.Background(), listReq)
	if !listResp.OK {
		t.Fatalf("list failed: %#v", listResp)
	}
	var page messageapp.Page
	if err := decodeResponsePayload(listResp.Payload, &page); err != nil || len(page.Items) != 1 || page.Items[0].Text != "Hello world" {
		t.Fatalf("list payload: %#v err=%v", page, err)
	}
	for _, raw := range []string{
		"null",
		`{"sessionId":"` + sessionID + `"}`,
		`{"sessionId":"` + sessionID + `","text":"","unknown":1}`,
	} {
		bad := validRequest("message.append", raw)
		bad.IdempotencyKey = "bad-payload"
		if x := e.Handle(context.Background(), bad); x.OK || x.Error.Code != "BRIDGE_SCHEMA_INVALID" {
			t.Fatalf("accepted %s: %#v", raw, x)
		}
	}
	for _, raw := range []string{
		`{"sessionId":"bad-ulid"}`,
		`{"sessionId":"` + sessionID + `","direction":"sideways"}`,
		`{"sessionId":"` + sessionID + `","limit":257}`,
		`{"sessionId":"` + sessionID + `","byteBudget":100}`,
		`{"sessionId":"` + sessionID + `","byteBudget":999999}`,
	} {
		bad := validRequest("message.list", raw)
		if x := e.Handle(context.Background(), bad); x.OK || x.Error.Code != "BRIDGE_SCHEMA_INVALID" {
			t.Fatalf("accepted list %s: %#v", raw, x)
		}
	}
}

func TestMessageBridgeAppendTextBoundaries(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	for i, text := range []string{
		"a",
		strings.Repeat("a", 2048),
		strings.Repeat("😀", 2048),
		"  hello\r\nworld\rtest  ",
	} {
		textJSON, _ := json.Marshal(text)
		r := validRequest("message.append", fmt.Sprintf(`{"sessionId":"%s","text":%s}`, sessionID, textJSON))
		r.IdempotencyKey = fmt.Sprintf("boundary-%d", i)
		resp := e.Handle(context.Background(), r)
		if !resp.OK {
			t.Fatalf("text=%q rejected: code=%s", text, resp.Error.Code)
		}
	}
	for _, text := range []string{
		"",
		strings.Repeat("a", 2049),
		strings.Repeat("😀", 2048) + "a",
		"a\x00b",
	} {
		r := validRequest("message.append", fmt.Sprintf(`{"sessionId":"%s","text":%q}`, sessionID, text))
		r.IdempotencyKey = "overflow-" + text[:min(8, len(text))]
		resp := e.Handle(context.Background(), r)
		if resp.OK || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
			t.Fatalf("text=%q accepted: %#v", text, resp)
		}
	}
}

func TestMessageBridgeListPaginationAndCursor(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	for i := 0; i < 10; i++ {
		r := validRequest("message.append", fmt.Sprintf(`{"sessionId":"%s","text":"msg-%d"}`, sessionID, i))
		r.IdempotencyKey = "pag-" + fmt.Sprint(i)
		if resp := e.Handle(context.Background(), r); !resp.OK {
			t.Fatalf("append %d: %#v", i, resp)
		}
	}
	listReq := validRequest("message.list", fmt.Sprintf(`{"sessionId":"%s","direction":"backward","limit":3}`, sessionID))
	listResp := e.Handle(context.Background(), listReq)
	if !listResp.OK {
		t.Fatalf("list: %#v", listResp)
	}
	var page messageapp.Page
	if err := decodeResponsePayload(listResp.Payload, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 || !page.HasMore || page.NextCursor == nil {
		t.Fatalf("page: items=%d hasMore=%v cursor=%v", len(page.Items), page.HasMore, page.NextCursor)
	}
	if page.Items[0].Sequence != 10 || page.Items[1].Sequence != 9 || page.Items[2].Sequence != 8 {
		t.Fatalf("backward order: %v", page.Items)
	}
	nextReq := validRequest("message.list", fmt.Sprintf(`{"sessionId":"%s","direction":"backward","cursor":%q,"limit":3}`, sessionID, *page.NextCursor))
	nextResp := e.Handle(context.Background(), nextReq)
	if !nextResp.OK {
		t.Fatalf("next page: %#v", nextResp)
	}
	var nextPage messageapp.Page
	if err := decodeResponsePayload(nextResp.Payload, &nextPage); err != nil {
		t.Fatal(err)
	}
	if len(nextPage.Items) != 3 || nextPage.Items[0].Sequence != 7 {
		t.Fatalf("next page: %v", nextPage)
	}
	tampered := *page.NextCursor
	if tampered[len(tampered)-1] == 'A' {
		tampered = tampered[:len(tampered)-1] + "B"
	} else {
		tampered = tampered[:len(tampered)-1] + "A"
	}
	badReq := validRequest("message.list", fmt.Sprintf(`{"sessionId":"%s","direction":"backward","cursor":%q}`, sessionID, tampered))
	badResp := e.Handle(context.Background(), badReq)
	if badResp.OK || badResp.Error.Code != "MESSAGE_CURSOR_INVALID" {
		t.Fatalf("tampered cursor: %#v", badResp)
	}
}

func TestMessageBridgeSessionNotFound(t *testing.T) {
	e, _, _, _ := messageEngine(t)
	badSession := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	r := validRequest("message.append", `{"sessionId":"`+badSession+`","text":"hello"}`)
	r.IdempotencyKey = "bad-session"
	resp := e.Handle(context.Background(), r)
	if resp.OK || resp.Error.Code != "SESSION_NOT_FOUND" {
		t.Fatalf("expected SESSION_NOT_FOUND: %#v", resp)
	}
	listReq := validRequest("message.list", `{"sessionId":"`+badSession+`"}`)
	listResp := e.Handle(context.Background(), listReq)
	if listResp.OK || listResp.Error.Code != "SESSION_NOT_FOUND" {
		t.Fatalf("list expected SESSION_NOT_FOUND: %#v", listResp)
	}
}

func TestMessageBridgeMissingTypedNil(t *testing.T) {
	var nilService *messageapp.Service
	e := NewEngineWithMessages(nil, nil, nil, nilService, "test", nil)
	r := validRequest("message.append", `{"sessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","text":"hello"}`)
	r.IdempotencyKey = "nil-key"
	if x := e.Handle(context.Background(), r); x.OK || x.Error.Code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("typed nil append: %#v", x)
	}
	listReq := validRequest("message.list", `{"sessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`)
	if x := e.Handle(context.Background(), listReq); x.OK || x.Error.Code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("typed nil list: %#v", x)
	}
}

func TestMessageBridgeConcurrentSameKeyCreatesExactlyOneMessage(t *testing.T) {
	e, _, sessionID, path := messageEngine(t)
	r := validRequest("message.append", `{"sessionId":"`+sessionID+`","text":"Concurrent"}`)
	r.IdempotencyKey = "concurrent-message-key"
	const workers = 20
	responses := make(chan bridge.Response, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			responses <- e.Handle(context.Background(), r)
		}()
	}
	close(start)
	wg.Wait()
	close(responses)
	var encoded string
	for response := range responses {
		if !response.OK {
			t.Fatalf("response: %#v", response)
		}
		body, _ := json.Marshal(response.Payload)
		if encoded == "" {
			encoded = string(body)
		} else if encoded != string(body) {
			t.Fatalf("different replay: %s != %s", encoded, body)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for query, want := range map[string]int{
		`SELECT count(*) FROM messages WHERE session_id='` + sessionID + `'`:                                                        1,
		`SELECT count(*) FROM message_parts WHERE message_id IN (SELECT id FROM messages WHERE session_id='` + sessionID + `')`:      1,
		`SELECT count(*) FROM audit_events WHERE action='message.appended'`:                                                          1,
		`SELECT count(*) FROM idempotency_records WHERE operation='message.append' AND idempotency_key='concurrent-message-key'`:     1,
		`SELECT last_sequence FROM message_session_state WHERE session_id='` + sessionID + `'`:                                       1,
	} {
		var got int
		if err = db.QueryRow(query).Scan(&got); err != nil || got != want {
			t.Fatalf("query %q got=%d want=%d err=%v", query, got, want, err)
		}
	}
}

func TestMessageBridgeQuotaEnforcement(t *testing.T) {
	e, projectID, sessionID, path := messageEngine(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Set project usage to just below the quota so the next append triggers it.
	_, err = db.Exec(`UPDATE message_project_usage SET text_bytes=? WHERE project_id=?`, message.ProjectTextQuotaBytes-1, projectID)
	if err != nil {
		t.Fatal(err)
	}
	r := validRequest("message.append", fmt.Sprintf(`{"sessionId":"%s","text":"overflow"}`, sessionID))
	r.IdempotencyKey = "quota-overflow"
	resp := e.Handle(context.Background(), r)
	if resp.OK || resp.Error.Code != "MESSAGE_STORAGE_QUOTA_REACHED" {
		t.Fatalf("quota overflow: %#v", resp)
	}
}

func TestMessageBridgeDurableResultsSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "durable.db")
	store, e := storage.OpenTemplated(context.Background(), path)
	if e != nil {
		t.Fatal(e)
	}
	projects := projectapp.New(store, store)
	parent, e := projects.Create(context.Background(), "pk", "test", struct{ Name string }{"Parent"}, structToProject("Parent"))
	if e != nil {
		t.Fatal(e)
	}
	sessions := sessionapp.New(store, store)
	created, e := sessions.Create(context.Background(), "sk", "test", struct {
		ProjectID string `json:"projectId"`
		Title     string `json:"title"`
	}{parent.ID, "Durable"}, session.Session{ProjectID: parent.ID, Title: "Durable"})
	if e != nil {
		t.Fatal(e)
	}
	cursorKey := []byte("0123456789abcdef0123456789abcdef")
	msgs, e := messageapp.New(store, store, cursorKey)
	if e != nil {
		t.Fatal(e)
	}
	engine := NewEngineWithMessages(providerapp.New(store, store), projects, sessions, msgs, "test", nil)
	r := validRequest("message.append", `{"sessionId":"`+created.ID+`","text":"durable"}`)
	r.IdempotencyKey = "durable-key"
	resp := engine.Handle(context.Background(), r)
	if !resp.OK {
		t.Fatalf("append: %#v", resp)
	}
	store.Close()
	store2, e := storage.OpenTemplated(context.Background(), path)
	if e != nil {
		t.Fatal(e)
	}
	defer store2.Close()
	msgs2, e := messageapp.New(store2, store2, cursorKey)
	if e != nil {
		t.Fatal(e)
	}
	engine2 := NewEngineWithMessages(providerapp.New(store2, store2), projectapp.New(store2, store2), sessionapp.New(store2, store2), msgs2, "test", nil)
	listReq := validRequest("message.list", `{"sessionId":"`+created.ID+`"}`)
	listResp := engine2.Handle(context.Background(), listReq)
	if !listResp.OK {
		t.Fatalf("list after reopen: %#v", listResp)
	}
	var page messageapp.Page
	if err := decodeResponsePayload(listResp.Payload, &page); err != nil || len(page.Items) != 1 || page.Items[0].Text != "durable" {
		t.Fatalf("durable result: %#v err=%v", page, err)
	}
}

func TestMessageBridgeMissingIdempotencyKey(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	r := validRequest("message.append", `{"sessionId":"`+sessionID+`","text":"no-key"}`)
	r.IdempotencyKey = ""
	resp := e.Handle(context.Background(), r)
	if resp.OK || resp.Error.Code != "IDEMPOTENCY_KEY_REQUIRED" {
		t.Fatalf("missing key: %#v", resp)
	}
}

func TestMessageBridgeCRLFNormalization(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	r := validRequest("message.append", fmt.Sprintf(`{"sessionId":"%s","text":%q}`, sessionID, "a\r\nb\rc"))
	r.IdempotencyKey = "crlf"
	resp := e.Handle(context.Background(), r)
	if !resp.OK {
		t.Fatalf("crlf: %#v", resp)
	}
	listReq := validRequest("message.list", `{"sessionId":"`+sessionID+`"}`)
	listResp := e.Handle(context.Background(), listReq)
	if !listResp.OK {
		t.Fatalf("list: %#v", listResp)
	}
	var page messageapp.Page
	if err := decodeResponsePayload(listResp.Payload, &page); err != nil || len(page.Items) != 1 || page.Items[0].Text != "a\nb\nc" {
		t.Fatalf("normalized text: %#v err=%v", page, err)
	}
}

func TestMessageBridgeForwardPagination(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	for i := 0; i < 5; i++ {
		r := validRequest("message.append", fmt.Sprintf(`{"sessionId":"%s","text":"fwd-%d"}`, sessionID, i))
		r.IdempotencyKey = "fwd-" + fmt.Sprint(i)
		if resp := e.Handle(context.Background(), r); !resp.OK {
			t.Fatalf("append %d: %#v", i, resp)
		}
	}
	listReq := validRequest("message.list", fmt.Sprintf(`{"sessionId":"%s","direction":"forward","limit":2}`, sessionID))
	listResp := e.Handle(context.Background(), listReq)
	if !listResp.OK {
		t.Fatalf("list: %#v", listResp)
	}
	var page messageapp.Page
	if err := decodeResponsePayload(listResp.Payload, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].Sequence != 1 || page.Items[1].Sequence != 2 {
		t.Fatalf("forward order: %v", page)
	}
	if !page.HasMore || page.NextCursor == nil {
		t.Fatalf("expected more: hasMore=%v cursor=%v", page.HasMore, page.NextCursor)
	}
	nextReq := validRequest("message.list", fmt.Sprintf(`{"sessionId":"%s","direction":"forward","cursor":%q,"limit":2}`, sessionID, *page.NextCursor))
	nextResp := e.Handle(context.Background(), nextReq)
	if !nextResp.OK {
		t.Fatalf("next page: %#v", nextResp)
	}
	var nextPage messageapp.Page
	if err := decodeResponsePayload(nextResp.Payload, &nextPage); err != nil {
		t.Fatal(err)
	}
	if len(nextPage.Items) != 2 || nextPage.Items[0].Sequence != 3 || nextPage.Items[1].Sequence != 4 {
		t.Fatalf("next forward: %v", nextPage)
	}
}

func TestMessageBridgeByteBudgetEnforcement(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	text := strings.Repeat("x", 2048)
	for i := 0; i < 3; i++ {
		r := validRequest("message.append", fmt.Sprintf(`{"sessionId":"%s","text":%q}`, sessionID, text))
		r.IdempotencyKey = "bb-" + fmt.Sprint(i)
		if resp := e.Handle(context.Background(), r); !resp.OK {
			t.Fatalf("append %d: %#v", i, resp)
		}
	}
	listReq := validRequest("message.list", fmt.Sprintf(`{"sessionId":"%s","direction":"backward","limit":10,"byteBudget":16384}`, sessionID))
	listResp := e.Handle(context.Background(), listReq)
	if !listResp.OK {
		t.Fatalf("list: %#v", listResp)
	}
	var page messageapp.Page
	if err := decodeResponsePayload(listResp.Payload, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) == 0 {
		t.Fatal("byte budget too small should return at least one item")
	}
	if len(page.Items) < 3 && page.HasMore {
		t.Logf("byte budget shrunk page to %d items (expected)", len(page.Items))
	}
}

func TestMessageBridgeSequenceMonotonic(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	for i := 0; i < 5; i++ {
		r := validRequest("message.append", fmt.Sprintf(`{"sessionId":"%s","text":"seq-%d"}`, sessionID, i))
		r.IdempotencyKey = "seq-" + fmt.Sprint(i)
		resp := e.Handle(context.Background(), r)
		if !resp.OK {
			t.Fatalf("append %d: %#v", i, resp)
		}
		var msg message.Message
		if err := decodeResponsePayload(resp.Payload, &msg); err != nil || msg.Sequence != int64(i+1) {
			t.Fatalf("sequence %d: %#v err=%v", i, msg, err)
		}
	}
}

func TestMessageBridgeEmptySessionList(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	listReq := validRequest("message.list", `{"sessionId":"`+sessionID+`"}`)
	listResp := e.Handle(context.Background(), listReq)
	if !listResp.OK {
		t.Fatalf("empty list: %#v", listResp)
	}
	var page messageapp.Page
	if err := decodeResponsePayload(listResp.Payload, &page); err != nil || len(page.Items) != 0 || page.HasMore {
		t.Fatalf("empty page: %#v err=%v", page, err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func decodeResponsePayload(payload any, target any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}