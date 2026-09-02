// risk.go is the M1 trust-tier classifier. It answers one question about a
// pending memory candidate: is its payload safe to auto-accept without a
// human in the loop, or must it stay pending for explicit confirmation?
//
// The classifier is deliberately conservative and pure (no I/O, no clock):
// it returns RiskHigh whenever there is ANY reason to route to a human, so
// the auto-accept path only ever fires on the clearly-benign tail. It is
// consulted ONLY when the default-off M1 switch is armed; with the switch
// off nothing calls it and every candidate stays pending as before.
package m8core

import "strings"

// Memory risk tiers.
const (
	// RiskLow: benign, low-sensitivity payload eligible for governed
	// auto-accept when the M1 switch is armed.
	RiskLow = "low"
	// RiskHigh: sensitive, secret-bearing or oversized payload that must
	// keep the explicit human-confirmation path regardless of the switch.
	RiskHigh = "high"
)

// autoAcceptMaxContentRunes caps auto-accept to short, glanceable facts.
// Anything longer is treated as high-risk and left for human review.
const autoAcceptMaxContentRunes = 280

// secretMarkers are case-insensitive substrings that force RiskHigh: they
// signal credentials, financial identifiers or government IDs that must
// never be memorised without an explicit human decision.
var secretMarkers = []string{
	"password", "passwd", "secret", "api key", "apikey", "token",
	"private key", "credential", "credit card", "ssn", "cvv",
	"密码", "密钥", "口令", "身份证", "银行卡", "验证码", "私钥",
}

// ClassifyMemoryRisk returns RiskLow only for short, private/public,
// secret-free payloads; everything else is RiskHigh. It is total and
// deterministic so the same candidate always classifies the same way.
func ClassifyMemoryRisk(doc PayloadDoc) string {
	content := strings.TrimSpace(doc.Content)
	if content == "" {
		return RiskHigh
	}
	if len([]rune(content)) > autoAcceptMaxContentRunes {
		return RiskHigh
	}
	// Only the two lowest sensitivity levels may auto-accept; "sensitive"
	// (and any unknown value) always routes to a human.
	if doc.Sensitivity != SensPublic && doc.Sensitivity != SensPrivate {
		return RiskHigh
	}
	lower := strings.ToLower(content)
	for _, marker := range secretMarkers {
		if strings.Contains(lower, marker) {
			return RiskHigh
		}
	}
	return RiskLow
}

// MemoryRiskAutoAcceptable reports whether the payload is eligible for the
// governed low-risk auto-accept path.
func MemoryRiskAutoAcceptable(doc PayloadDoc) bool {
	return ClassifyMemoryRisk(doc) == RiskLow
}
