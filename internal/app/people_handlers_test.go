package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/identity"
	"github.com/lunitide/lunitide/internal/people"
	sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func newPeopleEngine(t *testing.T) (*Engine, *identity.Service, *people.Service) {
	t.Helper()
	store, err := sqlitestore.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "people.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ident := identity.New(store)
	if err := ident.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	roster := people.New(store, ident, filepath.Join(t.TempDir(), "recv"), filepath.Join(t.TempDir(), "stage"))
	roster.SetListenAddr("127.0.0.1:0")
	t.Cleanup(roster.Close)
	e := NewEngine(nil, "test")
	e.SetIdentityPeopleServices(ident, roster)
	return e, ident, roster
}

func peopleCall(t *testing.T, e *Engine, method string, payload any) bridge.Response {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	r := bridge.Request{ID: "01AAAAAAAAAAAAAAAAAAAAAAAA", TraceID: "01BBBBBBBBBBBBBBBBBBBBBBB", Method: method, Payload: raw}
	handler, ok := RuntimeHandlers[bridge.Method(method)]
	if !ok || handler == nil {
		t.Fatalf("method %s missing from RuntimeHandlers", method)
	}
	return handler(e, context.Background(), r)
}

func peopleOK[Out any](t *testing.T, e *Engine, method string, payload any) Out {
	t.Helper()
	resp := peopleCall(t, e, method, payload)
	if !resp.OK {
		t.Fatalf("%s failed: %+v", method, resp.Error)
	}
	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var out Out
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v raw=%s", method, err, raw)
	}
	return out
}

func TestIdentityGetAndUpdate(t *testing.T) {
	e, _, _ := newPeopleEngine(t)
	got := peopleOK[identity.Public](t, e, "identity.get", map[string]any{})
	if got.Nickname != identity.DefaultNickname || got.Locked || !got.DiscoveryEnabled && got.PairingCode == "" {
		t.Fatalf("identity.get = %+v", got)
	}
	if got.DiscoveryEnabled {
		t.Fatal("LAN discovery must default off")
	}
	updated := peopleOK[identity.Public](t, e, "identity.update", map[string]any{
		"nickname": "mu", "status": "busy", "department": "研发", "title": "工程师", "orgName": "月汐", "bio": "本机",
	})
	if updated.Nickname != "mu" || updated.Status != identity.StatusBusy || updated.Department != "研发" {
		t.Fatalf("identity.update = %+v", updated)
	}
}

func TestIdentityPasswordLocksWrites(t *testing.T) {
	e, ident, _ := newPeopleEngine(t)
	peopleOK[identity.Public](t, e, "identity.password.set", map[string]any{"password": "tide-lock"})
	if err := ident.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Relock by constructing a fresh service on the same store is not needed:
	// SetPassword leaves unlocked. Unlock after a new engine on the same row.
	store, err := sqlitestore.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "lock.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	lockedIdent := identity.New(store)
	if err := lockedIdent.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := lockedIdent.SetPassword(context.Background(), "tide-lock", ""); err != nil {
		t.Fatal(err)
	}
	lockedIdent = identity.New(store)
	if err := lockedIdent.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	roster := people.New(store, lockedIdent, t.TempDir(), t.TempDir())
	roster.SetListenAddr("127.0.0.1:0")
	t.Cleanup(roster.Close)
	locked := NewEngine(nil, "test")
	locked.SetIdentityPeopleServices(lockedIdent, roster)
	resp := peopleCall(t, locked, "identity.update", map[string]any{"nickname": "nope"})
	if resp.OK || resp.Error == nil || resp.Error.Code != "IDENTITY_LOCKED" {
		t.Fatalf("locked update = %+v", resp)
	}
	peopleOK[identity.Public](t, locked, "identity.unlock", map[string]any{"password": "tide-lock"})
	peopleOK[identity.Public](t, locked, "identity.update", map[string]any{"nickname": "ok"})
}

func TestPeopleRosterDirectGroupAndFileConfirm(t *testing.T) {
	e, ident, roster := newPeopleEngine(t)
	self := ident.Public()
	if err := roster.IngestBeacon(context.Background(), people.Beacon{
		V: 1, Kind: "lunitide-people", SubjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Nickname: "同事甲", Department: "研发", Title: "设计师", OrgName: "月汐", Status: "online",
		PublicKey:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PairingHash: identity.PairingHash("654321", "01ARZ3NDEKTSV4RRFFQ69G5FAV"),
	}, "192.168.1.8"); err != nil {
		t.Fatal(err)
	}
	listed := peopleOK[struct {
		Items []map[string]any `json:"items"`
	}](t, e, "people.list", map[string]any{})
	if len(listed.Items) != 2 {
		t.Fatalf("list = %#v", listed.Items)
	}
	peer := peopleOK[map[string]any](t, e, "people.pair", map[string]any{
		"pairingCode": "654321", "subjectId": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "nickname": "同事甲",
	})
	if peer["trustState"] != "trusted" {
		t.Fatalf("pair = %#v", peer)
	}
	opened := peopleOK[struct {
		Thread   map[string]any   `json:"thread"`
		Messages []map[string]any `json:"messages"`
	}](t, e, "people.thread.open", map[string]any{"peerSubjectId": "01ARZ3NDEKTSV4RRFFQ69G5FAV"})
	threadID, _ := opened.Thread["threadId"].(string)
	if threadID == "" || opened.Thread["kind"] != "direct" {
		t.Fatalf("open = %#v", opened.Thread)
	}
	sent := peopleOK[struct {
		Message map[string]any `json:"message"`
	}](t, e, "people.thread.send", map[string]any{"threadId": threadID, "kind": "text", "body": "你好"})
	if sent.Message["body"] != "你好" || sent.Message["senderSubjectId"] != self.SubjectID {
		t.Fatalf("send = %#v", sent.Message)
	}
	listedThreads := peopleOK[struct {
		Items []map[string]any `json:"items"`
	}](t, e, "people.thread.list", map[string]any{})
	if len(listedThreads.Items) == 0 {
		t.Fatal("thread list empty after send")
	}
	last, _ := listedThreads.Items[0]["lastMessage"].(map[string]any)
	if last == nil || last["body"] != "你好" {
		t.Fatalf("thread list lastMessage = %#v", listedThreads.Items[0])
	}
	if unread, _ := listedThreads.Items[0]["unreadCount"].(float64); unread != 0 {
		t.Fatalf("own send must not leave unread: %#v", listedThreads.Items[0])
	}
	file := peopleOK[struct {
		Message map[string]any `json:"message"`
		Offer   map[string]any `json:"offer"`
	}](t, e, "people.thread.send", map[string]any{
		"threadId": threadID, "kind": "file", "fileName": "note.txt", "fileMime": "text/plain",
		"contentBase64": base64.StdEncoding.EncodeToString([]byte("secret")),
	})
	if file.Offer["status"] != "pending" {
		t.Fatalf("file must stay pending until confirm: %#v", file.Offer)
	}
	rejected := peopleOK[map[string]any](t, e, "people.file.decide", map[string]any{
		"offerId": file.Offer["offerId"], "accept": false,
	})
	if rejected["status"] != "rejected" {
		t.Fatalf("decide reject = %#v", rejected)
	}
	group := peopleOK[map[string]any](t, e, "people.group.create", map[string]any{
		"title": "研发群", "ownerSubjectId": self.SubjectID,
		"memberSubjectIds": []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV"},
	})
	if group["kind"] != "group" || group["title"] != "研发群" {
		t.Fatalf("group = %#v", group)
	}
	disc := peopleOK[map[string]any](t, e, "people.discovery.get", map[string]any{})
	if disc["enabled"] != false {
		t.Fatalf("discovery default = %#v", disc)
	}
}

func TestPeopleHandlersUnavailableWithoutServices(t *testing.T) {
	e := NewEngine(nil, "test")
	resp := peopleCall(t, e, "people.list", map[string]any{})
	if resp.OK || resp.Error == nil || resp.Error.Code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("unwired list = %+v", resp)
	}
}

func TestPeopleGroupRejectsDiscoveredPeer(t *testing.T) {
	e, _, roster := newPeopleEngine(t)
	if err := roster.IngestBeacon(context.Background(), people.Beacon{
		V: 1, Kind: "lunitide-people", SubjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Nickname: "路人", Status: "online",
	}, "10.0.0.2"); err != nil {
		t.Fatal(err)
	}
	resp := peopleCall(t, e, "people.group.create", map[string]any{
		"title": "不可信群", "memberSubjectIds": []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV"},
	})
	if resp.OK || resp.Error == nil || resp.Error.Code != "PEOPLE_NOT_TRUSTED" {
		t.Fatalf("discovered group = %+v", resp)
	}
}

func TestPeopleContactUpdateAndFileStage(t *testing.T) {
	e, _, roster := newPeopleEngine(t)
	if err := roster.IngestBeacon(context.Background(), people.Beacon{
		V: 1, Kind: "lunitide-people", SubjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Nickname: "同事甲", Status: "online",
		PairingHash: identity.PairingHash("654321", "01ARZ3NDEKTSV4RRFFQ69G5FAV"),
	}, "10.0.0.8"); err != nil {
		t.Fatal(err)
	}
	updated := peopleOK[map[string]any](t, e, "people.contact.update", map[string]any{
		"subjectId": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "remark": "阿甲",
	})
	if updated["remark"] != "阿甲" {
		t.Fatalf("remark = %#v", updated)
	}
	blocked := peopleOK[map[string]any](t, e, "people.contact.update", map[string]any{
		"subjectId": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "blocked": true,
	})
	if blocked["blocked"] != true {
		t.Fatalf("blocked = %#v", blocked)
	}
	opened := peopleOK[struct {
		Thread map[string]any `json:"thread"`
	}](t, e, "people.thread.open", map[string]any{"peerSubjectId": "01ARZ3NDEKTSV4RRFFQ69G5FAV"})
	threadID, _ := opened.Thread["threadId"].(string)
	staged := peopleOK[map[string]any](t, e, "people.file.stage", map[string]any{
		"uploadId": "01ARZ3NDEKTSV4RRFFQ69G5FAW", "last": true, "fileName": "note.txt",
		"contentBase64": base64.StdEncoding.EncodeToString([]byte("hello-stage")),
	})
	path, _ := staged["localPath"].(string)
	if staged["ready"] != true || path == "" {
		t.Fatalf("stage = %#v", staged)
	}
	sent := peopleOK[struct {
		Message map[string]any `json:"message"`
		Offer   map[string]any `json:"offer"`
	}](t, e, "people.thread.send", map[string]any{
		"threadId": threadID, "kind": "file", "fileName": "note.txt", "localPath": path,
	})
	if sent.Offer["status"] != "pending" || sent.Message["fileSize"].(float64) != 11 {
		t.Fatalf("staged send = %#v %#v", sent.Message, sent.Offer)
	}
	resp := peopleCall(t, e, "people.peer.add", map[string]any{"hostAddr": "127.0.0.1:1"})
	if resp.OK || resp.Error == nil || resp.Error.Code != "PEOPLE_UNREACHABLE" {
		code := ""
		if resp.Error != nil {
			code = resp.Error.Code
		}
		t.Fatalf("unreachable add ok=%v code=%s resp=%+v", resp.OK, code, resp)
	}
}

func TestIdentityInvisibleStopsBeacon(t *testing.T) {
	e, ident, roster := newPeopleEngine(t)
	updated := peopleOK[identity.Public](t, e, "identity.update", map[string]any{"status": "invisible"})
	if updated.Status != identity.StatusInvisible {
		t.Fatalf("status = %+v", updated)
	}
	if _, ok := roster.CurrentBeacon(); ok {
		t.Fatal("identity.update invisible must stop broadcasting")
	}
	if ident.Public().Status != identity.StatusInvisible {
		t.Fatal("identity cache drifted")
	}
}

type peopleCaptureHost struct {
	png []byte
	err error
}

func (h peopleCaptureHost) Available() bool                   { return true }
func (h peopleCaptureHost) ScreenSize() (int, int)            { return 1, 1 }
func (h peopleCaptureHost) ScreenOrigin() (int, int)          { return 0, 0 }
func (h peopleCaptureHost) CursorPosition() (int, int, error) { return 0, 0, nil }
func (h peopleCaptureHost) MouseMove(int, int) error          { return nil }
func (h peopleCaptureHost) MouseClick(string, int) error      { return nil }
func (h peopleCaptureHost) MouseDrag(int, int, int, int) error {
	return nil
}
func (h peopleCaptureHost) KeyboardType(string) error       { return nil }
func (h peopleCaptureHost) KeyboardShortcut([]string) error { return nil }
func (h peopleCaptureHost) MouseScroll(int) error           { return nil }
func (h peopleCaptureHost) MouseScrollH(int) error          { return nil }
func (h peopleCaptureHost) EnsureForeground() error         { return nil }
func (h peopleCaptureHost) ScreenCapture() ([]byte, error)  { return h.png, h.err }
func (h peopleCaptureHost) WindowCapture(string) ([]byte, int, int, error) {
	return nil, 0, 0, nil
}
func (h peopleCaptureHost) ActiveWindow() (string, string, error) { return "", "", nil }
func (h peopleCaptureHost) ListWindows() ([]ccapp.WindowInfo, error) {
	return nil, nil
}
func (h peopleCaptureHost) FocusWindow(string) (ccapp.WindowInfo, error) {
	return ccapp.WindowInfo{}, nil
}
func (h peopleCaptureHost) ObserveDialogs() ([]ccapp.DialogSnapshot, error) { return nil, nil }
func (h peopleCaptureHost) ConfirmDialog(string) (ccapp.DialogSnapshot, error) {
	return ccapp.DialogSnapshot{}, nil
}
func (h peopleCaptureHost) ObserveUI(int) ([]ccapp.UINode, error) { return nil, nil }
func (h peopleCaptureHost) ClipboardGet() (string, error)         { return "", nil }
func (h peopleCaptureHost) ClipboardSet(string) error             { return nil }
func (h peopleCaptureHost) WindowAction(string, string, int, int, int, int) (ccapp.WindowInfo, error) {
	return ccapp.WindowInfo{}, nil
}
func (h peopleCaptureHost) QuitApp(string) (int, ccapp.WindowInfo, error) {
	return 0, ccapp.WindowInfo{}, nil
}
func (h peopleCaptureHost) MenuClick(string) error        { return nil }
func (h peopleCaptureHost) SetValue(string, string) error { return nil }
func (h peopleCaptureHost) InvokeUI(string) error         { return nil }

func TestPeopleScreenCaptureUsesNativeHost(t *testing.T) {
	e, _, _ := newPeopleEngine(t)
	store, err := sqlitestore.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "cc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ccSvc := ccapp.New(store.AgentRuntimeRepository())
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	ccSvc.SetHost(peopleCaptureHost{png: png})
	e.SetCcControlService(ccSvc)

	got := peopleOK[map[string]any](t, e, "people.screen.capture", map[string]any{})
	if got["mimeType"] != "image/png" {
		t.Fatalf("mimeType = %#v", got["mimeType"])
	}
	raw, _ := got["contentBase64"].(string)
	if raw == "" {
		t.Fatal("missing contentBase64")
	}
	if decoded, err := base64.StdEncoding.DecodeString(raw); err != nil || string(decoded) != string(png) {
		t.Fatalf("decode = %q err=%v", decoded, err)
	}
}

func TestPeopleScreenCaptureUnsupportedWithoutHost(t *testing.T) {
	e, _, _ := newPeopleEngine(t)
	resp := peopleCall(t, e, "people.screen.capture", map[string]any{})
	if resp.OK {
		t.Fatal("expected failure without cc host")
	}
	if resp.Error == nil || resp.Error.Code != "PEOPLE_CAPTURE_UNSUPPORTED" {
		t.Fatalf("code = %+v", resp.Error)
	}
}

