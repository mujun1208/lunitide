// Package complexity implements the full-conversation complexity router
// (M6/02 §09): deterministic feature scoring into four tiers, each
// decision carrying reason codes, a confidence and the router version.
//
// The router is deliberately rule-based, not model-based — the same
// conversation must yield the same (inputDigest, routerVersion) decision
// row. Reason codes are the auditable "why": every signal that moved the
// tier is named in the record.
package complexity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
)

// RouterVersion is the frozen rule-set version.
const RouterVersion = "rule-v1"

// ConversationSignals is the extracted feature vector of one full
// conversation. Unknown/zero fields are simply not scored.
type ConversationSignals struct {
	MessageCount     int     `json:"messageCount"`
	TurnCount        int     `json:"turnCount"`
	ToolCallCount    int     `json:"toolCallCount"`
	DistinctTools    int     `json:"distinctTools"`
	FileEditCount    int     `json:"fileEditCount"`
	DistinctFiles    int     `json:"distinctFiles"`
	EstTokens       int     `json:"estTokens"`
	ErrorCount      int     `json:"errorCount"`
	RetryCount      int     `json:"retryCount"`
	DelegationHints int     `json:"delegationHints"`
	ParallelismHint bool    `json:"parallelismHint"`
	SecurityRelevant bool   `json:"securityRelevant"`
	IrreversibleOps  bool    `json:"irreversibleOps"`
	CrossModuleScope bool   `json:"crossModuleScope"`
}

// Signal weights (tier scoring). Weights are additive; the thresholds
// carve the score space into the four tiers.
const (
	wMessage     = 0.5
	wTurn        = 1.0
	wToolCall    = 0.5
	wDistinctTool = 1.5
	wFileEdit    = 1.0
	wDistinctFile = 2.0
	wError       = 1.0
	wRetry       = 1.5
	wDelegation  = 4.0
	wParallel    = 3.0
	wSecurity    = 5.0
	wIrreversible = 6.0
	wCrossModule = 4.0

	tierModerate = 8.0
	tierComplex  = 16.0
)

// Decision is the router output.
type Decision struct {
	Tier         string   `json:"tier"`
	RoutedPath   string   `json:"routedPath"`
	ReasonCodes  []string `json:"reasonCodes"`
	Confidence   float64  `json:"confidence"`
	Score        float64  `json:"score"`
	InputDigest  string   `json:"inputDigest"`
	RouterVersion string  `json:"routerVersion"`
}

// Digest canonicalizes the signals into the decision input digest — the
// same conversation must produce the same digest.
func Digest(s ConversationSignals) string {
	canonical, err := json.Marshal(s)
	if err != nil {
		// signals are a plain struct; marshal cannot fail in practice —
		// fall back to a stable error digest rather than panicking.
		sum := sha256.Sum256([]byte(fmt.Sprintf("digest-error:%v", err)))
		return hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// Route scores the full conversation and picks the tier and path.
//
// Determinism: pure function of the signals. Confidence reflects how far
// the score sits from the nearest tier boundary (a score deep inside a
// band is high-confidence; one at a boundary is low) — an honest signal
// for the policy layer to demand human review.
func Route(s ConversationSignals) Decision {
	score := 0.0
	reasons := make([]string, 0, 12)

	if s.MessageCount > 20 {
		score += wMessage * float64(s.MessageCount-20)
	}
	if s.TurnCount > 6 {
		score += wTurn * float64(s.TurnCount-6)
		reasons = append(reasons, "turn.depth")
	}
	if s.ToolCallCount > 12 {
		score += wToolCall * float64(s.ToolCallCount-12)
		reasons = append(reasons, "tool.volume")
	}
	if s.DistinctTools > 4 {
		score += wDistinctTool * float64(s.DistinctTools-4)
		reasons = append(reasons, "tool.breadth")
	}
	if s.FileEditCount > 8 {
		score += wFileEdit * float64(s.FileEditCount-8)
		reasons = append(reasons, "edit.volume")
	}
	if s.DistinctFiles > 5 {
		score += wDistinctFile * float64(s.DistinctFiles-5)
		reasons = append(reasons, "edit.breadth")
	}
	if s.EstTokens > 200_000 {
		score += 4.0
		reasons = append(reasons, "context.size")
	}
	if s.ErrorCount > 3 {
		score += wError * float64(s.ErrorCount-3)
		reasons = append(reasons, "error.repetition")
	}
	if s.RetryCount > 2 {
		score += wRetry * float64(s.RetryCount-2)
		reasons = append(reasons, "retry.loop")
	}
	if s.DelegationHints > 0 {
		score += wDelegation * float64(s.DelegationHints)
		reasons = append(reasons, "delegation.hint")
	}
	if s.ParallelismHint {
		score += wParallel
		reasons = append(reasons, "parallelism")
	}
	if s.CrossModuleScope {
		score += wCrossModule
		reasons = append(reasons, "scope.cross-module")
	}

	tier := "simple"
	switch {
	case score >= tierComplex:
		tier = "complex"
	case score >= tierModerate:
		tier = "moderate"
	}

	// high-risk overrides complex when the conversation touches security
	// surfaces or irreversible operations — always the delegated path,
	// always with the gate codes named.
	if s.SecurityRelevant {
		reasons = append(reasons, "risk.security")
	}
	if s.IrreversibleOps {
		reasons = append(reasons, "risk.irreversible")
	}
	if (s.SecurityRelevant || s.IrreversibleOps) && (tier == "complex" || score >= tierModerate) {
		tier = "high-risk"
	} else if s.SecurityRelevant || s.IrreversibleOps {
		// a risky but small task stays on its small path; the risk is
		// still named for the gate layer.
		score += wSecurity
	}

	if tier == "simple" && len(reasons) == 0 {
		reasons = append(reasons, "scope.simple")
	}

	boundary := nearestBoundary(score)
	confidence := clamp01(1.0 - boundary/8.0)

	return Decision{
		Tier:          tier,
		RoutedPath:    tierPath(tier),
		ReasonCodes:   reasons,
		Confidence:   confidence,
		Score:         math.Round(score*100) / 100,
		InputDigest:   Digest(s),
		RouterVersion: RouterVersion,
	}
}

func tierPath(tier string) string {
	switch tier {
	case "simple":
		return "single"
	case "moderate":
		return "planned-single"
	case "complex", "high-risk":
		return "delegated"
	}
	return "single"
}

// nearestBoundary returns the distance from score to the nearest tier
// boundary (moderate=8, complex=16).
func nearestBoundary(score float64) float64 {
	d1 := math.Abs(score - tierModerate)
	d2 := math.Abs(score - tierComplex)
	return math.Min(d1, d2)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
