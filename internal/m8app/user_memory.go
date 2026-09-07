package m8app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// ProposeUserMemory deduplicates direct user statements across sessions and
// restarts. A previously rejected statement stays rejected until user action.
func (s *MemoryService) ProposeUserMemory(ctx context.Context, subjectID string, doc m8core.PayloadDoc) (m8core.MemoryCandidate, error) {
	if s == nil || s.uow == nil {
		return m8core.MemoryCandidate{}, ErrServiceUnavailable
	}
	s.userMemoryMu.Lock()
	defer s.userMemoryMu.Unlock()
	if err := ctx.Err(); err != nil {
		return m8core.MemoryCandidate{}, err
	}
	var prior m8core.MemoryCandidate
	err := s.uow.TransactMemory(ctx, func(tx MemoryTx) error {
		if finder, ok := tx.(interface {
			FindUserMemoryCandidate(string, string, string) (m8core.MemoryCandidate, error)
		}); ok {
			var err error
			prior, err = finder.FindUserMemoryCandidate(subjectID, doc.ScopeID, doc.Content)
			return err
		}
		for _, state := range []string{m8core.CandPending, m8core.CandConfirmed, m8core.CandRejected} {
			rows, err := tx.ListCandidatesByState(state, 200)
			if err != nil {
				return err
			}
			for _, row := range rows {
				var stored m8core.PayloadDoc
				if row.SubjectID == subjectID && json.Unmarshal([]byte(row.Payload), &stored) == nil && stored.ScopeID == doc.ScopeID && stored.Content == doc.Content {
					prior = row
					return nil
				}
			}
		}
		return nil
	})
	if err != nil || prior.CandidateID != "" {
		return prior, err
	}
	result, err := s.ProposeCandidate(ctx, ProposeInput{SubjectID: subjectID, Doc: doc, Inferred: false, Trust: m8core.TrustUntrusted, Actor: "chat.user-memory"})
	return result.Candidate, err
}

// PersonalPreferenceSnapshot keeps raw legacy records manageable in the memory
// center, while excluding assistant/expert summaries from ordinary chat.
func (s *MemoryService) PersonalPreferenceSnapshot(ctx context.Context, subjectID, scopeID string, maxItems, maxBytes int) ([]string, error) {
	rows, err := s.visibleConfirmedCandidates(ctx, 200)
	if err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	used := 0
	for _, row := range rows {
		if row.SubjectID != subjectID {
			continue
		}
		var doc m8core.PayloadDoc
		if json.Unmarshal([]byte(row.Payload), &doc) != nil || doc.ScopeID != scopeID || doc.Sensitivity == m8core.SensSensitive {
			continue
		}
		text := m8core.PersonalMemoryContent(doc)
		if text == "" || seen[text] || used+len(text) > maxBytes {
			continue
		}
		out = append(out, text)
		seen[text] = true
		used += len(text)
		if len(out) >= maxItems {
			break
		}
	}
	return out, nil
}

func candidateSourceSession(payload string) string {
	var doc m8core.PayloadDoc
	if json.Unmarshal([]byte(payload), &doc) != nil {
		return ""
	}
	return strings.TrimSpace(m8core.MemorySourceSession(doc))
}

// A fact hidden or removed in the memory center must also leave all automatic
// injection paths. Older in-memory adapters retain the same read interface.
func (s *MemoryService) visibleConfirmedCandidates(ctx context.Context, limit int) ([]m8core.MemoryCandidate, error) {
	if s == nil || s.uow == nil {
		return nil, ErrServiceUnavailable
	}
	var out []m8core.MemoryCandidate
	err := s.uow.TransactMemory(ctx, func(tx MemoryTx) error {
		var err error
		if reader, ok := tx.(interface {
			ListVisibleConfirmedCandidates(int) ([]m8core.MemoryCandidate, error)
		}); ok {
			out, err = reader.ListVisibleConfirmedCandidates(limit)
		} else {
			out, err = tx.ListCandidatesByState(m8core.CandConfirmed, limit)
		}
		return err
	})
	return out, err
}
