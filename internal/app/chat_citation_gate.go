package app

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const mroAdvisoryLine = "辅助建议，不构成放行。"

// CitationBlock is one grounded fragment shown under an assistant bubble.
type CitationBlock struct {
	ExpertID   string `json:"expertId,omitempty"`
	ExpertName string `json:"expertName,omitempty"`
	DocID      string `json:"docId,omitempty"`
	DocType    string `json:"docType,omitempty"`
	Revision   string `json:"revision"`
	Locator    string `json:"locator"`
	Quote      string `json:"quote"`
	ATA        string `json:"ata,omitempty"`
}

// GateChip is a non-destructive warning attached to an MRO answer.
type GateChip struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
}

var (
	pnToken      = regexp.MustCompile(`(?i)(?:NAS|PN)[-A-Z0-9]{2,}|[A-Z]{2,}[0-9]{3,}`)
	torqueDigits = regexp.MustCompile(`扭矩[^。\n]{0,24}\d`)
)

// GateMROAnswer prepends the advisory line when missing. High-risk definite
// claims with zero cites keep the model text and attach a warning chip (C5).
func GateMROAnswer(text string, cites []CitationBlock) (out string, chips []GateChip) {
	out = text
	if !containsAdvisory(text) {
		if strings.TrimSpace(text) == "" {
			out = mroAdvisoryLine
		} else {
			out = mroAdvisoryLine + "\n" + text
		}
		chips = append(chips, GateChip{Kind: "advisory"})
	}
	if hasDefinitePNOrTorque(text) && len(cites) == 0 {
		chips = append(chips, GateChip{Kind: "ungrounded", Detail: "未找到受控依据，勿据此操作"})
	}
	return out, chips
}

func containsAdvisory(text string) bool {
	return strings.Contains(text, "辅助建议") || strings.Contains(text, "不构成放行")
}

func hasDefinitePNOrTorque(text string) bool {
	if strings.Contains(text, "件号") && pnToken.MatchString(text) {
		return true
	}
	return torqueDigits.MatchString(text)
}

// RestoreCouncilCitations appends any cite whose DocID or quote prefix is
// missing from the chair draft.
func RestoreCouncilCitations(draft string, cites []CitationBlock) (string, bool) {
	restored := false
	for _, c := range cites {
		if c.DocID != "" && strings.Contains(draft, c.DocID) {
			continue
		}
		q := c.Quote
		if n := utf8.RuneCountInString(q); n > 40 {
			q = string([]rune(q)[:40])
		}
		if q != "" && strings.Contains(draft, q) {
			continue
		}
		draft += "\n\n" + formatCiteAppendix(c)
		restored = true
	}
	return draft, restored
}

func formatCiteAppendix(c CitationBlock) string {
	rev := strings.TrimSpace(c.Revision)
	if rev == "" {
		rev = "—"
	}
	quote := strings.TrimSpace(c.Quote)
	if n := utf8.RuneCountInString(quote); n > 40 {
		quote = string([]rune(quote)[:40])
	}
	name := strings.TrimSpace(c.ExpertName)
	if name == "" {
		name = "机务"
	}
	return fmt.Sprintf("【引用 · %s】修订 %s · %s\n%s", name, rev, strings.TrimSpace(c.DocID), quote)
}

func turnHasMROName(names []string) bool {
	for _, n := range names {
		if isMROColleague(strings.TrimSpace(n), "") || strings.TrimSpace(n) == "mro-expert" {
			return true
		}
	}
	return false
}

func applyMROAnswerGate(persistText string, state *streamState) (next string, streamDelta string) {
	if state == nil || (!state.mroTurn && len(state.kbCites) == 0) {
		return persistText, ""
	}
	thinking, body := splitPersistedThinkingText(persistText)
	gated, chips := GateMROAnswer(body, state.kbCites)
	restored := false
	if len(state.kbCites) > 0 {
		gated, restored = RestoreCouncilCitations(gated, state.kbCites)
	}
	next = gated
	if thinking != "" {
		if next == "" {
			next = thinking
		} else {
			next = thinking + "\n\n" + gated
		}
	}
	if restored && body != "" {
		if i := strings.Index(gated, body); i >= 0 && i+len(body) <= len(gated) {
			streamDelta = gated[i+len(body):]
		}
	}
	if marker := formatMroCiteMarker(state.kbCites, chips, state.kbDiscarded, restored); marker != "" {
		if strings.TrimSpace(next) == "" {
			next = marker
		} else {
			next = strings.TrimRight(next, "\n") + "\n" + marker
		}
		if streamDelta != "" {
			streamDelta = strings.TrimRight(streamDelta, "\n") + "\n" + marker
		} else {
			streamDelta = "\n" + marker
		}
	}
	return next, streamDelta
}

func splitPersistedThinkingText(text string) (thinking, body string) {
	const mark = "【思考过程】"
	trimmed := strings.TrimLeft(text, " \t\r\n")
	if !strings.HasPrefix(trimmed, mark) {
		return "", text
	}
	rest := strings.TrimPrefix(trimmed, mark)
	rest = strings.TrimPrefix(rest, "\n")
	if i := strings.Index(rest, "\n\n"); i >= 0 {
		return strings.TrimSpace(rest[:i]), rest[i+2:]
	}
	return strings.TrimSpace(rest), ""
}

func formatMroCiteMarker(cites []CitationBlock, chips []GateChip, discarded int, restored bool) string {
	gate := ""
	for _, c := range chips {
		if c.Kind == "ungrounded" {
			gate = "ungrounded"
			break
		}
		if gate == "" {
			gate = c.Kind
		}
	}
	payload := struct {
		Cites     []CitationBlock `json:"cites"`
		Gate      string          `json:"gate,omitempty"`
		Discarded int             `json:"discarded"`
		Restored  bool            `json:"restored"`
	}{Cites: cites, Gate: gate, Discarded: discarded, Restored: restored}
	if payload.Cites == nil {
		payload.Cites = []CitationBlock{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return "<!--mro-cite:" + string(raw) + "-->"
}
