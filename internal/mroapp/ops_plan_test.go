package mroapp

import "testing"

func TestAssembleWorkPackageFourSources(t *testing.T) {
	got := AssembleWorkPackage([]string{"card"}, []string{"ad"}, []string{"mel"}, []string{"open"})
	if len(got.Sources) != 4 {
		t.Fatalf("sources = %#v", got.Sources)
	}
}

func TestProposeIntervalChangeRequiresDualCite(t *testing.T) {
	if ok, reason := ProposeIntervalChange("", "fleet"); ok || reason == "" {
		t.Fatalf("missing mpd: %v %q", ok, reason)
	}
	if ok, reason := ProposeIntervalChange("MPD 05-20", ""); ok || reason == "" {
		t.Fatalf("missing fleet: %v %q", ok, reason)
	}
	if ok, _ := ProposeIntervalChange("MPD 05-20", "本队 FH 日志"); !ok {
		t.Fatal("dual cite should pass")
	}
}

func TestCheckScheduleConstraintsC1ToC7(t *testing.T) {
	v := CheckScheduleConstraints(ScheduleInput{
		Assignments: []ScheduleAssignment{
			{TailNo: "B-1", Start: "2026-09-01", End: "2026-09-10", Hours: 20, Skill: "avionic"},
			{TailNo: "B-1", Start: "2026-09-05", End: "2026-09-12", Hours: 5, Skill: "avionic"},
		},
		Slots:      []CapacitySlot{{Skill: "avionic", Hours: 10}},
		Dues:       []DueStatus{{State: DueStateOverdue}},
		AOGTails:   []string{"B-1"},
		KitMissing: []string{"SEAL"},
		LongLeadPN: []string{"LLP-1"},
		HasCite:    false,
	})
	seen := map[string]bool{}
	for _, item := range v {
		seen[item.Code] = true
	}
	for _, code := range []string{"C1", "C2", "C3", "C4", "C5", "C6", "C7"} {
		if !seen[code] {
			t.Fatalf("missing %s in %+v", code, v)
		}
	}
	if n := CheckScheduleConstraints(ScheduleInput{HasCite: true}); len(n) != 0 {
		t.Fatalf("clean schedule = %+v", n)
	}
}
