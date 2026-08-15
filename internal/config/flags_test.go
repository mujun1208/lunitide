package config

import (
	"crypto/sha256"
	"errors"
	"strconv"
	"sync"
	"testing"
)

// allLevels lists the five frozen gauge levels.
func allLevels() []Level {
	return []Level{LevelInternal, LevelCanary1, LevelCanary10, LevelCanary50, LevelGA100}
}

// TestFlagKillSwitch covers the kill-switch semantics: latched on, new
// actions are refused at every level while reads/exports stay open;
// off, every valid level admits new actions; invalid levels are rejected.
func TestFlagKillSwitch(t *testing.T) {
	f := NewFlags(LevelCanary10)
	if f.KillSwitchEnabled() {
		t.Fatal("kill switch must start off")
	}

	f.EnableKillSwitch()
	if !f.KillSwitchEnabled() {
		t.Fatal("kill switch must report enabled after EnableKillSwitch")
	}
	for _, l := range allLevels() {
		if err := f.SetM5(l); err != nil {
			t.Fatalf("SetM5(%s): %v", l, err)
		}
		if f.AllowNewAction() {
			t.Fatalf("kill switch 开启后 %s 档仍允许新动作", l)
		}
		if !f.AllowReadExport() {
			t.Fatalf("kill switch 不得影响存量产物的读/导出（%s 档）", l)
		}
	}

	// The latch is one-way: repeated trips keep it enabled.
	f.EnableKillSwitch()
	if !f.KillSwitchEnabled() {
		t.Fatal("kill switch 是单向闩锁，不允许被复位")
	}

	g := NewFlags(LevelInternal)
	for _, l := range allLevels() {
		if err := g.SetM5(l); err != nil {
			t.Fatalf("SetM5(%s): %v", l, err)
		}
		if !g.AllowNewAction() {
			t.Fatalf("kill switch 关闭时 %s 档应允许新动作", l)
		}
		if !g.AllowReadExport() {
			t.Fatalf("AllowReadExport 应恒为 true")
		}
	}

	if err := g.SetM5(Level("bogus")); !errors.Is(err, ErrLevelInvalid) {
		t.Fatalf("SetM5 非法值 err = %v, want ErrLevelInvalid", err)
	}
	if g.M5() != LevelGA100 {
		t.Fatalf("非法 SetM5 不得改动档位: %s", g.M5())
	}

	// NewFlags coerces an invalid default to the most conservative gauge.
	h := NewFlags(Level("nope"))
	if h.M5() != LevelInternal {
		t.Fatalf("NewFlags(非法默认) 应收敛为 internal, got %s", h.M5())
	}
	if !h.AllowNewAction() {
		t.Fatal("coerced flags must still allow new actions (internal is a live gauge)")
	}
}

// TestFlagLevels covers the five-level Evaluate math with known userIDs:
// deterministic hash membership, exact threshold boundaries, ga100 always
// true and internal whitelist-only.
func TestFlagLevels(t *testing.T) {
	const salt = "m5-t-5.5.4"
	hashByte := func(userID string) byte {
		sum := sha256.Sum256([]byte(userID + "|" + salt))
		return sum[0]
	}

	f := NewFlags(LevelInternal)

	// ga100: everyone.
	if err := f.SetM5(LevelGA100); err != nil {
		t.Fatal(err)
	}
	for _, uid := range []string{"u1", "u2", "u3"} {
		if !f.Evaluate(uid, salt) {
			t.Fatalf("ga100 应 100%% 命中: %s", uid)
		}
	}

	// internal: whitelist only, and nobody without a checker.
	f.SetInternalChecker(func(userID string) bool { return userID == "staff-1" })
	if err := f.SetM5(LevelInternal); err != nil {
		t.Fatal(err)
	}
	if !f.Evaluate("staff-1", salt) {
		t.Fatal("白名单用户应命中 internal 档")
	}
	if f.Evaluate("staff-2", salt) {
		t.Fatal("非白名单用户不得命中 internal 档")
	}
	bare := NewFlags(LevelInternal)
	if bare.Evaluate("staff-1", salt) {
		t.Fatal("未注入白名单时 internal 档应恒 false")
	}

	// Percentage cohorts: membership equals the frozen byte math.
	for _, uid := range []string{"alpha", "beta", "gamma", "delta", "epsilon"} {
		b := hashByte(uid)
		cases := []struct {
			level Level
			want  bool
		}{
			{LevelCanary1, b < 3},
			{LevelCanary10, b < 26},
			{LevelCanary50, b < 128},
		}
		for _, tc := range cases {
			if err := f.SetM5(tc.level); err != nil {
				t.Fatal(err)
			}
			if got := f.Evaluate(uid, salt); got != tc.want {
				t.Fatalf("Evaluate(%s@%s) = %v, want %v (hash byte %d)", uid, tc.level, got, tc.want, b)
			}
		}
	}

	// Stability: the same user lands on the same side on every call.
	if err := f.SetM5(LevelCanary10); err != nil {
		t.Fatal(err)
	}
	first := f.Evaluate("stable-user", salt)
	for i := 0; i < 10; i++ {
		if f.Evaluate("stable-user", salt) != first {
			t.Fatal("同一 (userID, salt) 的求值结果必须稳定")
		}
	}

	// Boundaries: a user whose hash byte equals a threshold is excluded
	// below it and included above it (byte < threshold comparisons).
	edge3 := findEdgeUser(t, salt, 3)
	edge26 := findEdgeUser(t, salt, 26)
	edge128 := findEdgeUser(t, salt, 128)

	f.SetM5(LevelCanary1)
	if f.Evaluate(edge3, salt) {
		t.Fatalf("首字节=3 的用户不应命中 1%% 档（<3）: %s", edge3)
	}
	f.SetM5(LevelCanary10)
	if !f.Evaluate(edge3, salt) {
		t.Fatalf("首字节=3 的用户应命中 10%% 档（<26）: %s", edge3)
	}
	if f.Evaluate(edge26, salt) {
		t.Fatalf("首字节=26 的用户不应命中 10%% 档（<26）: %s", edge26)
	}
	f.SetM5(LevelCanary50)
	if !f.Evaluate(edge26, salt) {
		t.Fatalf("首字节=26 的用户应命中 50%% 档（<128）: %s", edge26)
	}
	if f.Evaluate(edge128, salt) {
		t.Fatalf("首字节=128 的用户不应命中 50%% 档（<128）: %s", edge128)
	}
	f.SetM5(LevelGA100)
	if !f.Evaluate(edge128, salt) {
		t.Fatal("ga100 档必须命中所有用户")
	}
}

// findEdgeUser deterministically searches a userID whose cohort hash
// first byte equals want (a byte value occurs within a few hundred tries
// at 1/256 odds; 100k tries make failure statistically impossible).
func findEdgeUser(t *testing.T, salt string, want byte) string {
	t.Helper()
	for i := 0; i < 100000; i++ {
		uid := "edge-" + strconv.Itoa(i)
		sum := sha256.Sum256([]byte(uid + "|" + salt))
		if sum[0] == want {
			return uid
		}
	}
	t.Fatalf("100000 个 userID 内未找到首字节=%d 的用户", want)
	return ""
}

// TestFlagsConcurrent hammers the flag set from concurrent goroutines
// mixing writes (SetM5, EnableKillSwitch, SetInternalChecker) with reads
// (M5, KillSwitchEnabled, AllowNewAction, AllowReadExport, Evaluate).
// Run with -race where available; on Windows without cgo -race, mutex
// correctness plus -count=3 replays stand in.
func TestFlagsConcurrent(t *testing.T) {
	f := NewFlags(LevelCanary1)
	levels := allLevels()
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				switch (w + j) % 5 {
				case 0:
					_ = f.SetM5(levels[j%len(levels)])
				case 1:
					f.EnableKillSwitch()
				case 2:
					f.SetInternalChecker(func(userID string) bool { return userID == "staff" })
				case 3:
					_ = f.M5()
					_ = f.KillSwitchEnabled()
					_ = f.AllowNewAction()
					_ = f.AllowReadExport()
				case 4:
					_ = f.Evaluate("user-"+strconv.Itoa(j), "salt")
					_ = f.IsInternal("staff")
				}
			}
		}(w)
	}
	wg.Wait()
	if !f.KillSwitchEnabled() {
		t.Fatal("并发期间发生过 EnableKillSwitch，结束后开关必须为开")
	}
}
