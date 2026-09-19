package mediahost

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/bridge"
)

type playerEngine struct {
	mu      sync.Mutex
	methods []string
	nextOp  string
	last    bridge.Request
}

func (e *playerEngine) Call(_ context.Context, r bridge.Request) (bridge.Response, error) {
	e.mu.Lock()
	e.methods = append(e.methods, r.Method)
	e.last = r
	e.mu.Unlock()
	switch r.Method {
	case "internal.media.player.attach":
		return bridge.Success(r.ID, map[string]any{"leaseToken": "lease-a", "generation": 1, "expiresAt": time.Now().UTC().Format(time.RFC3339)}), nil
	case "internal.media.player.next":
		if e.nextOp == "" {
			return bridge.Success(r.ID, map[string]any{"operationId": nil, "desiredState": nil}), nil
		}
		return bridge.Success(r.ID, map[string]any{"operationId": e.nextOp, "desiredState": "playing"}), nil
	case "internal.media.player.report":
		return bridge.Success(r.ID, map[string]any{"accepted": true}), nil
	default:
		return bridge.Failure(r.ID, r.TraceID, "UNKNOWN", r.Method, false), nil
	}
}

func (e *playerEngine) listed() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.methods))
	copy(out, e.methods)
	return out
}

func TestPlayerAttachStampsSentAtAndKeepsShortDeadline(t *testing.T) {
	engine := &playerEngine{}
	p := &Player{Engine: engine, WindowInstanceID: "window-a"}
	if err := p.Attach(context.Background(), ulid.Make().String()); err != nil {
		t.Fatal(err)
	}
	if engine.last.SentAt.IsZero() {
		t.Fatal("attach must stamp SentAt")
	}
	if engine.last.DeadlineMS != 8000 {
		t.Fatalf("deadline %d", engine.last.DeadlineMS)
	}
	if engine.last.Method != "internal.media.player.attach" {
		t.Fatalf("method %s", engine.last.Method)
	}
}

func TestPlayerDoesNotReportPlayingWithoutObservedEvent(t *testing.T) {
	engine := &playerEngine{nextOp: ulid.Make().String()}
	p := &Player{Engine: engine, WindowInstanceID: "window-a"}
	session := ulid.Make().String()
	if err := p.Attach(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, method := range engine.listed() {
		if method == "internal.media.player.report" {
			t.Fatal("play() or next must not fabricate player.report")
		}
	}
}

func TestPlayerReportObservedPlayingCallsEngine(t *testing.T) {
	op := ulid.Make().String()
	engine := &playerEngine{nextOp: op}
	p := &Player{Engine: engine, WindowInstanceID: "window-a"}
	if err := p.Attach(context.Background(), ulid.Make().String()); err != nil {
		t.Fatal(err)
	}
	gotOp, desired, err := p.Next(context.Background())
	if err != nil || gotOp != op || desired != "playing" {
		t.Fatalf("next op=%s desired=%s err=%v", gotOp, desired, err)
	}
	if err := p.ReportObserved(context.Background(), "playing", 10, 100); err != nil {
		t.Fatal(err)
	}
	listed := engine.listed()
	if len(listed) != 3 || listed[2] != "internal.media.player.report" {
		t.Fatalf("%v", listed)
	}
}

func TestRunLeaseAttachesOnSnapshotAndNeverReports(t *testing.T) {
	engine := &playerEngine{}
	p := &Player{Engine: engine, WindowInstanceID: "window-a"}
	events := make(chan bridge.Event, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		RunLease(ctx, p, events)
		close(done)
	}()
	session := ulid.Make().String()
	events <- bridge.Event{Type: bridge.EventMediaSnapshot, Media: &bridge.MediaEvent{Kind: "invalidate", MediaSessionID: session, Revision: 1}}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(engine.listed()) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	listed := engine.listed()
	if len(listed) == 0 || listed[0] != "internal.media.player.attach" {
		t.Fatalf("%v", listed)
	}
	raw, _ := json.Marshal(listed)
	if strings.Contains(string(raw), "player.report") || strings.Contains(string(raw), "player.next") {
		t.Fatalf("lease loop must not next/report: %s", raw)
	}
}

func TestPlayerObservePlayingWithoutCommandDoesNotReport(t *testing.T) {
	engine := &playerEngine{}
	p := &Player{Engine: engine, WindowInstanceID: "window-a"}
	accepted, err := p.Observe(context.Background(), ulid.Make().String(), "playing", 0, 100)
	if err != nil || accepted {
		t.Fatalf("playing without command must not be accepted: accepted=%v err=%v", accepted, err)
	}
	for _, method := range engine.listed() {
		if method == "internal.media.player.report" {
			t.Fatal("observe must not fabricate player.report")
		}
	}
}

func TestPlayerObservePlayingReportsAfterNext(t *testing.T) {
	op := ulid.Make().String()
	engine := &playerEngine{nextOp: op}
	p := &Player{Engine: engine, WindowInstanceID: "window-a"}
	accepted, err := p.Observe(context.Background(), ulid.Make().String(), "playing", 10, 100)
	if err != nil || !accepted {
		t.Fatalf("observed playing should report: accepted=%v err=%v", accepted, err)
	}
	listed := engine.listed()
	if len(listed) != 3 || listed[0] != "internal.media.player.attach" || listed[1] != "internal.media.player.next" || listed[2] != "internal.media.player.report" {
		t.Fatalf("%v", listed)
	}
}

func TestStartRenewReattachesWithoutReporting(t *testing.T) {
	engine := &playerEngine{}
	p := &Player{Engine: engine, WindowInstanceID: "window-a", RenewEvery: 20 * time.Millisecond}
	session := ulid.Make().String()
	if err := p.Attach(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.StartRenew(ctx)
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(engine.listed()) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	listed := engine.listed()
	if len(listed) < 2 {
		t.Fatalf("expected renew attach, got %v", listed)
	}
	for _, method := range listed {
		if method != "internal.media.player.attach" {
			t.Fatalf("renew must only attach: %v", listed)
		}
	}
}
