// Full-conversation complexity routing and the frozen delegation synthesis
// contract (migration 0053): m6_complexity_decision, m6_child_manifest,
// m6_result_bundle, m6_synthesis_record.
//
// Routing (M6/02 §09): the router scores the whole conversation (not the
// last turn) into one of four tiers mapped onto one of three paths —
//
//	simple          -> single           (one model, one pass)
//	moderate        -> planned-single   (one model, explicit plan)
//	complex         -> delegated        (delegation + barrier + merge)
//	high-risk       -> delegated        (complex path + mandatory gates)
//
// every decision carries reason codes (why this tier), a confidence in
// [0,1] and the router version; (inputDigest, routerVersion) is UNIQUE —
// the same conversation scored by the same router is the same decision.
//
// Synthesis contract: before a delegation spawns, its ChildContextManifest
// is frozen (task scope, locked inputs, budget, capabilities — digest
// pinned); children return ResultBundles (claims, patch digest, test
// evidence, usage — digest pinned); the root records one SynthesisRecord
// (adopted consistent set, conflicts, missing evidence, adoption reasons —
// digest pinned). All three digests are content-addressed and immutable.
package m6supply

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Complexity tiers (m6_complexity_decision.tier CHECK set).
const (
	TierSimple   = "simple"
	TierModerate = "moderate"
	TierComplex  = "complex"
	TierHighRisk = "high-risk"
)

// Routing paths (m6_complexity_decision.routed_path CHECK set).
const (
	PathSingle        = "single"
	PathPlannedSingle = "planned-single"
	PathDelegated     = "delegated"
)

// TierPath maps a tier onto its routed path.
func TierPath(tier string) string {
	switch tier {
	case TierSimple:
		return PathSingle
	case TierModerate:
		return PathPlannedSingle
	case TierComplex, TierHighRisk:
		return PathDelegated
	}
	return PathSingle
}

// ValidTier checks the tier enum.
func ValidTier(tier string) bool {
	switch tier {
	case TierSimple, TierModerate, TierComplex, TierHighRisk:
		return true
	}
	return false
}

// ComplexityDecision is one scored routing decision.
type ComplexityDecision struct {
	ID            string
	SessionID     string
	InputDigest   string
	RouterVersion string
	Tier          string
	RoutedPath    string
	ReasonCodes   string // JSON array of codes
	Confidence    float64
	CreatedAt     time.Time
}

// ValidateDecisionInput checks the decision payload shape.
func ValidateDecisionInput(tier, reasonCodes string, confidence float64) error {
	if !ValidTier(tier) {
		return fmt.Errorf("tier must be simple|moderate|complex|high-risk")
	}
	var codes []string
	if err := json.Unmarshal([]byte(reasonCodes), &codes); err != nil {
		return fmt.Errorf("reasonCodes must be a JSON array of strings: %v", err)
	}
	if len(codes) < 1 || len(codes) > 64 {
		return fmt.Errorf("reasonCodes must carry 1..64 codes")
	}
	for _, c := range codes {
		if len(c) < 1 || len(c) > 128 {
			return fmt.Errorf("reason code length must be 1..128")
		}
	}
	if confidence < 0 || confidence > 1 {
		return fmt.Errorf("confidence must be within [0,1]")
	}
	return nil
}

// ── Frozen synthesis contract ───────────────────────────────────────────────

// ChildContextManifest freezes what a delegation may see and do. The
// digest covers the canonical JSON of scope + locked inputs + budget +
// capabilities; manifest_digest is UNIQUE — identical contexts collapse.
type ChildContextManifest struct {
	ID            string
	DelegationID  string
	ManifestDigest string
	TaskScope     string // JSON
	LockedInputs  string // JSON
	BudgetJSON    string
	Capabilities  string // JSON
	CreatedAt     time.Time
}

// ResultBundle freezes what a child claims back. result_digest is UNIQUE;
// (delegation_id, attempt) is UNIQUE — one bundle per attempt.
type ResultBundle struct {
	ID           string
	DelegationID string
	ChildID      string
	Attempt      int64
	BaseHead     string
	Claims       string // JSON
	PatchDigest  string
	TestEvidence string // JSON
	Usage        string // JSON
	RiskNotes    string // JSON, optional
	ResultDigest string
	CreatedAt    time.Time
}

// SynthesisRecord freezes the root's adoption decision over the bundles.
type SynthesisRecord struct {
	ID              string
	RootID          string
	BarrierID       string
	SynthesisDigest string
	Consistent      string // JSON
	Conflicts       string // JSON
	MissingEvidence string // JSON
	AdoptionReasons string // JSON
	CreatedAt       time.Time
}

// DigestJSON canonicalizes v and returns its sha-256 hex digest.
func DigestJSON(v any) (string, error) {
	canonical, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// ManifestPayload is the canonical struct behind ManifestDigest.
type ManifestPayload struct {
	TaskScope     json.RawMessage `json:"taskScope"`
	LockedInputs  json.RawMessage `json:"lockedInputs"`
	Budget        json.RawMessage `json:"budget"`
	Capabilities  json.RawMessage `json:"capabilities"`
}

// BundlePayload is the canonical struct behind ResultDigest.
type BundlePayload struct {
	ChildID      string          `json:"childId"`
	Attempt      int64           `json:"attempt"`
	BaseHead     string          `json:"baseHead"`
	Claims       json.RawMessage `json:"claims"`
	PatchDigest  string          `json:"patchDigest,omitempty"`
	TestEvidence json.RawMessage `json:"testEvidence"`
	Usage        json.RawMessage `json:"usage"`
	RiskNotes    json.RawMessage `json:"riskNotes,omitempty"`
}

// SynthesisPayload is the canonical struct behind SynthesisDigest.
type SynthesisPayload struct {
	RootID          string          `json:"rootId"`
	BarrierID       string          `json:"barrierId,omitempty"`
	Consistent      json.RawMessage `json:"consistent"`
	Conflicts       json.RawMessage `json:"conflicts"`
	MissingEvidence json.RawMessage `json:"missingEvidence"`
	AdoptionReasons json.RawMessage `json:"adoptionReasons"`
}

// ValidateJSONDoc checks that raw is a non-empty JSON document within the
// given byte bound (0 disables the bound).
func ValidateJSONDoc(raw string, maxBytes int) error {
	if len(raw) < 2 {
		return fmt.Errorf("empty JSON document")
	}
	if maxBytes > 0 && len(raw) > maxBytes {
		return fmt.Errorf("JSON document over %d bytes", maxBytes)
	}
	if !json.Valid([]byte(raw)) {
		return fmt.Errorf("invalid JSON document")
	}
	return nil
}
