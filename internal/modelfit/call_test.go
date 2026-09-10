package modelfit

import "testing"

func TestUsageIntegrityDoesNotTreatZeroAsReported(t *testing.T) {
	if got := ClassifyUsage(UsageNumbers{}, false); got != UsageUnknown {
		t.Fatalf("empty usage = %q, want unknown", got)
	}
	if got := ClassifyUsage(UsageNumbers{InputTokens: 3, OutputTokens: 1, TotalTokens: 4}, true); got != UsageReported {
		t.Fatalf("provider reported = %q", got)
	}
	if got := ClassifyUsage(UsageNumbers{OutputTokens: 8}, false); got != UsagePartial {
		t.Fatalf("output only = %q, want partial", got)
	}
}

func TestStreamUsageSnapshotsDoNotSum(t *testing.T) {
	acc := UsageAccumulator{}
	acc.ObserveSnapshot(UsageNumbers{InputTokens: 10, OutputTokens: 2, TotalTokens: 12})
	acc.ObserveSnapshot(UsageNumbers{InputTokens: 10, OutputTokens: 5, TotalTokens: 15})
	if acc.Latest.OutputTokens != 5 || acc.Latest.InputTokens != 10 || acc.Updates != 2 {
		t.Fatalf("snapshots summed or dropped: %+v", acc)
	}
}

func TestCallAttemptIdentityStaysStableAcrossReceipt(t *testing.T) {
	intent := NewCallAttempt(CallIdentity{OwnerScope: "session", TaskID: "t1", TurnID: "u1", CallID: "c1", AttemptID: "a1"}, "chat")
	if intent.Status != CallIntent {
		t.Fatalf("intent status = %q", intent.Status)
	}
	done := intent.Receive(UsageNumbers{InputTokens: 2, OutputTokens: 3, TotalTokens: 5}, true, CallSucceeded)
	if done.CallID != "c1" || done.AttemptID != "a1" || done.Status != CallSucceeded {
		t.Fatalf("receipt changed identity: %+v", done)
	}
	if done.UsageIntegrity != UsageReported {
		t.Fatalf("integrity = %q", done.UsageIntegrity)
	}
}
