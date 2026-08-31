package m8app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/oklog/ulid/v2"
)

const confirmedInjectIndex = "confirmed-v1"

// RecallForInject scores confirmed candidate payloads so chat.start can fill
// PinnedFacts with user-readable text. This is the live inject corpus: the
// facts-v1 Recall path still searches fact identifiers for the recall.query
// API. Pending candidates never appear. Sensitive payloads are redacted.
// A RecallTrace is always persisted when scoring runs (M8-010).
func (s *MemoryService) RecallForInject(ctx context.Context, in RecallInput) (RecallResult, error) {
	if s == nil || s.uow == nil {
		return RecallResult{}, ErrServiceUnavailable
	}
	if len(in.ScopeID) < 1 || len(in.ScopeID) > m8core.MaxScopeID || len(in.Query) < 1 || len(in.Query) > 2048 {
		return RecallResult{}, fmt.Errorf("%w: scope/query invalid", ErrPayloadInvalid)
	}
	if s.policy == nil {
		return RecallResult{}, ErrPolicyUnavailable
	}
	allowed, perr := s.policy(ctx, s.subject, in.ScopeID)
	if perr != nil {
		return RecallResult{}, fmt.Errorf("%w: %v", ErrPolicyUnavailable, perr)
	}
	if !allowed {
		_ = s.uow.TransactMemory(ctx, func(tx MemoryTx) error {
			_, err := tx.AppendAuditEvent(audit.Event{
				ID: ulid.Make().String(), Action: "memory.recall.refused",
				ResourceType: "memory_scope", ResourceID: in.ScopeID,
				Actor: actorOr(s.subject), CreatedAt: s.clock.Now().UTC().Format(time.RFC3339),
			})
			return err
		})
		return RecallResult{}, fmt.Errorf("%w: scope %q subject %q", ErrRecallScopeDenied, in.ScopeID, s.subject)
	}
	if in.TopK < 1 {
		in.TopK = 6
	}
	if in.TopK > m8core.RecallMaxTopK {
		in.TopK = m8core.RecallMaxTopK
	}
	if in.IndexVersion == "" {
		in.IndexVersion = confirmedInjectIndex
	}
	now := s.clock.Now().UTC()
	terms := injectTerms(in.Query)
	subjectID := strings.TrimSpace(in.SubjectID)
	ftsCand := map[string]bool{}
	var ftsSummaries []MemoryFTSHit
	if s.fts != nil {
		if hits, ferr := s.fts.SearchMemoryFactFTS(ctx, in.Query, in.TopK*4); ferr == nil {
			for _, hit := range hits {
				switch hit.Kind {
				case "candidate":
					ftsCand[hit.SourceID] = true
				case "summary":
					ftsSummaries = append(ftsSummaries, hit)
				}
			}
		}
	}
	rows, err := s.listCandidates(ctx, m8core.CandConfirmed, 200)
	if err != nil {
		return RecallResult{}, err
	}

	type scored struct {
		cand  m8core.MemoryCandidate
		doc   m8core.PayloadDoc
		cov   float64
		fresh float64
		score float64
	}
	var candidates []scored
	redactions := map[string]bool{}
	var notAdopted []string
	for _, c := range rows {
		if subjectID != "" && c.SubjectID != subjectID {
			continue
		}
		var doc m8core.PayloadDoc
		if json.Unmarshal([]byte(c.Payload), &doc) != nil {
			continue
		}
		if doc.ScopeID != in.ScopeID {
			continue
		}
		content := strings.TrimSpace(doc.Content)
		if content == "" {
			continue
		}
		matched := 0
		lc := strings.ToLower(content)
		for _, t := range terms {
			if strings.Contains(lc, t) {
				matched++
			}
		}
		fts := ftsOverlap(in.Query, content)
		if matched == 0 && fts < 0.34 && !ftsCand[c.CandidateID] {
			continue
		}
		if matched == 0 {
			matched = 1
		}
		if doc.Sensitivity == m8core.SensSensitive {
			redactions["policy:sensitivity=sensitive"] = true
			continue
		}
		cov := float64(matched) / float64(len(terms))
		age := now.Sub(parseTime(c.CreatedAt))
		fresh := 1.0 - age.Hours()/(365*24)
		if fresh < 0 {
			fresh = 0
		}
		sc := 0.8*cov + 0.2*fresh
		if sc < m8core.RecallScoreFloor {
			notAdopted = append(notAdopted, fmt.Sprintf("candidate %s: score %.3f below floor", c.CandidateID, sc))
			continue
		}
		candidates = append(candidates, scored{cand: c, doc: doc, cov: cov, fresh: fresh, score: sc})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].cand.CandidateID < candidates[j].cand.CandidateID
	})
	if len(candidates) > in.TopK {
		for _, c := range candidates[in.TopK:] {
			notAdopted = append(notAdopted, fmt.Sprintf("candidate %s: beyond topK", c.cand.CandidateID))
		}
		candidates = candidates[:in.TopK]
	}

	hits := make([]RecallHit, 0, len(candidates))
	reasons := make([]string, 0, len(candidates))
	for _, c := range candidates {
		hits = append(hits, RecallHit{
			Source:          "candidate:" + c.cand.CandidateID,
			Version:         1,
			Score:           round3(c.score),
			ScoreComponents: map[string]float64{"keyword": round3(c.cov), "freshness": round3(c.fresh)},
			Freshness:       c.cand.CreatedAt,
			Content:         strings.TrimSpace(c.doc.Content),
		})
		reasons = append(reasons, fmt.Sprintf("candidate %s: keyword %.3f, freshness %.3f", c.cand.CandidateID, c.cov, c.fresh))
	}
	for _, sum := range ftsSummaries {
		if subjectID != "" {
			break
		}
		if len(hits) >= in.TopK {
			break
		}
		content := strings.TrimSpace(sum.Body)
		if content == "" {
			continue
		}
		hits = append(hits, RecallHit{
			Source:  "summary:" + sum.SourceID,
			Version: 1,
			Score:   0.5,
			Content: content,
		})
		reasons = append(reasons, "summary "+sum.SourceID+": fts")
	}
	red := make([]string, 0, len(redactions))
	for r := range redactions {
		red = append(red, r)
	}
	sort.Strings(red)
	if notAdopted == nil {
		notAdopted = []string{}
	}

	traceID := ulid.Make().String()
	storedHits := make([]RecallHit, len(hits))
	for i, h := range hits {
		storedHits[i] = RecallHit{
			Source: h.Source, Version: h.Version, Score: h.Score,
			ScoreComponents: h.ScoreComponents, Freshness: h.Freshness,
		}
	}
	minHits, err := json.Marshal(storedHits)
	if err != nil {
		return RecallResult{}, err
	}
	err = s.uow.TransactMemory(ctx, func(tx MemoryTx) error {
		if err := tx.PutRecallTrace(m8core.RecallTrace{
			ID:             traceID,
			QueryDigest:    m8core.DigestOf(in.Query),
			HitsJSON:       string(minHits),
			ReasonsJSON:    mustJSON(reasons),
			RedactionsJSON: mustJSON(red),
			CreatedAt:      now.Format(time.RFC3339),
		}); err != nil {
			return fmt.Errorf("%w: trace persist failed: %v", ErrExplanationUnavailable, err)
		}
		_, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "memory.recall.inject",
			ResourceType: "memory_scope", ResourceID: in.ScopeID,
			Actor: actorOr(s.subject), AfterDigest: m8core.DigestOf(in.Query),
			CreatedAt: now.Format(time.RFC3339),
		})
		return err
	})
	if err != nil {
		return RecallResult{}, err
	}
	return RecallResult{
		TraceID:      traceID,
		Hits:         hits,
		Explanation:  RecallExplanation{Reasons: reasons, Redactions: red, NotAdopted: notAdopted, Missing: false},
		IndexVersion: in.IndexVersion,
	}, nil
}

func injectTerms(q string) []string {
	terms := splitTerms(q)
	seen := map[string]bool{}
	out := make([]string, 0, len(terms)+8)
	for _, t := range terms {
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		runes := []rune(t)
		if len(runes) < 2 || !containsCJK(runes) {
			continue
		}
		for i := 0; i+1 < len(runes); i++ {
			gram := string(runes[i : i+2])
			if seen[gram] {
				continue
			}
			seen[gram] = true
			out = append(out, gram)
		}
	}
	if len(out) == 0 {
		return terms
	}
	return out
}

func containsCJK(runes []rune) bool {
	for _, r := range runes {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

func ftsTrigrams(s string) map[string]bool {
	s = strings.ToLower(strings.TrimSpace(s))
	out := map[string]bool{}
	runes := []rune(s)
	if len(runes) == 0 {
		return out
	}
	if len(runes) < 3 {
		out[string(runes)] = true
		return out
	}
	for i := 0; i+2 < len(runes); i++ {
		out[string(runes[i:i+3])] = true
	}
	return out
}

func ftsOverlap(query, body string) float64 {
	qg, bg := ftsTrigrams(query), ftsTrigrams(body)
	if len(qg) == 0 || len(bg) == 0 {
		return 0
	}
	hit := 0
	for g := range qg {
		if bg[g] {
			hit++
		}
	}
	return float64(hit) / float64(len(qg))
}
