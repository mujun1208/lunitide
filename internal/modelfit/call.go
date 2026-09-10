package modelfit

type UsageIntegrity string

const (
	UsageReported  UsageIntegrity = "reported"
	UsagePartial   UsageIntegrity = "partial"
	UsageEstimated UsageIntegrity = "estimated"
	UsageUnknown   UsageIntegrity = "unknown"
)

type CallStatus string

const (
	CallIntent    CallStatus = "intent"
	CallSent      CallStatus = "sent"
	CallSucceeded CallStatus = "succeeded"
	CallFailed    CallStatus = "failed"
	CallCancelled CallStatus = "cancelled"
	CallUnknown   CallStatus = "unknown"
)

type UsageNumbers struct {
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedInputTokens int
	CacheWriteTokens  int
}

func ClassifyUsage(n UsageNumbers, reported bool) UsageIntegrity {
	if reported && (n.InputTokens > 0 || n.OutputTokens > 0 || n.TotalTokens > 0) {
		return UsageReported
	}
	if n.InputTokens > 0 || n.OutputTokens > 0 || n.TotalTokens > 0 || n.CachedInputTokens > 0 {
		return UsagePartial
	}
	return UsageUnknown
}

type UsageAccumulator struct {
	Latest  UsageNumbers
	Updates int
}

func (a *UsageAccumulator) ObserveSnapshot(n UsageNumbers) {
	a.Latest = n
	a.Updates++
}

type CallIdentity struct {
	OwnerScope   string
	TaskID       string
	TurnID       string
	CallID       string
	ParentCallID string
	AttemptID    string
}

type CallAttempt struct {
	CallIdentity
	Purpose        string
	Status         CallStatus
	Usage          UsageNumbers
	UsageIntegrity UsageIntegrity
}

func NewCallAttempt(id CallIdentity, purpose string) CallAttempt {
	return CallAttempt{CallIdentity: id, Purpose: purpose, Status: CallIntent, UsageIntegrity: UsageUnknown}
}

func (a CallAttempt) Receive(n UsageNumbers, reported bool, status CallStatus) CallAttempt {
	a.Usage = n
	a.UsageIntegrity = ClassifyUsage(n, reported)
	a.Status = status
	return a
}
