package policy

import (
	"errors"
	"strings"
	"testing"
)

func platformNode() *Node {
	n, err := Attach(nil, Node{
		ID: "platform-root", Level: LevelPlatform, Version: 1,
		Constraints: Constraints{
			"model.allowlist": Allowlist("gpt-5", "claude-5", "glm-5"),
			"spend.ceiling":   Ceiling(1000),
			"retention.floor": Floor(30),
			"region.deny":     Denylist("eu"),
			"control.required": Required("policy.review"),
			"audit.flag":      Flag(true),
		},
	})
	if err != nil {
		panic(err)
	}
	return n
}

func mustAttach(t *testing.T, parent *Node, child Node) *Node {
	t.Helper()
	n, err := Attach(parent, child)
	if err != nil {
		t.Fatalf("Attach(%s) failed: %v", child.ID, err)
	}
	return n
}

func fullChain(t *testing.T) (*Node, *Node, *Node, *Node) {
	t.Helper()
	p := platformNode()
	o := mustAttach(t, p, Node{ID: "org-1", Level: LevelOrganization, OrgID: "01JDPOLICYORG00000001", Version: 4, ExpectedParentVer: 1,
		Constraints: Constraints{
			"model.allowlist": Allowlist("gpt-5", "glm-5"),
			"spend.ceiling":   Ceiling(400),
			"region.deny":     Denylist("eu", "us"),
		}})
	s := mustAttach(t, o, Node{ID: "space-1", Level: LevelTeamSpace, OrgID: "01JDPOLICYORG00000001", Version: 2, ExpectedParentVer: 4,
		Constraints: Constraints{
			"spend.ceiling": Ceiling(200),
			"control.required": Required("policy.review", "sod.check"),
		}})
	prj := mustAttach(t, s, Node{ID: "proj-1", Level: LevelProject, OrgID: "01JDPOLICYORG00000001", Version: 7, ExpectedParentVer: 2,
		Constraints: Constraints{
			"spend.ceiling": Ceiling(150),
			"model.allowlist": Allowlist("glm-5"),
		}})
	return p, o, s, prj
}

func TestMergeTightenOnly(t *testing.T) {
	t.Run("four-level chain merges to the meet of every layer", func(t *testing.T) {
		_, _, _, prj := fullChain(t)
		eff := prj.Effective()
		if got := eff["model.allowlist"].Set; len(got) != 1 || got[0] != "glm-5" {
			t.Fatalf("allowlist must narrow to [glm-5], got %v", got)
		}
		if got := eff["spend.ceiling"].Number; got != 150 {
			t.Fatalf("ceiling must take the minimum, got %v", got)
		}
		if got := eff["retention.floor"].Number; got != 30 {
			t.Fatalf("floor must survive from platform, got %v", got)
		}
		if got := eff["region.deny"].Set; len(got) != 2 {
			t.Fatalf("deny must union to [eu us], got %v", got)
		}
		if got := eff["control.required"].Set; len(got) != 2 {
			t.Fatalf("required must union to [policy.review sod.check], got %v", got)
		}
		if !eff["audit.flag"].Bool {
			t.Fatal("flag must stay true (platform AND)")
		}
	})

	t.Run("meet is never wider than any single layer (merge proof)", func(t *testing.T) {
		p, o, s, prj := fullChain(t)
		eff := prj.Effective()
		for name, layer := range map[string]Constraints{
			"platform": p.Constraints, "organization": o.Constraints,
			"teamspace": s.Constraints, "project": prj.Constraints,
		} {
			for k, lv := range layer {
				ev, ok := eff[k]
				if !ok {
					t.Fatalf("effective set lost dimension %q from %s", k, name)
				}
				if relate(lv, ev) == relationRelaxed {
					t.Fatalf("effective %q is wider than %s layer", k, name)
				}
			}
		}
	})

	t.Run("relaxing any dimension is rejected and names the dimension", func(t *testing.T) {
		cases := []struct {
			name   string
			child  Constraints
			wantOn string
		}{
			{"allowlist adds unlisted model", Constraints{"model.allowlist": Allowlist("gpt-5", "claude-5", "glm-5", "llama-4")}, "model.allowlist"},
			{"ceiling raised", Constraints{"spend.ceiling": Ceiling(2000)}, "spend.ceiling"},
			{"floor lowered", Constraints{"retention.floor": Floor(7)}, "retention.floor"},
			{"required drops parent entry", Constraints{"control.required": Required("sod.check")}, "control.required"},
			{"deny drops parent entry", Constraints{"region.deny": Denylist("us")}, "region.deny"},
			{"flag switched off", Constraints{"audit.flag": Flag(false)}, "audit.flag"},
			{"cross-kind reuse of a dimension", Constraints{"spend.ceiling": Allowlist("x")}, "spend.ceiling"},
		}
		parent := platformNode()
		for _, tc := range cases {
			_, err := Attach(parent, Node{ID: "org-bad", Level: LevelOrganization, Version: 1, ExpectedParentVer: 1, Constraints: tc.child})
			if !errors.Is(err, ErrRelaxationDenied) {
				t.Fatalf("%s: want M9-009, got %v", tc.name, err)
			}
			if Code(err) != "M9-009" {
				t.Fatalf("%s: want code M9-009, got %s", tc.name, Code(err))
			}
			if !strings.Contains(err.Error(), tc.wantOn) {
				t.Fatalf("%s: error must locate dimension %q, got %v", tc.name, tc.wantOn, err)
			}
		}
	})

	t.Run("parent missing fails closed with M9-008", func(t *testing.T) {
		if _, err := Attach(nil, Node{ID: "orphan", Level: LevelOrganization, Version: 1, Constraints: Constraints{}}); !errors.Is(err, ErrParentMissing) {
			t.Fatalf("want M9-008, got %v", err)
		}
		p := platformNode()
		if _, err := Attach(p, Node{ID: "skip", Level: LevelProject, Version: 1, ExpectedParentVer: 1, Constraints: Constraints{}}); !errors.Is(err, ErrParentMissing) {
			t.Fatalf("level jump must fail with M9-008, got %v", err)
		}
	})

	t.Run("parent republish races the child draft (T-06)", func(t *testing.T) {
		p := platformNode()
		if _, err := Attach(p, Node{ID: "org-stale", Level: LevelOrganization, Version: 1, ExpectedParentVer: 0, Constraints: Constraints{}}); !errors.Is(err, ErrVersionStale) {
			t.Fatalf("want M9-010 for stale parent version, got %v", err)
		}
	})

	t.Run("digest pins the verdict and blocks replay after change", func(t *testing.T) {
		_, _, _, prj := fullChain(t)
		dec := Decide(prj)
		if err := dec.ReplayAgainst(prj.Digest()); err != nil {
			t.Fatalf("same digest must replay, got %v", err)
		}
		if err := dec.ReplayAgainst("0000"); !errors.Is(err, ErrVersionStale) {
			t.Fatalf("moved digest must refuse replay with M9-010, got %v", err)
		}
		// Re-attach a stricter sibling: any accepted change moves the digest.
		s2, err := Attach(prj.parent, Node{ID: "proj-2", Level: LevelProject, OrgID: "01JDPOLICYORG00000001", Version: 8, ExpectedParentVer: 2,
			Constraints: Constraints{"spend.ceiling": Ceiling(100)}})
		if err != nil {
			t.Fatalf("stricter sibling must attach: %v", err)
		}
		if s2.Digest() == prj.Digest() {
			t.Fatal("different effective sets must produce different digests")
		}
		if err := Decide(prj).ReplayAgainst(s2.Digest()); !errors.Is(err, ErrVersionStale) {
			t.Fatal("old verdict must not replay against the new policy digest")
		}
	})

	t.Run("digest is canonical: set order and key order do not matter", func(t *testing.T) {
		a := Constraints{"x.deny": Denylist("b", "a", "c"), "m.ceiling": Ceiling(9)}
		b := Constraints{"m.ceiling": Ceiling(9), "x.deny": Denylist("c", "b", "a")}
		if Digest(a) != Digest(b) {
			t.Fatal("same constraints in different order must share one digest")
		}
	})

	t.Run("chain versions record every contributing layer", func(t *testing.T) {
		_, _, _, prj := fullChain(t)
		chain := prj.ChainVersions()
		if len(chain) != 4 {
			t.Fatalf("want 4 layers, got %v", chain)
		}
		if !strings.HasPrefix(chain[0], "platform:platform-root@v1") || !strings.HasSuffix(chain[3], "project:proj-1@v7") {
			t.Fatalf("chain order broken: %v", chain)
		}
	})

	t.Run("simulator preflights tighten vs relax per dimension", func(t *testing.T) {
		before := Constraints{"model.allowlist": Allowlist("a", "b"), "spend.ceiling": Ceiling(100), "old.flag": Flag(true)}
		after := Constraints{"model.allowlist": Allowlist("a"), "spend.ceiling": Ceiling(200), "new.deny": Denylist("x")}
		rows := Simulate(before, after)
		byKey := make(map[string]DimChange, len(rows))
		for _, r := range rows {
			byKey[r.Key] = r
		}
		if byKey["model.allowlist"].Direction != DirectionTightened {
			t.Fatalf("narrower allowlist must read tightened, got %s", byKey["model.allowlist"].Direction)
		}
		if byKey["spend.ceiling"].Direction != DirectionRelaxed {
			t.Fatalf("higher ceiling must read relaxed, got %s", byKey["spend.ceiling"].Direction)
		}
		if byKey["new.deny"].Direction != DirectionAdded || byKey["old.flag"].Direction != DirectionRemoved {
			t.Fatalf("added/removed detection broken: %+v", byKey)
		}
		// The relaxed row predicts the Tighten refusal.
		if _, err := Tighten(before, after); !errors.Is(err, ErrRelaxationDenied) || !strings.Contains(err.Error(), "spend.ceiling") {
			t.Fatalf("simulator relaxed row must match Tighten refusal, got %v", err)
		}
	})

	t.Run("equal constraints re-attach unchanged", func(t *testing.T) {
		p := platformNode()
		same, err := Attach(p, Node{ID: "org-same", Level: LevelOrganization, Version: 2, ExpectedParentVer: 1,
			Constraints: Constraints{"spend.ceiling": Ceiling(1000)}})
		if err != nil {
			t.Fatalf("equal dimension must attach: %v", err)
		}
		if same.Effective()["spend.ceiling"].Number != 1000 {
			t.Fatal("equal merge must keep the parent value")
		}
		if same.Digest() != p.Digest() {
			t.Fatal("identical effective sets must share the platform digest")
		}
	})
}
