// expert_bus.go is the M2 shared expert working-memory bus.
//
// FREEZE RELATIONSHIP: the expert freeze is "experts are personas on the
// single 月汐 engine, never independent (P2) runtimes". M2 does NOT add a
// process, a scheduler, or inter-agent messaging. It is purely an in-turn,
// in-process scratch pad: when armed, each council specialist's opinion is
// appended to a bounded bus and rendered into the NEXT specialist's prompt,
// so later experts build on earlier ones instead of deliberating blind.
// With the switch off the bus is never constructed and the council runs
// exactly as before (independent parallel passes). The bus never persists
// beyond the turn and never crosses sessions.
package app

import "strings"

// expertBusMaxRenderRunes caps the whole rendered prior-findings block so a
// shared council turn cannot balloon the prompt.
const expertBusMaxRenderRunes = 1600

// expertBusMaxEntryRunes caps one expert's contribution inside the render.
const expertBusMaxEntryRunes = 400

type expertBusEntry struct {
	Expert string
	Text   string
}

// expertSharedBus is an ordered, bounded, in-turn scratch pad of expert
// opinions. It is not safe for concurrent use: the shared-bus path runs
// experts sequentially by design (each reads all prior contributions).
type expertSharedBus struct {
	entries []expertBusEntry
}

func newExpertSharedBus() *expertSharedBus { return &expertSharedBus{} }

// append records one expert's contribution. Empty text or expert is
// ignored so a silent specialist adds nothing to later prompts.
func (b *expertSharedBus) append(expert, text string) {
	if b == nil {
		return
	}
	expert = strings.TrimSpace(expert)
	text = strings.TrimSpace(text)
	if expert == "" || text == "" {
		return
	}
	b.entries = append(b.entries, expertBusEntry{Expert: expert, Text: clipRunesTo(text, expertBusMaxEntryRunes)})
}

func (b *expertSharedBus) len() int {
	if b == nil {
		return 0
	}
	return len(b.entries)
}

// render formats the accumulated contributions for injection into the next
// expert's user prompt. It returns "" when empty so the first expert (and
// the disarmed path) gets no extra context. The whole block is capped.
func (b *expertSharedBus) render() string {
	if b == nil || len(b.entries) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("【其他专家已给出的要点（请在此基础上补充或提出异议，不要简单重复）】\n")
	for _, e := range b.entries {
		sb.WriteString("- ")
		sb.WriteString(e.Expert)
		sb.WriteString("：")
		sb.WriteString(e.Text)
		sb.WriteByte('\n')
	}
	return clipRunesTo(sb.String(), expertBusMaxRenderRunes)
}

// clipRunesTo trims s to at most n runes, appending an ellipsis marker when
// it had to cut.
func clipRunesTo(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
