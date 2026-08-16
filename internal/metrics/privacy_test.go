package metrics

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

var day0 = time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
var day1 = day0.Add(24 * time.Hour)
var day2 = day1.Add(24 * time.Hour)

func sample(group, subject string, v float64) Sample {
	return Sample{GroupKey: group, Subject: subject, Value: v}
}

func TestPrivacyThreshold(t *testing.T) {
	t.Run("T-20: groups below the frozen k suppress with zero leakage (M9-030)", func(t *testing.T) {
		e := NewEngine(FrozenK)
		e.SetRollout("01JDORG", true)
		samples := []Sample{
			// 4 distinct subjects → suppressed
			sample("scope=aml", "s1", 10), sample("scope=aml", "s2", 10),
			sample("scope=aml", "s3", 10), sample("scope=aml", "s4", 10),
			// 5 distinct subjects (one repeats — dedup) → released
			sample("scope=tts", "s1", 4), sample("scope=tts", "s2", 4),
			sample("scope=tts", "s3", 4), sample("scope=tts", "s4", 4),
			sample("scope=tts", "s5", 4), sample("scope=tts", "s5", 6),
		}
		out, err := e.Aggregate("01JDORG", "ops", day0, day1, samples, day1)
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != 2 {
			t.Fatalf("want 2 groups, got %d", len(out))
		}
		var aml, tts Aggregate
		for _, a := range out {
			if a.GroupKey == "scope=aml" {
				aml = a
			} else {
				tts = a
			}
		}
		if !aml.Suppressed || aml.Subjects != 0 || aml.Sum != 0 || aml.Mean != 0 {
			t.Fatalf("low-sample group must leak nothing: %+v", aml)
		}
		if tts.Suppressed || tts.Subjects != FrozenK {
			t.Fatalf("k-th group must release: %+v", tts)
		}
	})

	t.Run("subject dedup: one subject cannot lift a group to k alone", func(t *testing.T) {
		e := NewEngine(FrozenK)
		e.SetRollout("01JDORG", true)
		var samples []Sample
		for i := 0; i < 40; i++ { // 40 observations, 1 distinct subject
			samples = append(samples, sample("scope=solo", "only-one", float64(i)))
		}
		out, err := e.Aggregate("01JDORG", "ops", day0, day1, samples, day1)
		if err != nil {
			t.Fatal(err)
		}
		if !out[0].Suppressed {
			t.Fatalf("40 observations from 1 subject must still suppress: %+v", out[0])
		}
	})

	t.Run("M9-030: direct low-sample single-group query refuses", func(t *testing.T) {
		e := NewEngine(FrozenK)
		e.SetRollout("01JDORG", true)
		_, err := e.Aggregate("01JDORG", "ops", day0, day1, []Sample{sample("scope=x", "s1", 1)}, day1)
		if err != nil {
			t.Fatal(err) // suppressed output, not an error — caller shows 样本不足
		}
		out, _ := e.Aggregate("01JDORG", "ops", day1, day2, []Sample{sample("scope=y", "s1", 1)}, day2)
		if !out[0].Suppressed {
			t.Fatalf("single-subject group must be suppressed: %+v", out[0])
		}
		// errors.Is still works for the taxonomy when drill-down refuses
		_, err = e.Aggregate("01JDORG", "ops", day0, day1, []Sample{sample("prompt:p-1", "s1", 1)}, day1)
		if !errors.Is(err, ErrPrivacyThreshold) || M9Code(err) != "M9-030" {
			t.Fatalf("drill-down: want M9-030, got %v", err)
		}
	})

	t.Run("forbidden drill-down dimensions refuse (prompt/file/user/session)", func(t *testing.T) {
		e := NewEngine(FrozenK)
		e.SetRollout("01JDORG", true)
		for _, key := range []string{
			"prompt:p-1", "file:report.doc", "user:alice", "session:01JDX",
			"scope=aml/user:alice", "file:x,scope=aml",
		} {
			_, err := e.Aggregate("01JDORG", "ops", day0, day1, []Sample{sample(key, "s1", 1)}, day1)
			if !errors.Is(err, ErrPrivacyThreshold) || M9Code(err) != "M9-030" {
				t.Fatalf("group %q: want M9-030, got %v", key, err)
			}
		}
		// frozen dimensions stay queryable
		if _, err := e.Aggregate("01JDORG", "ops", day0, day1, []Sample{sample("scope=aml/runner-tier=local", "s1", 1)}, day1); err != nil {
			t.Fatalf("frozen dimensions must pass: %v", err)
		}
	})

	t.Run("progressive rollout: default off, per-org switch is effective", func(t *testing.T) {
		e := NewEngine(FrozenK)
		var samples []Sample
		for i := 1; i <= FrozenK; i++ {
			samples = append(samples, sample("scope=on", fmt.Sprintf("s%d", i), 1))
		}
		if _, err := e.Aggregate("01JDORG", "ops", day0, day1, samples, day1); err == nil {
			t.Fatal("org without rollout must be refused")
		}
		e.SetRollout("01JDORG", true)
		if _, err := e.Aggregate("01JDORG", "ops", day0, day1, samples, day1); err != nil {
			t.Fatalf("enabled org must aggregate: %v", err)
		}
		// another org stays off even after the first opted in
		if _, err := e.Aggregate("01JDOTHER", "ops", day0, day1, samples, day1); err == nil {
			t.Fatal("rollout must be per-org")
		}
	})

	t.Run("window rules: 24h minimum and natural-day alignment", func(t *testing.T) {
		e := NewEngine(FrozenK)
		e.SetRollout("01JDORG", true)
		var samples []Sample
		for i := 1; i <= FrozenK; i++ {
			samples = append(samples, sample("scope=w", fmt.Sprintf("s%d", i), 1))
		}
		if _, err := e.Aggregate("01JDORG", "ops", day0, day0.Add(12*time.Hour), samples, day1); err == nil {
			t.Fatal("12h window must refuse")
		}
		off := day0.Add(time.Hour)
		if _, err := e.Aggregate("01JDORG", "ops", off, off.Add(24*time.Hour), samples, day1); err == nil {
			t.Fatal("day-misaligned window must refuse")
		}
		if _, err := e.Aggregate("01JDORG", "ops", day1, day0, samples, day1); err == nil {
			t.Fatal("inverted window must refuse")
		}
	})

	t.Run("cross-window differential control: overlapping windows refuse (M9-030)", func(t *testing.T) {
		e := NewEngine(FrozenK)
		e.SetRollout("01JDORG", true)
		var samples []Sample
		for i := 1; i <= FrozenK; i++ {
			samples = append(samples, sample("scope=d", fmt.Sprintf("s%d", i), 1))
		}
		if _, err := e.Aggregate("01JDORG", "ops", day0, day2, samples, day2); err != nil {
			t.Fatal(err) // 48h window first
		}
		// overlapping 24h slice of the same group → differential attack refused
		_, err := e.Aggregate("01JDORG", "ops", day0, day1, samples, day1)
		if !errors.Is(err, ErrPrivacyThreshold) || M9Code(err) != "M9-030" {
			t.Fatalf("overlapping window: want M9-030, got %v", err)
		}
		// disjoint next window is fine
		day3 := day2.Add(24 * time.Hour)
		if _, err := e.Aggregate("01JDORG", "ops", day2, day3, samples, day3); err != nil {
			t.Fatalf("disjoint window must pass: %v", err)
		}
		// suppression state from earlier queries does not leak across orgs
		e.SetRollout("01JDOTHER", true)
		if _, err := e.Aggregate("01JDOTHER", "ops", day0, day1, samples, day1); err != nil {
			t.Fatalf("another org has independent window history: %v", err)
		}
	})

	t.Run("access review journal records every query", func(t *testing.T) {
		e := NewEngine(FrozenK)
		e.SetRollout("01JDORG", true)
		var samples []Sample
		for i := 1; i <= FrozenK; i++ {
			samples = append(samples, sample("scope=rev", fmt.Sprintf("s%d", i), 1))
		}
		if _, err := e.Aggregate("01JDORG", "auditor-a", day0, day1, samples, day1); err != nil {
			t.Fatal(err)
		}
		low := []Sample{sample("scope=low", "s1", 1)}
		if _, err := e.Aggregate("01JDORG", "auditor-b", day0, day1, low, day1); err != nil {
			t.Fatal(err)
		}
		reviews := e.Reviews()
		if len(reviews) != 2 {
			t.Fatalf("want 2 review entries, got %d", len(reviews))
		}
		if reviews[0].Viewer != "auditor-a" || reviews[0].Groups != 1 || reviews[0].Suppressed != 0 {
			t.Fatalf("entry 0: %+v", reviews[0])
		}
		if reviews[1].Viewer != "auditor-b" || reviews[1].Suppressed != 1 {
			t.Fatalf("entry 1 must count the suppressed group: %+v", reviews[1])
		}
	})
}
