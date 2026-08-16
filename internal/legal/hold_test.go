package legal

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

var now0 = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

func activeHold(id, org, scope string, ttl time.Duration) Hold {
	return Hold{ID: id, OrgID: org, Scope: scope, AuthorityRef: "case-2026-" + id, ExpiresAt: now0.Add(ttl)}
}

func TestHold(t *testing.T) {
	t.Run("activation without an authority reference refuses M9-029", func(t *testing.T) {
		r := NewRegistry()
		h := activeHold("h-1", "01JDORG", "user:alice", time.Hour)
		h.AuthorityRef = ""
		if _, err := r.Activate(h, "counsel", now0); !errors.Is(err, ErrHoldAuthorityNeeded) || M9Code(err) != "M9-029" {
			t.Fatalf("want M9-029, got %v", err)
		}
		noExpiry := activeHold("h-2", "01JDORG", "user:alice", time.Hour)
		noExpiry.ExpiresAt = time.Time{}
		if _, err := r.Activate(noExpiry, "counsel", now0); M9Code(err) != "M9-029" {
			t.Fatalf("no expiry: want M9-029, got %v", err)
		}
		past := activeHold("h-3", "01JDORG", "user:alice", time.Hour)
		past.ExpiresAt = now0.Add(-time.Minute)
		if _, err := r.Activate(past, "counsel", now0); M9Code(err) != "M9-029" {
			t.Fatalf("past expiry: want M9-029, got %v", err)
		}
		if len(r.Journal()) != 0 {
			t.Fatalf("refused activations must leave no journal noise: %v", r.Journal())
		}
	})

	t.Run("T-18: delete hitting an active hold diverts, never purges (M9-028, zero wrongful purges)", func(t *testing.T) {
		r := NewRegistry()
		if _, err := r.Activate(activeHold("h-1", "01JDORG", "user:alice", 24*time.Hour), "counsel", now0); err != nil {
			t.Fatal(err)
		}
		d, err := r.ScreenDelete("01JDORG", "user:alice", "msg-001", "alice", now0.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if !d.Redirected || d.HoldID != "h-1" {
			t.Fatalf("hit must divert to evidence: %+v", d)
		}
		// 用户侧隐藏 + 替代墓碑
		ts, ok := r.Tombstone("msg-001")
		if !ok || ts.HoldID != "h-1" || ts.Notice == "" {
			t.Fatalf("tombstone must replace the object in the user view: %+v ok=%v", ts, ok)
		}
		// 转受限证据库：证据可及且归属正确
		objs, err := r.AccessEvidence("h-1", "case-2026-h-1", "counsel", now0.Add(2*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if len(objs) != 1 || objs[0].ObjectID != "msg-001" || objs[0].OrgID != "01JDORG" {
			t.Fatalf("evidence store must hold the diverted object: %+v", objs)
		}
		// 重复筛选同一对象幂等：不重复入证据库
		if _, err := r.ScreenDelete("01JDORG", "user:alice", "msg-001", "alice", now0.Add(3*time.Minute)); err != nil {
			t.Fatal(err)
		}
		objs, _ = r.AccessEvidence("h-1", "case-2026-h-1", "counsel", now0.Add(4*time.Minute))
		if len(objs) != 1 {
			t.Fatalf("repeat screen must not duplicate evidence rows: %+v", objs)
		}
		// 激活/阻断/访问全审计
		kinds := map[string]bool{}
		for _, e := range r.Journal() {
			kinds[e.Kind] = true
		}
		if !kinds[EventActivate] || !kinds[EventScreenBlocked] || !kinds[EventAccess] {
			t.Fatalf("activate/screen-blocked/access must all be journaled: %v", kinds)
		}
	})

	t.Run("scope matching: exact, hierarchy and subject segments", func(t *testing.T) {
		r := NewRegistry()
		if _, err := r.Activate(activeHold("h-proj", "01JDORG", "project:p1", 24*time.Hour), "counsel", now0); err != nil {
			t.Fatal(err)
		}
		for _, scope := range []string{"project:p1", "project:p1/message:01JDX", "message:01JDX/project:p1"} {
			d, err := r.ScreenDelete("01JDORG", scope, "obj-"+scope, "alice", now0.Add(time.Minute))
			if err != nil || !d.Redirected {
				t.Fatalf("scope %q must hit: d=%+v err=%v", scope, d, err)
			}
		}
		d, err := r.ScreenDelete("01JDORG", "project:p2", "obj-other", "alice", now0.Add(time.Minute))
		if err != nil || d.Redirected {
			t.Fatalf("unrelated scope must clear: %+v err=%v", d, err)
		}
		// 其他组织的对象不受本组织保全影响
		d, err = r.ScreenDelete("01JDOTHER", "project:p1", "obj-foreign", "bob", now0.Add(time.Minute))
		if err != nil || d.Redirected {
			t.Fatalf("cross-org isolation: %+v err=%v", d, err)
		}
	})

	t.Run("release demands its own authority basis (M9-029) and unblocks deletes", func(t *testing.T) {
		r := NewRegistry()
		_, _ = r.Activate(activeHold("h-1", "01JDORG", "user:alice", 24*time.Hour), "counsel", now0)
		if err := r.Release("h-1", "", "counsel", now0.Add(time.Hour)); !errors.Is(err, ErrHoldAuthorityNeeded) || M9Code(err) != "M9-029" {
			t.Fatalf("release without basis: want M9-029, got %v", err)
		}
		if err := r.Release("h-1", "order-2026-release-9", "counsel", now0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		d, err := r.ScreenDelete("01JDORG", "user:alice", "msg-after", "alice", now0.Add(2*time.Hour))
		if err != nil || d.Redirected {
			t.Fatalf("released hold must stop blocking: %+v err=%v", d, err)
		}
		// 解除后证据封存：访问拒绝且不再增长
		if _, err := r.AccessEvidence("h-1", "case-2026-h-1", "counsel", now0.Add(2*time.Hour)); err == nil {
			t.Fatal("evidence of a released hold must be sealed")
		}
	})

	t.Run("expired hold stops blocking without any release ceremony", func(t *testing.T) {
		r := NewRegistry()
		_, _ = r.Activate(activeHold("h-short", "01JDORG", "user:bob", time.Minute), "counsel", now0)
		d, err := r.ScreenDelete("01JDORG", "user:bob", "msg-late", "bob", now0.Add(time.Hour))
		if err != nil || d.Redirected {
			t.Fatalf("expired hold must not block: %+v err=%v", d, err)
		}
	})

	t.Run("projection lag behind the authority registry fail-closes (M9-028)", func(t *testing.T) {
		r := NewRegistry()
		_, _ = r.Activate(activeHold("h-1", "01JDORG", "user:alice", 24*time.Hour), "counsel", now0)
		r.mu.Lock()
		delete(r.projection["01JDORG"], "h-1") // simulate a stale projection
		r.mu.Unlock()
		_, err := r.ScreenDelete("01JDORG", "user:alice", "msg-77", "alice", now0.Add(time.Minute))
		if !errors.Is(err, ErrHoldActive) || M9Code(err) != "M9-028" {
			t.Fatalf("stale projection must fail closed: want M9-028, got %v", err)
		}
	})

	t.Run("T-19: restore replays holds before reads open (no restoration-window leak)", func(t *testing.T) {
		r := NewRestoringRegistry()
		// 未重放前：门禁与激活都拒绝（先加载 Hold、策略、吊销水位，再开放读取）
		if _, err := r.ScreenDelete("01JDORG", "user:alice", "msg-r", "alice", now0); err == nil {
			t.Fatal("un-replayed registry must refuse screening")
		}
		if _, err := r.Activate(activeHold("h-new", "01JDORG", "user:alice", time.Hour), "counsel", now0); err == nil {
			t.Fatal("un-replayed registry must refuse activation")
		}
		// 备份早于保全激活：快照里带着 active hold 重放
		snap := []Hold{activeHold("h-backup", "01JDORG", "user:alice", 48*time.Hour)}
		if err := r.ReplaySnapshot(snap, now0); err != nil {
			t.Fatal(err)
		}
		d, err := r.ScreenDelete("01JDORG", "user:alice", "msg-restored", "alice", now0.Add(time.Minute))
		if err != nil || !d.Redirected || d.HoldID != "h-backup" {
			t.Fatalf("restored hold must block immediately after replay: %+v err=%v", d, err)
		}
		// 已开放的 registry 拒绝二次重放
		if err := r.ReplaySnapshot(nil, now0); err == nil {
			t.Fatal("double replay must be refused")
		}
	})

	t.Run("evidence access and export are authority-gated and journaled (M9-029)", func(t *testing.T) {
		r := NewRegistry()
		_, _ = r.Activate(activeHold("h-1", "01JDORG", "user:alice", 24*time.Hour), "counsel", now0)
		_, _ = r.ScreenDelete("01JDORG", "user:alice", "msg-9", "alice", now0.Add(time.Minute))
		if _, err := r.AccessEvidence("h-1", "", "counsel", now0); !errors.Is(err, ErrHoldAuthorityNeeded) || M9Code(err) != "M9-029" {
			t.Fatalf("access without basis: want M9-029, got %v", err)
		}
		if _, err := r.ExportEvidence("h-1", "", "counsel", now0); M9Code(err) != "M9-029" {
			t.Fatalf("export without basis: want M9-029, got %v", err)
		}
		objs, err := r.ExportEvidence("h-1", "case-2026-h-1", "counsel", now0.Add(2*time.Minute))
		if err != nil || len(objs) != 1 {
			t.Fatalf("authorized export must succeed: %+v err=%v", objs, err)
		}
		kinds := map[string]bool{}
		for _, e := range r.Journal() {
			kinds[e.Kind] = true
		}
		if !kinds[EventAccess] || !kinds[EventExport] {
			t.Fatalf("access/export must be journaled: %v", kinds)
		}
	})

	t.Run("concurrent screening keeps exactly one evidence row per object", func(t *testing.T) {
		r := NewRegistry()
		_, _ = r.Activate(activeHold("h-cc", "01JDORG", "user:alice", 24*time.Hour), "counsel", now0)
		const workers = 24
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				d, err := r.ScreenDelete("01JDORG", "user:alice", "msg-cc", "alice", now0.Add(time.Duration(i)*time.Second))
				if err != nil || !d.Redirected {
					t.Errorf("worker %d: %+v err=%v", i, d, err)
				}
			}(i)
		}
		wg.Wait()
		objs, err := r.AccessEvidence("h-cc", "case-2026-h-cc", "counsel", now0.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if len(objs) != 1 {
			t.Fatalf("exactly one evidence row expected, got %d", len(objs))
		}
		ts, ok := r.Tombstone("msg-cc")
		if !ok || fmt.Sprint(ts.CreatedAt.IsZero()) == "true" {
			t.Fatalf("tombstone must exist: %+v", ts)
		}
	})
}
