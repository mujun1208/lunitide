package scheduler

import "testing"
import "time"

func TestParseAtSchedule(t *testing.T) {
	if err := ParseSchedule("at:2026-08-27T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if !IsAtSchedule("at:2026-08-27T12:00:00+08:00") {
		t.Fatal("expected at schedule")
	}
	if err := ParseSchedule("每天"); err == nil {
		t.Fatal("bad cron accepted")
	}
}

func TestNextFireTimeAtIsTheStamp(t *testing.T) {
	stamp := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	got, err := nextFireTime("at:2026-08-27T12:00:00Z", stamp.Add(-time.Hour))
	if err != nil || !got.Equal(stamp) {
		t.Fatalf("got %v %v", got, err)
	}
	past, err := nextFireTime("at:2026-08-27T12:00:00Z", stamp.Add(time.Hour))
	if err != nil || !past.Equal(stamp) {
		t.Fatalf("past at: %v %v", past, err)
	}
}

func TestValidateJobAcceptsIsolatedAndAt(t *testing.T) {
	job := validJob("once", "at:2026-08-27T12:00:00Z")
	job.SessionMode = "isolated"
	job.RunOnce = true
	if err := ValidateJob(job); err != nil {
		t.Fatal(err)
	}
}
