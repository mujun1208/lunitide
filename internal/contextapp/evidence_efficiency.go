package contextapp

// EvidenceEfficiency records projection work avoided, not a provider bill or
// measured token saving. Raw sources and durable history are never modified.
type EvidenceEfficiency struct {
	DuplicateSources   int
	SourceBytesAvoided int
}

// prepareEvidence removes only an exact replay of the same identified source
// within the same evidence lane of one already scoped envelope. Equal text
// from different files, revisions, sessions or authorities is not a duplicate.
// Instructions, task/workspace state, checkpoints and user turns are protected.
func prepareEvidence(env ContextEnvelope) (ContextEnvelope, EvidenceEfficiency, []SelectionTraceEntry) {
	var stats EvidenceEfficiency
	var trace []SelectionTraceEntry
	if env.DisableTokenEfficiency {
		return env, stats, trace
	}
	dedup := func(sources []ContextSource) []ContextSource {
		seen := make(map[ContextSource]bool, len(sources))
		out := make([]ContextSource, 0, len(sources))
		for _, src := range sources {
			// Missing identity/provenance cannot prove identical evidence.
			if src.ID == "" || src.Provenance == "" || src.Deleted || !seen[src] {
				out = append(out, src)
				seen[src] = true
				continue
			}
			stats.DuplicateSources++
			stats.SourceBytesAvoided += len(src.Content)
			trace = append(trace, SelectionTraceEntry{SourceType: src.Type, SourceID: src.ID, Authority: src.Authority, Provenance: src.Provenance, RejectReason: "exact_source_duplicate"})
		}
		if len(out) == len(sources) {
			return sources
		}
		return out
	}
	env.AttachmentExcerpts = dedup(env.AttachmentExcerpts)
	env.HandoffCapsules = dedup(env.HandoffCapsules)
	env.RelatedEvidence = dedup(env.RelatedEvidence)
	return env, stats, trace
}
