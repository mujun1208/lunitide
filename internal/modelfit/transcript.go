package modelfit

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type NativeScope struct {
	OwnerScope string `json:"ownerScope"`
	SessionID  string `json:"sessionId"`
}

type NativeMessage struct {
	ID         string          `json:"id"`
	TurnID     string          `json:"turnId"`
	CallID     string          `json:"callId"`
	Role       string          `json:"role"`
	Provenance string          `json:"provenance"`
	Sequence   int64           `json:"sequence"`
	Complete   bool            `json:"complete"`
	WireJSON   json.RawMessage `json:"-"`
}

type NativeEpoch struct {
	ID                string         `json:"id"`
	Scope             NativeScope    `json:"scope"`
	Target            TargetIdentity `json:"target"`
	Profile           ModelProfile   `json:"profile"`
	Revision          int64          `json:"revision"`
	LastSequence      int64          `json:"lastSequence"`
	ObservedModel     *string        `json:"observedModel"`
	ObservedRevision  *string        `json:"observedRevision"`
	IdentityIntegrity string         `json:"identityIntegrity"`
	ObservedAt        string         `json:"observedAt"`
	State             string         `json:"state"`
}

type NativeSnapshot struct {
	Epoch    NativeEpoch     `json:"epoch"`
	Messages []NativeMessage `json:"messages"`
}

type ReplayDecision struct {
	Mode       string          `json:"mode"`
	ReasonCode string          `json:"reasonCode"`
	EpochID    string          `json:"epochId"`
	Messages   []NativeMessage `json:"messages"`
}

type ReplayContinuation struct {
	Decision ReplayDecision
	Epoch    NativeEpoch
	TaskID   string
	Budget   json.RawMessage
}

func CheckReplay(snapshot NativeSnapshot, target TargetIdentity, profile ModelProfile) (ReplayDecision, error) {
	if err := validateNativeSequence(snapshot.Messages); err != nil {
		return ReplayDecision{Mode: "structured_rebuild", ReasonCode: "NATIVE_SEQUENCE_INVALID"}, nil
	}
	ok, err := replayCompatible(snapshot.Epoch, target, profile)
	if err != nil {
		return ReplayDecision{}, err
	}
	if !ok {
		return ReplayDecision{Mode: "structured_rebuild", ReasonCode: "NATIVE_TARGET_MISMATCH"}, nil
	}
	return ReplayDecision{
		Mode:     "native",
		EpochID:  snapshot.Epoch.ID,
		Messages: cloneNativeMessages(snapshot.Messages),
	}, nil
}

func ContinueReplay(snapshot NativeSnapshot, target TargetIdentity, profile ModelProfile, taskID string, budget json.RawMessage) (ReplayContinuation, error) {
	decision, err := CheckReplay(snapshot, target, profile)
	if err != nil {
		return ReplayContinuation{}, err
	}
	out := ReplayContinuation{
		Decision: decision,
		Epoch:    snapshot.Epoch,
		TaskID:   taskID,
		Budget:   append(json.RawMessage(nil), budget...),
	}
	if decision.ReasonCode != "NATIVE_TARGET_MISMATCH" {
		return out, nil
	}
	epoch, err := newNativeEpoch(snapshot.Epoch.Scope, target, profile)
	if err != nil {
		return ReplayContinuation{}, err
	}
	out.Epoch = epoch
	out.Decision.EpochID = epoch.ID
	return out, nil
}

func replayCompatible(epoch NativeEpoch, target TargetIdentity, profile ModelProfile) (bool, error) {
	if epoch.State != "" && epoch.State != "active" {
		return false, nil
	}
	haveTarget, err := TargetDigest(epoch.Target)
	if err != nil {
		return false, err
	}
	wantTarget, err := TargetDigest(target)
	if err != nil {
		return false, err
	}
	if haveTarget != wantTarget {
		return false, nil
	}
	haveProfile, err := ProfileDigest(epoch.Profile)
	if err != nil {
		return false, err
	}
	wantProfile, err := ProfileDigest(profile)
	if err != nil {
		return false, err
	}
	if haveProfile != wantProfile {
		return false, nil
	}
	if epoch.Target.CodecVersion != target.CodecVersion || epoch.Profile.CodecVersion != profile.CodecVersion {
		return false, nil
	}
	return true, nil
}

func validateNativeSequence(messages []NativeMessage) error {
	for i, msg := range messages {
		if msg.Sequence != int64(i+1) {
			return fmt.Errorf("native sequence must be 1..n without gaps")
		}
	}
	return nil
}

func cloneNativeMessages(in []NativeMessage) []NativeMessage {
	if in == nil {
		return nil
	}
	out := make([]NativeMessage, len(in))
	for i, msg := range in {
		out[i] = msg
		if msg.WireJSON != nil {
			out[i].WireJSON = append(json.RawMessage(nil), msg.WireJSON...)
		}
	}
	return out
}

func newNativeEpoch(scope NativeScope, target TargetIdentity, profile ModelProfile) (NativeEpoch, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return NativeEpoch{}, err
	}
	return NativeEpoch{
		ID:       hex.EncodeToString(raw[:]),
		Scope:    scope,
		Target:   target,
		Profile:  profile,
		Revision: 1,
		State:    "active",
	}, nil
}
