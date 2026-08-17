package sqlite

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/brapp"
)

// fakeBrHost records host calls without touching the real browser
// landscape.
type fakeBrHost struct {
	connects    []string
	disconnects []string
	navigates   []string
	clears      []int
	usage       [3]int64
	connectErr  error
}

func (f *fakeBrHost) Detect(_ context.Context, _ brapp.Settings) (brapp.DetectReport, error) {
	return brapp.DetectReport{Builtin: true}, nil
}

func (f *fakeBrHost) Connect(_ context.Context, sessionID, _ string, _ brapp.Settings) (string, error) {
	if f.connectErr != nil {
		return "", f.connectErr
	}
	f.connects = append(f.connects, sessionID)
	return "ws://127.0.0.1:9333/devtools/browser/fake", nil
}

func (f *fakeBrHost) Disconnect(_ context.Context, sessionID, _ string) error {
	f.disconnects = append(f.disconnects, sessionID)
	return nil
}

func (f *fakeBrHost) Navigate(_ context.Context, _ brapp.Session, rawURL string) error {
	f.navigates = append(f.navigates, rawURL)
	return nil
}

func (f *fakeBrHost) SnapshotUsage(_ context.Context, _ string) (int64, int64, int64) {
	return f.usage[0], f.usage[1], f.usage[2]
}

func (f *fakeBrHost) ClearData(_ context.Context, _ string, _ time.Time) (int64, error) {
	f.clears = append(f.clears, 1)
	return 4096, nil
}

func newBrService(t *testing.T) (*brapp.Service, *fakeBrHost) {
	t.Helper()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "br-multimode.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	host := &fakeBrHost{}
	svc := brapp.New(store.AgentRuntimeRepository(), t.TempDir())
	svc.SetHost(host)
	// Offline DNS stub: every hostname resolves to a public address so
	// the private-network gate never touches the real network.
	svc.SetResolver(func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	})
	return svc, host
}

func TestBrSettingsSeedAndUpdate(t *testing.T) {
	ctx := context.Background()
	svc, _ := newBrService(t)

	settings, err := svc.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Mode != brapp.ModeBuiltin || settings.ExtensionPort != 9222 ||
		settings.DataRetentionDays != 30 || !settings.BlockPrivateNetwork {
		t.Fatalf("unexpected seed: %+v", settings)
	}

	retention := 14
	updated, err := svc.UpdateSettings(ctx, brapp.SettingsPatch{DataRetentionDays: &retention})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DataRetentionDays != 14 || updated.Mode != brapp.ModeBuiltin {
		t.Fatalf("unexpected update: %+v", updated)
	}

	// invalid mode rejected, nothing changed
	bad := "firefox"
	if _, err := svc.UpdateSettings(ctx, brapp.SettingsPatch{Mode: &bad}); !errors.Is(err, brapp.ErrBrSchema) {
		t.Fatalf("expected ErrBrSchema, got %v", err)
	}
}

func TestBrConnectStateMachineAndModeSwitch(t *testing.T) {
	ctx := context.Background()
	svc, host := newBrService(t)

	sess, err := svc.Connect(ctx, "br-test-1", brapp.ModeBuiltin, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if sess.State != brapp.StateConnected || sess.Mode != brapp.ModeBuiltin {
		t.Fatalf("unexpected session: %+v", sess)
	}

	// idempotent re-connect answers the same connected session
	again, err := svc.Connect(ctx, "br-test-1", brapp.ModeBuiltin, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if again.State != brapp.StateConnected || len(host.connects) != 1 {
		t.Fatalf("connect not idempotent: %+v connects=%d", again, len(host.connects))
	}

	// mode switch force-disconnects the live session
	edge := brapp.ModeEdge
	if _, err := svc.UpdateSettings(ctx, brapp.SettingsPatch{Mode: &edge}); err != nil {
		t.Fatal(err)
	}
	sessions, err := svc.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].State != brapp.StateDisconnected {
		t.Fatalf("mode switch should disconnect: %+v", sessions)
	}
	if len(host.disconnects) != 1 || host.disconnects[0] != "br-test-1" {
		t.Fatalf("host disconnect missing: %v", host.disconnects)
	}

	// connect failure parks the session in error
	host.connectErr = errors.New("boom")
	if _, err := svc.Connect(ctx, "br-test-2", brapp.ModeExtension, "tester"); !errors.Is(err, brapp.ErrBrMode) {
		t.Fatalf("expected ErrBrMode, got %v", err)
	}
	sessions, err = svc.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range sessions {
		if s.SessionID == "br-test-2" && s.State == brapp.StateError && strings.Contains(s.Detail, "boom") {
			found = true
		}
	}
	if !found {
		t.Fatalf("error session missing: %+v", sessions)
	}

	// disconnect idempotent
	out, err := svc.Disconnect(ctx, "br-test-1", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if out.State != brapp.StateDisconnected {
		t.Fatalf("unexpected disconnect: %+v", out)
	}
	if _, err := svc.Disconnect(ctx, "br-missing", "tester"); !errors.Is(err, brapp.ErrBrNotFound) {
		t.Fatalf("expected ErrBrNotFound, got %v", err)
	}
}

func TestBrNavigateURLPolicy(t *testing.T) {
	ctx := context.Background()
	svc, _ := newBrService(t)

	sess, err := svc.Connect(ctx, "br-nav-1", brapp.ModeBuiltin, "tester")
	if err != nil {
		t.Fatal(err)
	}
	_ = sess

	cases := []struct {
		url  string
		want error
	}{
		{"https://example.com/docs", nil},
		{"http://example.com:8080/page", nil},
		{"file:///C:/Windows/win.ini", brapp.ErrBrURLPolicy},
		{"https://example.com:9999/page", brapp.ErrBrURLPolicy},
		{"https://127.0.0.1/debug", brapp.ErrBrURLPolicy},
		{"https://192.168.1.10/admin", brapp.ErrBrURLPolicy},
		{"https://localhost:8080/x", brapp.ErrBrURLPolicy},
	}
	for _, tc := range cases {
		_, err := svc.Navigate(ctx, "br-nav-1", tc.url, "tester")
		if tc.want == nil {
			if err != nil {
				t.Fatalf("navigate %q unexpected error: %v", tc.url, err)
			}
			continue
		}
		if !errors.Is(err, tc.want) {
			t.Fatalf("navigate %q: expected %v, got %v", tc.url, tc.want, err)
		}
	}

	// allowlist gate: only listed origin prefixes pass
	allow := []string{"https://docs.example.com"}
	if _, err := svc.UpdateSettings(ctx, brapp.SettingsPatch{Allowlist: &allow}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Navigate(ctx, "br-nav-1", "https://docs.example.com/guide", "tester"); err != nil {
		t.Fatalf("allowlisted navigate failed: %v", err)
	}
	if _, err := svc.Navigate(ctx, "br-nav-1", "https://other.example.com/guide", "tester"); !errors.Is(err, brapp.ErrBrURLPolicy) {
		t.Fatalf("expected allowlist rejection, got %v", err)
	}

	// navigating a disconnected/unknown session
	if _, err := svc.Disconnect(ctx, "br-nav-1", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Navigate(ctx, "br-nav-1", "https://docs.example.com/after", "tester"); !errors.Is(err, brapp.ErrBrState) {
		t.Fatalf("expected ErrBrState, got %v", err)
	}
	if _, err := svc.Navigate(ctx, "br-ghost", "https://docs.example.com/x", "tester"); !errors.Is(err, brapp.ErrBrNotFound) {
		t.Fatalf("expected ErrBrNotFound, got %v", err)
	}
}

func TestBrDataUsageAndClear(t *testing.T) {
	ctx := context.Background()
	svc, host := newBrService(t)
	host.usage = [3]int64{1000, 600, 100}

	if _, err := svc.Connect(ctx, "br-data-1", brapp.ModeBuiltin, "tester"); err != nil {
		t.Fatal(err)
	}
	usage, err := svc.DataUsage(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 1 || usage[0].ProfileBytes != 1000 || usage[0].CacheBytes != 600 || usage[0].CookiesBytes != 100 {
		t.Fatalf("unexpected usage: %+v", usage)
	}

	cleared, err := svc.ClearData(ctx, "", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.ClearedSessions) != 1 || cleared.FreedBytes != 4096 || len(host.clears) != 1 {
		t.Fatalf("unexpected clear: %+v", cleared)
	}

	// snapshot row removed by clear
	usage, err = svc.DataUsage(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 1 || usage[0].ProfileBytes != 1000 {
		t.Fatalf("usage should re-snapshot after clear: %+v", usage)
	}

	if _, err := svc.DataUsage(ctx, "br-ghost"); !errors.Is(err, brapp.ErrBrNotFound) {
		t.Fatalf("expected ErrBrNotFound, got %v", err)
	}
}

func TestBrPermissionWorkflow(t *testing.T) {
	ctx := context.Background()
	svc, _ := newBrService(t)

	// ask flow: pending → decide
	pending, err := svc.RequestPermission(ctx, "https://maps.example.com", brapp.PermGeolocation, "", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if pending.State != brapp.PermStatePending || pending.Policy != brapp.PolicyAsk {
		t.Fatalf("unexpected pending: %+v", pending)
	}
	granted, err := svc.DecidePermission(ctx, pending.PermissionID, "grant", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if granted.State != brapp.PermStateGranted || granted.DecidedAt == "" {
		t.Fatalf("unexpected grant: %+v", granted)
	}
	// already decided → state error
	if _, err := svc.DecidePermission(ctx, pending.PermissionID, "deny", "tester"); !errors.Is(err, brapp.ErrBrState) {
		t.Fatalf("expected ErrBrState, got %v", err)
	}

	// policy allow: auto-grant on request
	policy := brapp.PolicyAllow
	if _, err := svc.SetPermissionPolicy(ctx, "https://meet.example.com", brapp.PermCamera, policy, "tester"); err != nil {
		t.Fatal(err)
	}
	auto, err := svc.RequestPermission(ctx, "https://meet.example.com", brapp.PermCamera, "", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if auto.State != brapp.PermStateGranted {
		t.Fatalf("policy allow should auto-grant: %+v", auto)
	}

	// pending row resolved by a policy swap to deny
	askRow, err := svc.RequestPermission(ctx, "https://notify.example.com", brapp.PermNotifications, "", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if askRow.State != brapp.PermStatePending {
		t.Fatalf("expected pending: %+v", askRow)
	}
	deny := brapp.PolicyDeny
	if _, err := svc.SetPermissionPolicy(ctx, "https://notify.example.com", brapp.PermNotifications, deny, "tester"); err != nil {
		t.Fatal(err)
	}
	list, err := svc.ListPermissions(ctx, brapp.PermStatePending)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.PermissionID == askRow.PermissionID {
			t.Fatalf("pending row should have been resolved by policy swap")
		}
	}

	// invalid inputs
	if _, err := svc.RequestPermission(ctx, "https://x.example.com", "screen-capture", "", "tester"); !errors.Is(err, brapp.ErrBrSchema) {
		t.Fatalf("expected ErrBrSchema, got %v", err)
	}
	if _, err := svc.DecidePermission(ctx, askRow.PermissionID, "maybe", "tester"); !errors.Is(err, brapp.ErrBrSchema) {
		t.Fatalf("expected ErrBrSchema, got %v", err)
	}
}

func TestBrRateLimitLifecycle(t *testing.T) {
	ctx := context.Background()
	svc, _ := newBrService(t)
	for i := 0; i < brapp.BrLifecycleRatePerMinute; i++ {
		id := "br-rl-" + string(rune('a'+i))
		if _, err := svc.Connect(ctx, id, brapp.ModeBuiltin, "tester"); err != nil {
			t.Fatalf("connect %d failed: %v", i, err)
		}
	}
	if _, err := svc.Connect(ctx, "br-rl-overflow", brapp.ModeBuiltin, "tester"); !errors.Is(err, brapp.ErrBrRateLimited) {
		t.Fatalf("expected ErrBrRateLimited, got %v", err)
	}
}
