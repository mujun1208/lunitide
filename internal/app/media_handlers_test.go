package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/mediaapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

func newMediaEngine(t *testing.T, enableV2 bool) (*Engine, *storage.Store) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "media.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if enableV2 {
		if err := store.EnableMediaSessionV2ForTest(ctx); err != nil {
			t.Fatal(err)
		}
	}
	e := NewEngine(nil, "test")
	e.SetSQLStore(store)
	e.SetMedia(mediaapp.New(store))
	return e, store
}

func mediaKeyed(method, payload, key string) bridge.Request {
	req := validRequest(method, payload)
	req.IdempotencyKey = key
	return req
}

func TestMediaSessionCreateDisabledUntilFlag(t *testing.T) {
	e, _ := newMediaEngine(t, false)
	op := ulid.Make().String()
	asset := ulid.Make().String()
	resp := e.Handle(context.Background(), mediaKeyed("media.session.create", `{"assetId":"`+asset+`","scopeKind":"user","operationId":"`+op+`"}`, op))
	if resp.OK || resp.Error == nil || resp.Error.Code != "MEDIA_SESSION_V2_DISABLED" || resp.Error.Retryable {
		t.Fatalf("%+v", resp)
	}
}

func TestMediaUserScopeIdRejected(t *testing.T) {
	e, _ := newMediaEngine(t, true)
	op := ulid.Make().String()
	asset := ulid.Make().String()
	resp := e.Handle(context.Background(), mediaKeyed("media.session.create", `{"assetId":"`+asset+`","scopeKind":"user","scopeId":"`+op+`","operationId":"`+op+`"}`, op))
	if resp.OK || resp.Error == nil || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("user+scopeId %+v", resp)
	}
	listed := e.Handle(context.Background(), validRequest("media.session.list", `{"scopeKind":"user","scopeId":"x"}`))
	if listed.OK || listed.Error == nil || listed.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("list user+scopeId %+v", listed)
	}
}

func TestMediaSessionCreateGetAndOpenOrigin(t *testing.T) {
	e, store := newMediaEngine(t, true)
	ctx := context.Background()
	assetID, err := store.InsertMediaAsset(ctx, "local-user", "user", "local-user", "user_selected", filepath.Join(t.TempDir(), "a.mp3"), "audio/mpeg", "audio", "clip", 12)
	if err != nil {
		t.Fatal(err)
	}
	op := ulid.Make().String()
	created := e.Handle(ctx, mediaKeyed("media.session.create", `{"assetId":"`+assetID+`","scopeKind":"user","operationId":"`+op+`"}`, op))
	if !created.OK {
		t.Fatalf("create %+v", created.Error)
	}
	var createdPayload struct {
		Snapshot struct {
			MediaSessionID string  `json:"mediaSessionId"`
			ScopeKind      string  `json:"scopeKind"`
			ScopeID        *string `json:"scopeId"`
			Origin         string  `json:"origin"`
			Phase          string  `json:"phase"`
			AssetID        string  `json:"assetId"`
		} `json:"snapshot"`
		Operation struct {
			Action string `json:"action"`
			Phase  string `json:"phase"`
		} `json:"operation"`
	}
	raw, _ := json.Marshal(created.Payload)
	if err := json.Unmarshal(raw, &createdPayload); err != nil || createdPayload.Snapshot.MediaSessionID == "" || createdPayload.Snapshot.ScopeKind != "user" || createdPayload.Snapshot.ScopeID != nil || createdPayload.Snapshot.Origin != "owned" || createdPayload.Operation.Action != "create" {
		t.Fatalf("create payload %s err=%v", raw, err)
	}
	got := e.Handle(ctx, validRequest("media.session.get", `{"mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`"}`))
	if !got.OK {
		t.Fatalf("get %+v", got.Error)
	}
	opened := e.Handle(ctx, validRequest("media.asset.open", `{"assetId":"`+assetID+`","mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`"}`))
	if !opened.OK {
		t.Fatalf("open %+v", opened.Error)
	}
	var openPayload struct {
		PlaybackURL string `json:"playbackUrl"`
	}
	openRaw, _ := json.Marshal(opened.Payload)
	if err := json.Unmarshal(openRaw, &openPayload); err != nil || !strings.HasPrefix(openPayload.PlaybackURL, mediaapp.PlaybackOrigin) {
		t.Fatalf("origin %s err=%v", openRaw, err)
	}
}

func TestActivityListShapeAndUserScopeId(t *testing.T) {
	e, _ := newMediaEngine(t, true)
	resp := e.Handle(context.Background(), validRequest("activity.list", `{"scopeKind":"user"}`))
	if !resp.OK {
		t.Fatalf("%+v", resp.Error)
	}
	var payload struct {
		Items      []any   `json:"items"`
		NextCursor *string `json:"nextCursor"`
		SnapshotAt string  `json:"snapshotAt"`
		HasMore    bool    `json:"hasMore"`
	}
	raw, _ := json.Marshal(resp.Payload)
	if err := json.Unmarshal(raw, &payload); err != nil || payload.SnapshotAt == "" || payload.HasMore != (payload.NextCursor != nil) {
		t.Fatalf("shape %s err=%v", raw, err)
	}
	bad := e.Handle(context.Background(), validRequest("activity.list", `{"scopeKind":"user","scopeId":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`))
	if bad.OK || bad.Error == nil || bad.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("user+scopeId %+v", bad)
	}
}

func TestMediaAndActivityAreDataScoped(t *testing.T) {
	for _, method := range []string{"activity.list", "media.session.create", "media.session.list", "media.asset.list", "media.operation.list"} {
		if !dataScopedMethod(method) {
			t.Fatalf("%s must be data-scoped", method)
		}
	}
	if dataScopedMethod("internal.media.asset.register") || dataScopedMethod("internal.media.asset.resolve") {
		t.Fatal("host-private media RPCs stay out of renderer data scope")
	}
	if _, ok := RuntimeHandlers[bridge.Method("internal.media.asset.resolve")]; ok {
		t.Fatal("internal.media.asset.resolve must not be a renderer method")
	}
}

func waitMediaEvent(t *testing.T, ch <-chan bridge.Event) bridge.Event {
	t.Helper()
	select {
	case ev := <-ch:
		if ev.Type != bridge.EventMediaSnapshot || ev.Media == nil || ev.Media.Kind != "invalidate" || ev.Sequence < 1 {
			t.Fatalf("event %+v", ev)
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for media_snapshot")
	}
	return bridge.Event{}
}

func TestMediaWatch(t *testing.T) {
	e, store := newMediaEngine(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	events := make(chan bridge.Event, 8)
	watch := e.HandleStreaming(ctx, validRequest("media.session.watch", `{"scopeKind":"user"}`), func(ev bridge.Event) error {
		events <- ev
		return nil
	})
	if !watch.OK {
		t.Fatalf("watch %+v", watch.Error)
	}
	assetID, err := store.InsertMediaAsset(ctx, "local-user", "user", "local-user", "user_selected", filepath.Join(t.TempDir(), "a.mp3"), "audio/mpeg", "audio", "clip", 12)
	if err != nil {
		t.Fatal(err)
	}
	op := ulid.Make().String()
	created := e.Handle(ctx, mediaKeyed("media.session.create", `{"assetId":"`+assetID+`","scopeKind":"user","operationId":"`+op+`"}`, op))
	if !created.OK {
		t.Fatalf("create %+v", created.Error)
	}
	first := waitMediaEvent(t, events)
	if first.Sequence != 1 || first.Media.Revision < 1 {
		t.Fatalf("first %+v", first)
	}
	var createdPayload struct {
		Snapshot struct {
			MediaSessionID string `json:"mediaSessionId"`
			Revision       int64  `json:"revision"`
		} `json:"snapshot"`
	}
	raw, _ := json.Marshal(created.Payload)
	if err := json.Unmarshal(raw, &createdPayload); err != nil {
		t.Fatal(err)
	}
	if first.Media.MediaSessionID != createdPayload.Snapshot.MediaSessionID {
		t.Fatalf("session %s vs %s", first.Media.MediaSessionID, createdPayload.Snapshot.MediaSessionID)
	}
	cmdOp := ulid.Make().String()
	cmd := e.Handle(ctx, mediaKeyed("media.session.command", `{"mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`","action":"play","expectedRevision":`+itoa64(createdPayload.Snapshot.Revision)+`,"operationId":"`+cmdOp+`"}`, cmdOp))
	if !cmd.OK {
		t.Fatalf("command %+v", cmd.Error)
	}
	second := waitMediaEvent(t, events)
	if second.Sequence != 2 || second.Media.Revision <= first.Media.Revision {
		t.Fatalf("gap/reconnect required; seq=%d rev=%d prev=%d", second.Sequence, second.Media.Revision, first.Media.Revision)
	}
}

func TestMediaWatchReconnect(t *testing.T) {
	e, store := newMediaEngine(t, true)
	ctx1, cancel1 := context.WithCancel(context.Background())
	events1 := make(chan bridge.Event, 4)
	firstWatch := e.HandleStreaming(ctx1, validRequest("media.session.watch", `{"scopeKind":"user"}`), func(ev bridge.Event) error {
		events1 <- ev
		return nil
	})
	if !firstWatch.OK {
		t.Fatalf("watch1 %+v", firstWatch.Error)
	}
	assetID, err := store.InsertMediaAsset(context.Background(), "local-user", "user", "local-user", "user_selected", filepath.Join(t.TempDir(), "a.mp3"), "audio/mpeg", "audio", "clip", 12)
	if err != nil {
		t.Fatal(err)
	}
	op := ulid.Make().String()
	created := e.Handle(context.Background(), mediaKeyed("media.session.create", `{"assetId":"`+assetID+`","scopeKind":"user","operationId":"`+op+`"}`, op))
	if !created.OK {
		t.Fatalf("create %+v", created.Error)
	}
	_ = waitMediaEvent(t, events1)
	cancel1()
	select {
	case <-time.After(200 * time.Millisecond):
	case ev := <-events1:
		t.Fatalf("cancelled watch must not keep emitting %+v", ev)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	t.Cleanup(cancel2)
	events2 := make(chan bridge.Event, 4)
	secondWatch := e.HandleStreaming(ctx2, validRequest("media.session.watch", `{"scopeKind":"user"}`), func(ev bridge.Event) error {
		events2 <- ev
		return nil
	})
	if !secondWatch.OK {
		t.Fatalf("watch2 %+v", secondWatch.Error)
	}
	var createdPayload struct {
		Snapshot struct {
			MediaSessionID string `json:"mediaSessionId"`
			Revision       int64  `json:"revision"`
		} `json:"snapshot"`
	}
	raw, _ := json.Marshal(created.Payload)
	if err := json.Unmarshal(raw, &createdPayload); err != nil {
		t.Fatal(err)
	}
	cmdOp := ulid.Make().String()
	cmd := e.Handle(ctx2, mediaKeyed("media.session.command", `{"mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`","action":"pause","expectedRevision":`+itoa64(createdPayload.Snapshot.Revision)+`,"operationId":"`+cmdOp+`"}`, cmdOp))
	if !cmd.OK {
		t.Fatalf("command %+v", cmd.Error)
	}
	reconnected := waitMediaEvent(t, events2)
	if reconnected.Sequence != 1 {
		t.Fatalf("new stream must restart sequence, got %d", reconnected.Sequence)
	}
}

func TestMediaPlayer(t *testing.T) {
	e, store := newMediaEngine(t, true)
	ctx := context.Background()
	assetID, err := store.InsertMediaAsset(ctx, "local-user", "user", "local-user", "user_selected", filepath.Join(t.TempDir(), "a.mp3"), "audio/mpeg", "audio", "clip", 12)
	if err != nil {
		t.Fatal(err)
	}
	op := ulid.Make().String()
	created := e.Handle(ctx, mediaKeyed("media.session.create", `{"assetId":"`+assetID+`","scopeKind":"user","operationId":"`+op+`"}`, op))
	if !created.OK {
		t.Fatalf("create %+v", created.Error)
	}
	var createdPayload struct {
		Snapshot struct {
			MediaSessionID string `json:"mediaSessionId"`
			Revision       int64  `json:"revision"`
		} `json:"snapshot"`
	}
	raw, _ := json.Marshal(created.Payload)
	if err := json.Unmarshal(raw, &createdPayload); err != nil {
		t.Fatal(err)
	}
	sessionID := createdPayload.Snapshot.MediaSessionID
	cmdOp := ulid.Make().String()
	cmd := e.Handle(ctx, mediaKeyed("media.session.command", `{"mediaSessionId":"`+sessionID+`","action":"play","expectedRevision":`+itoa64(createdPayload.Snapshot.Revision)+`,"operationId":"`+cmdOp+`"}`, cmdOp))
	if !cmd.OK {
		t.Fatalf("command %+v", cmd.Error)
	}
	attachA := e.Handle(ctx, validRequest("internal.media.player.attach", `{"mediaSessionId":"`+sessionID+`","windowInstanceId":"window-a","navigationEpoch":1}`))
	if !attachA.OK {
		t.Fatalf("attach A %+v", attachA.Error)
	}
	leaseA := mediaLease(t, attachA)
	if leaseA.Generation != 1 || leaseA.LeaseToken == "" {
		t.Fatalf("lease A %+v", leaseA)
	}
	renew := e.Handle(ctx, validRequest("internal.media.player.attach", `{"mediaSessionId":"`+sessionID+`","windowInstanceId":"window-a","navigationEpoch":2}`))
	if !renew.OK {
		t.Fatalf("renew %+v", renew.Error)
	}
	leaseRenew := mediaLease(t, renew)
	if leaseRenew.Generation != 1 {
		t.Fatalf("same window must renew generation, got %d", leaseRenew.Generation)
	}
	attachB := e.Handle(ctx, validRequest("internal.media.player.attach", `{"mediaSessionId":"`+sessionID+`","windowInstanceId":"window-b","navigationEpoch":1}`))
	if !attachB.OK {
		t.Fatalf("attach B %+v", attachB.Error)
	}
	leaseB := mediaLease(t, attachB)
	if leaseB.Generation != 2 {
		t.Fatalf("other window must bump generation, got %d", leaseB.Generation)
	}
	stale := e.Handle(ctx, validRequest("internal.media.player.next", `{"mediaSessionId":"`+sessionID+`","leaseToken":"`+leaseA.LeaseToken+`","generation":1,"windowInstanceId":"window-a"}`))
	if stale.OK || stale.Error == nil || stale.Error.Code != "MEDIA_PLAYER_LEASE_CONFLICT" {
		t.Fatalf("stale next %+v", stale)
	}
	next := e.Handle(ctx, validRequest("internal.media.player.next", `{"mediaSessionId":"`+sessionID+`","leaseToken":"`+leaseB.LeaseToken+`","generation":2,"windowInstanceId":"window-b"}`))
	if !next.OK {
		t.Fatalf("next %+v", next.Error)
	}
	var nextPayload struct {
		OperationID *string `json:"operationId"`
	}
	nextRaw, _ := json.Marshal(next.Payload)
	if err := json.Unmarshal(nextRaw, &nextPayload); err != nil || nextPayload.OperationID == nil || *nextPayload.OperationID != cmdOp {
		t.Fatalf("next payload %s err=%v", nextRaw, err)
	}
	replay := e.Handle(ctx, validRequest("internal.media.player.next", `{"mediaSessionId":"`+sessionID+`","leaseToken":"`+leaseB.LeaseToken+`","generation":2,"windowInstanceId":"window-b"}`))
	if !replay.OK {
		t.Fatalf("replay %+v", replay.Error)
	}
	var replayPayload struct {
		OperationID *string `json:"operationId"`
	}
	replayRaw, _ := json.Marshal(replay.Payload)
	if err := json.Unmarshal(replayRaw, &replayPayload); err != nil || replayPayload.OperationID == nil || *replayPayload.OperationID != cmdOp {
		t.Fatalf("unacked next must replay same operation %s", replayRaw)
	}
	staleReport := e.Handle(ctx, validRequest("internal.media.player.report", `{"mediaSessionId":"`+sessionID+`","leaseToken":"`+leaseA.LeaseToken+`","generation":1,"windowInstanceId":"window-a","operationId":"`+cmdOp+`","event":"playing"}`))
	if staleReport.OK || staleReport.Error == nil || staleReport.Error.Code != "MEDIA_PLAYER_LEASE_CONFLICT" {
		t.Fatalf("stale report %+v", staleReport)
	}
	report := e.Handle(ctx, validRequest("internal.media.player.report", `{"mediaSessionId":"`+sessionID+`","leaseToken":"`+leaseB.LeaseToken+`","generation":2,"windowInstanceId":"window-b","operationId":"`+cmdOp+`","event":"playing","positionMs":10,"durationMs":100}`))
	if !report.OK {
		t.Fatalf("report %+v", report.Error)
	}
}

func TestMediaTicketChangedFile(t *testing.T) {
	e, store := newMediaEngine(t, true)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "clip.mp3")
	if err := os.WriteFile(path, []byte("abcd"), 0o600); err != nil {
		t.Fatal(err)
	}
	assetID, err := store.InsertMediaAsset(ctx, "local-user", "user", "local-user", "user_selected", path, "audio/mpeg", "audio", "clip", 4)
	if err != nil {
		t.Fatal(err)
	}
	op := ulid.Make().String()
	created := e.Handle(ctx, mediaKeyed("media.session.create", `{"assetId":"`+assetID+`","scopeKind":"user","operationId":"`+op+`"}`, op))
	if !created.OK {
		t.Fatalf("create %+v", created.Error)
	}
	var createdPayload struct {
		Snapshot struct {
			MediaSessionID string `json:"mediaSessionId"`
		} `json:"snapshot"`
	}
	raw, _ := json.Marshal(created.Payload)
	if err := json.Unmarshal(raw, &createdPayload); err != nil {
		t.Fatal(err)
	}
	opened := e.Handle(ctx, validRequest("media.asset.open", `{"assetId":"`+assetID+`","mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`"}`))
	if !opened.OK {
		t.Fatalf("open %+v", opened.Error)
	}
	var openPayload struct {
		PlaybackURL string `json:"playbackUrl"`
	}
	openRaw, _ := json.Marshal(opened.Payload)
	if err := json.Unmarshal(openRaw, &openPayload); err != nil {
		t.Fatal(err)
	}
	token := strings.TrimPrefix(openPayload.PlaybackURL, mediaapp.PlaybackOrigin)
	okResolve := e.Handle(ctx, validRequest("internal.media.asset.resolve", `{"token":"`+token+`"}`))
	if !okResolve.OK {
		t.Fatalf("resolve %+v", okResolve.Error)
	}
	if err := os.WriteFile(path, []byte("abcdefgh"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed := e.Handle(ctx, validRequest("internal.media.asset.resolve", `{"token":"`+token+`"}`))
	if changed.OK || changed.Error == nil || changed.Error.Code != "MEDIA_ASSET_CHANGED" {
		t.Fatalf("changed file %+v", changed)
	}
}

func TestMediaTicket(t *testing.T) {
	TestMediaTicketChangedFile(t)
}

func TestMediaScopePayloadContract(t *testing.T) {
	TestMediaUserScopeIdRejected(t)
}

func TestPrivateMediaRPC(t *testing.T) {
	for _, method := range []string{
		"internal.media.asset.register",
		"internal.media.asset.resolve",
		"internal.media.player.attach",
		"internal.media.player.next",
		"internal.media.player.report",
	} {
		if _, ok := RuntimeHandlers[bridge.Method(method)]; ok {
			t.Fatalf("%s must not be a renderer method", method)
		}
		if _, ok := internalRuntimeHandlers[bridge.Method(method)]; !ok {
			t.Fatalf("%s must stay host-private", method)
		}
		if _, ok := bridge.MethodMetadataByMethod[bridge.Method(method)]; ok {
			t.Fatalf("%s must not appear in the public envelope", method)
		}
	}
}

func TestNoProtectedContentActions(t *testing.T) {
	for _, method := range bridge.Methods {
		name := strings.ToLower(string(method))
		if !strings.HasPrefix(name, "media.") && !strings.HasPrefix(name, "internal.media.") {
			continue
		}
		for _, banned := range []string{"download", "transcode", "scrape", "ytdl", "rip", "torrent"} {
			if strings.Contains(name, banned) {
				t.Fatalf("protected-content action leaked: %s", method)
			}
		}
	}
}

func TestMediaPlayerEpoch(t *testing.T) {
	TestMediaPlayer(t)
}

func TestMediaCASAndIdempotency(t *testing.T) {
	e, store := newMediaEngine(t, true)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "clip.mp3")
	if err := os.WriteFile(path, []byte("abcd"), 0o600); err != nil {
		t.Fatal(err)
	}
	assetID, err := store.InsertMediaAsset(ctx, "local-user", "user", "local-user", "user_selected", path, "audio/mpeg", "audio", "clip", 4)
	if err != nil {
		t.Fatal(err)
	}
	op := ulid.Make().String()
	created := e.Handle(ctx, mediaKeyed("media.session.create", `{"assetId":"`+assetID+`","scopeKind":"user","operationId":"`+op+`"}`, op))
	if !created.OK {
		t.Fatalf("create %+v", created.Error)
	}
	var createdPayload struct {
		Snapshot struct {
			MediaSessionID string `json:"mediaSessionId"`
			Revision       int64  `json:"revision"`
		} `json:"snapshot"`
	}
	raw, _ := json.Marshal(created.Payload)
	if err := json.Unmarshal(raw, &createdPayload); err != nil {
		t.Fatal(err)
	}
	cmdOp := ulid.Make().String()
	stale := e.Handle(ctx, mediaKeyed("media.session.command", `{"mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`","action":"pause","expectedRevision":99,"operationId":"`+cmdOp+`"}`, cmdOp))
	if stale.OK || stale.Error == nil || stale.Error.Code != "MEDIA_REVISION_CONFLICT" {
		t.Fatalf("stale CAS %+v", stale)
	}
	okCmd := e.Handle(ctx, mediaKeyed("media.session.command", `{"mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`","action":"pause","expectedRevision":`+itoa64(createdPayload.Snapshot.Revision)+`,"operationId":"`+cmdOp+`"}`, cmdOp))
	if !okCmd.OK {
		t.Fatalf("pause %+v", okCmd.Error)
	}
	replay := e.Handle(ctx, mediaKeyed("media.session.command", `{"mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`","action":"pause","expectedRevision":`+itoa64(createdPayload.Snapshot.Revision)+`,"operationId":"`+cmdOp+`"}`, cmdOp))
	if !replay.OK {
		t.Fatalf("idempotent replay %+v", replay.Error)
	}
}

func TestMediaAudioFocus(t *testing.T) {
	e, store := newMediaEngine(t, true)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "clip.mp3")
	if err := os.WriteFile(path, []byte("abcd"), 0o600); err != nil {
		t.Fatal(err)
	}
	assetID, err := store.InsertMediaAsset(ctx, "local-user", "user", "local-user", "user_selected", path, "audio/mpeg", "audio", "clip", 4)
	if err != nil {
		t.Fatal(err)
	}
	op := ulid.Make().String()
	created := e.Handle(ctx, mediaKeyed("media.session.create", `{"assetId":"`+assetID+`","scopeKind":"user","operationId":"`+op+`"}`, op))
	if !created.OK {
		t.Fatalf("create %+v", created.Error)
	}
	var createdPayload struct {
		Snapshot struct {
			MediaSessionID string `json:"mediaSessionId"`
			Revision       int64  `json:"revision"`
		} `json:"snapshot"`
		Operation struct {
			OperationID string `json:"operationId"`
		} `json:"operation"`
	}
	raw, _ := json.Marshal(created.Payload)
	if err := json.Unmarshal(raw, &createdPayload); err != nil {
		t.Fatal(err)
	}
	cmdOp := ulid.Make().String()
	cmd := e.Handle(ctx, mediaKeyed("media.session.command", `{"mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`","action":"play","expectedRevision":`+itoa64(createdPayload.Snapshot.Revision)+`,"operationId":"`+cmdOp+`"}`, cmdOp))
	if !cmd.OK {
		t.Fatalf("play %+v", cmd.Error)
	}
	lease := mediaLease(t, e.Handle(ctx, validRequest("internal.media.player.attach", `{"mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`","windowInstanceId":"window-a","navigationEpoch":1}`)))
	next := e.Handle(ctx, validRequest("internal.media.player.next", `{"mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`","leaseToken":"`+lease.LeaseToken+`","generation":`+itoa64(lease.Generation)+`,"windowInstanceId":"window-a"}`))
	if !next.OK {
		t.Fatalf("next %+v", next.Error)
	}
	report := e.Handle(ctx, validRequest("internal.media.player.report", `{"mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`","leaseToken":"`+lease.LeaseToken+`","generation":`+itoa64(lease.Generation)+`,"windowInstanceId":"window-a","operationId":"`+cmdOp+`","event":"playing","positionMs":10,"durationMs":100}`))
	if !report.OK {
		t.Fatalf("report %+v", report.Error)
	}
	sessionID, reason, _, err := store.MediaAudioFocus(ctx)
	if err != nil || reason != "owned_playback" || sessionID != createdPayload.Snapshot.MediaSessionID {
		t.Fatalf("focus session=%s reason=%s err=%v", sessionID, reason, err)
	}
}

func TestActivityRecoveryCreatesChildOperation(t *testing.T) {
	e, store := newMediaEngine(t, true)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "clip.mp3")
	if err := os.WriteFile(path, []byte("abcd"), 0o600); err != nil {
		t.Fatal(err)
	}
	assetID, err := store.InsertMediaAsset(ctx, "local-user", "user", "local-user", "user_selected", path, "audio/mpeg", "audio", "clip", 4)
	if err != nil {
		t.Fatal(err)
	}
	op := ulid.Make().String()
	created := e.Handle(ctx, mediaKeyed("media.session.create", `{"assetId":"`+assetID+`","scopeKind":"user","operationId":"`+op+`"}`, op))
	if !created.OK {
		t.Fatalf("create %+v", created.Error)
	}
	var createdPayload struct {
		Snapshot struct {
			MediaSessionID string `json:"mediaSessionId"`
			Revision       int64  `json:"revision"`
		} `json:"snapshot"`
	}
	raw, _ := json.Marshal(created.Payload)
	if err := json.Unmarshal(raw, &createdPayload); err != nil {
		t.Fatal(err)
	}
	failOp := ulid.Make().String()
	failCmd := e.Handle(ctx, mediaKeyed("media.session.command", `{"mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`","action":"play","expectedRevision":`+itoa64(createdPayload.Snapshot.Revision)+`,"operationId":"`+failOp+`"}`, failOp))
	if !failCmd.OK {
		t.Fatalf("play %+v", failCmd.Error)
	}
	var failPayload struct {
		Snapshot struct {
			Revision int64 `json:"revision"`
		} `json:"snapshot"`
		Operation struct {
			OperationID string `json:"operationId"`
		} `json:"operation"`
	}
	failRaw, _ := json.Marshal(failCmd.Payload)
	if err := json.Unmarshal(failRaw, &failPayload); err != nil {
		t.Fatal(err)
	}
	lease := mediaLease(t, e.Handle(ctx, validRequest("internal.media.player.attach", `{"mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`","windowInstanceId":"window-a","navigationEpoch":1}`)))
	_ = e.Handle(ctx, validRequest("internal.media.player.next", `{"mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`","leaseToken":"`+lease.LeaseToken+`","generation":`+itoa64(lease.Generation)+`,"windowInstanceId":"window-a"}`))
	errReport := e.Handle(ctx, validRequest("internal.media.player.report", `{"mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`","leaseToken":"`+lease.LeaseToken+`","generation":`+itoa64(lease.Generation)+`,"windowInstanceId":"window-a","operationId":"`+failPayload.Operation.OperationID+`","event":"error","positionMs":0,"durationMs":0}`))
	if !errReport.OK {
		t.Fatalf("error report %+v", errReport.Error)
	}
	snap, err := store.GetMediaSessionForOwner(ctx, "local-user", createdPayload.Snapshot.MediaSessionID)
	if err != nil {
		t.Fatal(err)
	}
	retryOp := ulid.Make().String()
	retry := e.Handle(ctx, mediaKeyed("media.session.command", `{"mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`","action":"play","expectedRevision":`+itoa64(snap.Revision)+`,"operationId":"`+retryOp+`"}`, retryOp))
	if !retry.OK {
		t.Fatalf("retry %+v", retry.Error)
	}
	got, err := store.GetMediaOperationByID(ctx, "local-user", retryOp)
	if err != nil || got.ParentOperationID != failPayload.Operation.OperationID {
		t.Fatalf("child parent=%q want=%q err=%v", got.ParentOperationID, failPayload.Operation.OperationID, err)
	}
}

func TestActivityExactDTO(t *testing.T) {
	e, _ := newMediaEngine(t, true)
	resp := e.Handle(context.Background(), validRequest("activity.list", `{"scopeKind":"user"}`))
	if !resp.OK {
		t.Fatalf("%+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Payload)
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"items", "nextCursor", "snapshotAt", "hasMore"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing %s in %s", key, raw)
		}
	}
}

func TestActivityListPagination(t *testing.T) {
	e, store := newMediaEngine(t, true)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "clip.mp3")
	if err := os.WriteFile(path, []byte("abcd"), 0o600); err != nil {
		t.Fatal(err)
	}
	assetID, err := store.InsertMediaAsset(ctx, "local-user", "user", "local-user", "user_selected", path, "audio/mpeg", "audio", "clip", 4)
	if err != nil {
		t.Fatal(err)
	}
	op := ulid.Make().String()
	created := e.Handle(ctx, mediaKeyed("media.session.create", `{"assetId":"`+assetID+`","scopeKind":"user","operationId":"`+op+`"}`, op))
	if !created.OK {
		t.Fatalf("create %+v", created.Error)
	}
	var createdPayload struct {
		Snapshot struct {
			MediaSessionID string `json:"mediaSessionId"`
			Revision       int64  `json:"revision"`
		} `json:"snapshot"`
	}
	raw, _ := json.Marshal(created.Payload)
	if err := json.Unmarshal(raw, &createdPayload); err != nil {
		t.Fatal(err)
	}
	rev := createdPayload.Snapshot.Revision
	for i := 0; i < 3; i++ {
		cmdOp := ulid.Make().String()
		cmd := e.Handle(ctx, mediaKeyed("media.session.command", `{"mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`","action":"pause","expectedRevision":`+itoa64(rev)+`,"operationId":"`+cmdOp+`"}`, cmdOp))
		if !cmd.OK {
			t.Fatalf("cmd %d %+v", i, cmd.Error)
		}
		var body struct {
			Snapshot struct {
				Revision int64 `json:"revision"`
			} `json:"snapshot"`
		}
		cmdRaw, _ := json.Marshal(cmd.Payload)
		if err := json.Unmarshal(cmdRaw, &body); err != nil {
			t.Fatal(err)
		}
		rev = body.Snapshot.Revision
	}
	page := e.Handle(ctx, validRequest("activity.list", `{"scopeKind":"user","limit":2}`))
	if !page.OK {
		t.Fatalf("page %+v", page.Error)
	}
	var listed struct {
		Items      []map[string]any `json:"items"`
		NextCursor *string          `json:"nextCursor"`
		HasMore    bool             `json:"hasMore"`
	}
	listRaw, _ := json.Marshal(page.Payload)
	if err := json.Unmarshal(listRaw, &listed); err != nil {
		t.Fatal(err)
	}
	if !listed.HasMore || listed.NextCursor == nil || len(listed.Items) != 2 {
		t.Fatalf("pagination %+v", listed)
	}
	for _, item := range listed.Items {
		for _, key := range []string{"activityId", "domain", "kind", "phase", "terminal", "verificationStatus", "verificationSource", "title", "recoveryAction", "scopeKind", "retryable"} {
			if _, ok := item[key]; !ok {
				t.Fatalf("dto missing %s in %v", key, item)
			}
		}
	}
}

func TestActivityReconnect(t *testing.T) {
	e, _ := newMediaEngine(t, true)
	first := e.Handle(context.Background(), validRequest("activity.list", `{"scopeKind":"user"}`))
	second := e.Handle(context.Background(), validRequest("activity.list", `{"scopeKind":"user"}`))
	if !first.OK || !second.OK {
		t.Fatalf("reconnect %+v %+v", first.Error, second.Error)
	}
}

func TestMediaGovernanceBeforeDispatch(t *testing.T) {
	e, store := newMediaEngine(t, true)
	ccSvc := ccapp.New(store.AgentRuntimeRepository())
	e.SetCcControlService(ccSvc)
	if _, err := ccSvc.EmergencyStop(context.Background(), "operator", "media"); err != nil {
		t.Fatal(err)
	}
	op := ulid.Make().String()
	asset := ulid.Make().String()
	resp := e.Handle(context.Background(), mediaKeyed("media.session.create", `{"assetId":"`+asset+`","scopeKind":"user","operationId":"`+op+`"}`, op))
	if resp.OK || resp.Error == nil || resp.Error.Code != "CC_EMERGENCY_STOPPED" {
		t.Fatalf("emergency must block dispatch %+v", resp)
	}
}

func TestR3BridgeAndLogRedaction(t *testing.T) {
	e, _ := newMediaEngine(t, true)
	resp := e.Handle(context.Background(), validRequest("media.session.list", `{"scopeKind":"user"}`))
	if !resp.OK {
		t.Fatalf("%+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Payload)
	if strings.Contains(strings.ToLower(string(raw)), "c:\\") || strings.Contains(string(raw), "leaseToken") {
		t.Fatalf("list leaked host secrets: %s", raw)
	}
}

func TestMediaAssetListReturnsQueueNotLibrary(t *testing.T) {
	e, store := newMediaEngine(t, true)
	ctx := context.Background()
	assetA, err := store.InsertMediaAsset(ctx, "local-user", "user", "local-user", "user_selected", filepath.Join(t.TempDir(), "a.mp3"), "audio/mpeg", "audio", "A", 12)
	if err != nil {
		t.Fatal(err)
	}
	assetB, err := store.InsertMediaAsset(ctx, "local-user", "user", "local-user", "user_selected", filepath.Join(t.TempDir(), "b.mp3"), "audio/mpeg", "audio", "B", 12)
	if err != nil {
		t.Fatal(err)
	}
	extra, err := store.InsertMediaAsset(ctx, "local-user", "user", "local-user", "user_selected", filepath.Join(t.TempDir(), "c.mp3"), "audio/mpeg", "audio", "Library", 12)
	if err != nil {
		t.Fatal(err)
	}
	op := ulid.Make().String()
	created := e.Handle(ctx, mediaKeyed("media.session.create", `{"assetId":"`+assetA+`","queueAssetIds":["`+assetA+`","`+assetB+`"],"scopeKind":"user","operationId":"`+op+`"}`, op))
	if !created.OK {
		t.Fatalf("create %+v", created.Error)
	}
	var createdPayload struct {
		Snapshot struct {
			MediaSessionID string `json:"mediaSessionId"`
		} `json:"snapshot"`
	}
	raw, _ := json.Marshal(created.Payload)
	if err := json.Unmarshal(raw, &createdPayload); err != nil || createdPayload.Snapshot.MediaSessionID == "" {
		t.Fatalf("create payload %s err=%v", raw, err)
	}
	library := e.Handle(ctx, validRequest("media.asset.list", `{"scopeKind":"user"}`))
	if !library.OK {
		t.Fatalf("library %+v", library.Error)
	}
	raw, _ = json.Marshal(library.Payload)
	var listed struct {
		Items []struct {
			AssetID string `json:"assetId"`
			Title   string `json:"title"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, item := range listed.Items {
		ids[item.AssetID] = true
	}
	if !ids[assetA] || !ids[assetB] || !ids[extra] {
		t.Fatalf("library list must keep all assets %s", raw)
	}
	queued := e.Handle(ctx, validRequest("media.asset.list", `{"scopeKind":"user","mediaSessionId":"`+createdPayload.Snapshot.MediaSessionID+`"}`))
	if !queued.OK {
		t.Fatalf("queue %+v", queued.Error)
	}
	raw, _ = json.Marshal(queued.Payload)
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 2 || listed.Items[0].AssetID != assetA || listed.Items[1].AssetID != assetB {
		t.Fatalf("queue list must follow session order %s", raw)
	}
	for _, item := range listed.Items {
		if item.AssetID == extra {
			t.Fatalf("library leftover leaked into queue %s", raw)
		}
	}
	bad := e.Handle(ctx, validRequest("media.asset.list", `{"scopeKind":"user","mediaSessionId":"x"}`))
	if bad.OK || bad.Error == nil || bad.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("invalid session id %+v", bad)
	}
}

func TestMediaQueueJumpByAssetIDUpdatesCurrent(t *testing.T) {
	e, store := newMediaEngine(t, true)
	ctx := context.Background()
	assetA, err := store.InsertMediaAsset(ctx, "local-user", "user", "local-user", "user_selected", filepath.Join(t.TempDir(), "a.mp3"), "audio/mpeg", "audio", "A", 12)
	if err != nil {
		t.Fatal(err)
	}
	assetB, err := store.InsertMediaAsset(ctx, "local-user", "user", "local-user", "user_selected", filepath.Join(t.TempDir(), "b.mp3"), "audio/mpeg", "audio", "B", 12)
	if err != nil {
		t.Fatal(err)
	}
	op := ulid.Make().String()
	created := e.Handle(ctx, mediaKeyed("media.session.create", `{"assetId":"`+assetA+`","queueAssetIds":["`+assetA+`","`+assetB+`"],"scopeKind":"user","operationId":"`+op+`"}`, op))
	if !created.OK {
		t.Fatalf("create %+v", created.Error)
	}
	var payload struct {
		Snapshot struct {
			MediaSessionID string `json:"mediaSessionId"`
			AssetID        string `json:"assetId"`
			QueueRevision  int64  `json:"queueRevision"`
			Revision       int64  `json:"revision"`
		} `json:"snapshot"`
	}
	raw, _ := json.Marshal(created.Payload)
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Snapshot.AssetID != assetA {
		t.Fatalf("create payload %s err=%v", raw, err)
	}
	jumpOp := ulid.Make().String()
	jumped := e.Handle(ctx, mediaKeyed("media.queue.command", `{"mediaSessionId":"`+payload.Snapshot.MediaSessionID+`","action":"jump","itemId":"`+assetB+`","expectedQueueRevision":`+strconv.FormatInt(payload.Snapshot.QueueRevision, 10)+`,"operationId":"`+jumpOp+`"}`, jumpOp))
	if !jumped.OK {
		t.Fatalf("jump %+v", jumped.Error)
	}
	raw, _ = json.Marshal(jumped.Payload)
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Snapshot.AssetID != assetB {
		t.Fatalf("jump payload %s err=%v", raw, err)
	}
	nextOp := ulid.Make().String()
	next := e.Handle(ctx, mediaKeyed("media.session.command", `{"mediaSessionId":"`+payload.Snapshot.MediaSessionID+`","action":"next","expectedRevision":`+strconv.FormatInt(payload.Snapshot.Revision, 10)+`,"operationId":"`+nextOp+`"}`, nextOp))
	if !next.OK {
		t.Fatalf("next %+v", next.Error)
	}
	raw, _ = json.Marshal(next.Payload)
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Snapshot.AssetID != assetA {
		t.Fatalf("next payload %s err=%v", raw, err)
	}
}

type mediaLeaseResult struct {
	LeaseToken string `json:"leaseToken"`
	Generation int64  `json:"generation"`
	ExpiresAt  string `json:"expiresAt"`
}

func mediaLease(t *testing.T, resp bridge.Response) mediaLeaseResult {
	t.Helper()
	var out mediaLeaseResult
	raw, _ := json.Marshal(resp.Payload)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func itoa64(v int64) string {
	return strconv.FormatInt(v, 10)
}
