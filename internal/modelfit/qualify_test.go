package modelfit

import "testing"

func TestClassifiedRecoverOnlyAfterSQLiteAndAllowedDecision(t *testing.T) {
	if ClassifiedRecoverAllowed("json", RecoverNativeContinue) {
		t.Fatal("JSON writer must never recover")
	}
	if !ClassifiedRecoverAllowed("sqlite", RecoverNativeContinue) || !ClassifiedRecoverAllowed("sqlite", RecoverBusinessRebuild) {
		t.Fatal("classified sqlite recover must allow native and business rebuild")
	}
	if ClassifiedRecoverAllowed("sqlite", RecoverVerifyReadonly) || ClassifiedRecoverAllowed("sqlite", RecoverKeepStopped) {
		t.Fatal("readonly/stopped must stay Recover=false")
	}
}

func TestQualificationDefaultsUntestedAndNeverAdopted(t *testing.T) {
	got := DefaultQualification("deepseek", "deepseek-chat", CodecDeepSeekV1)
	if got.Status != QualifyUntested || got.Adopted {
		t.Fatalf("offline default must be untested and not adopted: %+v", got)
	}
	if got.Status == "passed_online" || got.Status == "adopted" {
		t.Fatal("must not claim online adoption")
	}
}
