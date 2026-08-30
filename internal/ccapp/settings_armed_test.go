package ccapp

import (
	"testing"
	"time"
)

func TestArmExpired(t *testing.T) {
	now, _ := time.Parse(time.RFC3339, "2026-08-30T12:00:00Z")
	if armExpired(now, "") {
		t.Fatal("empty arm is permanent")
	}
	if armExpired(now, "2026-08-30T12:30:00Z") {
		t.Fatal("future arm must still be active")
	}
	if !armExpired(now, "2026-08-30T11:59:00Z") {
		t.Fatal("past arm must expire")
	}
}

func TestNextArmedUntil(t *testing.T) {
	now, _ := time.Parse(time.RFC3339, "2026-08-30T12:00:00Z")
	cur := Settings{ArmedUntil: "keep"}
	off := false
	on := true
	thirty := 30
	zero := 0
	if got := nextArmedUntil(now, SettingsPatch{Enabled: &off}, cur); got != "" {
		t.Fatalf("disable must clear arm: %q", got)
	}
	if got := nextArmedUntil(now, SettingsPatch{Enabled: &on, ArmMinutes: &thirty}, cur); got != "2026-08-30T12:30:00Z" {
		t.Fatalf("30m arm: %q", got)
	}
	if got := nextArmedUntil(now, SettingsPatch{Enabled: &on, ArmMinutes: &zero}, cur); got != "" {
		t.Fatalf("0 minutes is permanent: %q", got)
	}
	if got := nextArmedUntil(now, SettingsPatch{Enabled: &on}, cur); got != "" {
		t.Fatalf("enable without armMinutes is permanent: %q", got)
	}
	if got := nextArmedUntil(now, SettingsPatch{}, cur); got != "keep" {
		t.Fatalf("unrelated patch keeps arm: %q", got)
	}
}
