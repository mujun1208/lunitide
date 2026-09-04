package mroapp

import (
	"regexp"
	"strings"
)

var (
	aogTailRe = regexp.MustCompile(`(?i)\b([A-Z]-\d{1,8})\b`)
	aogPNRe   = regexp.MustCompile(`(?i)(?:^|[\s：:])(?:pn|件号)\s*[:：]?\s*([A-Z0-9][A-Z0-9._/-]{1,31})`)
	aogQtyRe  = regexp.MustCompile(`(?i)(?:qty|数量)\s*[:：]?\s*(\d+(?:\.\d+)?)`)
)

type PartsStock struct {
	PN     string
	Qty    float64
	Source string
}

type Alternate struct {
	PNFrom      string
	PNTo        string
	CertOK      bool
	Effectivity string
	Qty         float64
}

type AlternateMatch struct {
	Alternate
	Accepted bool
	Reason   string
}

func configHits(effectivity, config string) bool {
	eff := strings.TrimSpace(effectivity)
	cfg := strings.TrimSpace(config)
	if eff == "" || eff == "*" {
		return true
	}
	return cfg != "" && strings.Contains(eff, cfg)
}

// FilterAlternates requires cert × effectivity × stock. Fail any one → quote-only.
func FilterAlternates(alts []Alternate, config string) []AlternateMatch {
	out := make([]AlternateMatch, 0, len(alts))
	for _, alt := range alts {
		m := AlternateMatch{Alternate: alt, Accepted: true}
		if !alt.CertOK {
			m.Accepted = false
			m.Reason = "认证无效"
		} else if !configHits(alt.Effectivity, config) {
			m.Accepted = false
			m.Reason = "构型不适用"
		} else if alt.Qty <= 0 {
			m.Accepted = false
			m.Reason = "无库存，降级询价"
		}
		out = append(out, m)
	}
	return out
}

type AOGDraft struct {
	TailNo string
	PN     string
	Qty    string
	Note   string
}

func ParseAOGPaste(text string) AOGDraft {
	draft := AOGDraft{}
	var notes []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "tail:") || strings.HasPrefix(line, "机尾:"):
			draft.TailNo = strings.TrimSpace(line[strings.Index(line, ":")+1:])
		case strings.HasPrefix(lower, "pn:") || strings.HasPrefix(line, "件号:"):
			draft.PN = strings.TrimSpace(line[strings.Index(line, ":")+1:])
		case strings.HasPrefix(lower, "qty:") || strings.HasPrefix(line, "数量:"):
			draft.Qty = strings.TrimSpace(line[strings.Index(line, ":")+1:])
		default:
			notes = append(notes, line)
		}
	}
	blob := strings.TrimSpace(text)
	if draft.TailNo == "" {
		if m := aogTailRe.FindStringSubmatch(blob); len(m) > 1 {
			draft.TailNo = strings.ToUpper(m[1])
		}
	}
	if draft.PN == "" {
		if m := aogPNRe.FindStringSubmatch(blob); len(m) > 1 {
			draft.PN = m[1]
		}
	}
	if draft.Qty == "" {
		if m := aogQtyRe.FindStringSubmatch(blob); len(m) > 1 {
			draft.Qty = m[1]
		}
	}
	draft.Note = strings.Join(notes, " ")
	if draft.Note == "" {
		draft.Note = strings.Join(strings.Fields(blob), " ")
	}
	return draft
}
