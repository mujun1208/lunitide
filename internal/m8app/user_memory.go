package m8app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

var (
	ErrMemoryModeOff                 = errors.New("MEMORY_MODE_OFF")
	ErrMemoryExplicitDenied          = errors.New("MEMORY_EXPLICIT_DENIED")
	ErrMemorySourceInvalid           = errors.New("MEMORY_SOURCE_INVALID")
	ErrMemoryScopeDisabled           = errors.New("MEMORY_SCOPE_DISABLED")
	ErrMemoryOperationReplayMismatch = errors.New("OPERATION_REPLAY_MISMATCH")
	ErrMemoryUndoConflict            = errors.New("MEMORY_UNDO_CONFLICT")
	ErrPurgeConfirmationRequired     = errors.New("PURGE_CONFIRMATION_REQUIRED")
	ErrPurgeGrantInvalid             = errors.New("PURGE_GRANT_INVALID")
)

// SaveExplicitMemory is the manual/auto explicit-save path: it writes a
// confirmed user fact immediately and never leaves a pending review banner.
func (s *MemoryService) SaveExplicitMemory(ctx context.Context, subjectID, text string) (m8core.MemoryCandidate, error) {
	if s == nil || s.uow == nil {
		return m8core.MemoryCandidate{}, ErrServiceUnavailable
	}
	content := strings.TrimSpace(text)
	if content == "" || m8core.RejectTransientMemory(content) {
		return m8core.MemoryCandidate{}, ErrMemorySourceInvalid
	}
	doc := m8core.PayloadDoc{
		Content:     content,
		ScopeID:     LearningScope,
		Sensitivity: m8core.SensPrivate,
		Leaves: []m8core.SourceLeafClaim{{
			JSONPointer: "/content",
			EvidenceRef: "operation:explicit",
			Digest:      m8core.DigestOf(content),
		}},
	}
	if m8core.ClassifyMemoryRisk(doc) != m8core.RiskLow {
		return m8core.MemoryCandidate{}, ErrMemorySourceInvalid
	}
	candidate, err := s.ProposeUserMemory(ctx, subjectID, doc)
	if err != nil {
		return m8core.MemoryCandidate{}, err
	}
	if candidate.State == m8core.CandConfirmed {
		if err := s.mirrorCanonical(ctx, subjectID, candidate.CandidateID, content); err != nil {
			return candidate, err
		}
		return candidate, nil
	}
	if candidate.State != m8core.CandPending {
		return candidate, ErrMemoryExplicitDenied
	}
	accepted, err := s.AutoAcceptCandidate(ctx, candidate.CandidateID, "memory.item.create")
	if err != nil {
		return candidate, err
	}
	if !accepted.Accepted {
		return candidate, ErrMemoryExplicitDenied
	}
	candidate.State = m8core.CandConfirmed
	if err := s.mirrorCanonical(ctx, subjectID, candidate.CandidateID, content); err != nil {
		return candidate, err
	}
	return candidate, nil
}

func (s *MemoryService) mirrorCanonical(ctx context.Context, subjectID, operationID, text string) error {
	if s == nil || s.uow == nil {
		return nil
	}
	writer, ok := s.uow.(interface {
		CreateCanonicalMemoryItem(context.Context, m8core.CanonicalMemoryWrite) (m8core.CanonicalMemoryResult, error)
	})
	if !ok {
		return nil
	}
	_, err := writer.CreateCanonicalMemoryItem(ctx, m8core.CanonicalMemoryWrite{
		SubjectID:      subjectID,
		ScopeKind:      "user",
		ScopeID:        subjectID,
		Kind:           m8core.ClassifyMemoryKind(text),
		Text:           text,
		OperationID:    operationID,
		IdempotencyKey: "memory.item.create:" + operationID,
	})
	return err
}

func (s *MemoryService) ListCanonicalUserFacts(ctx context.Context, subjectID string, limit int) ([]string, error) {
	if s == nil || s.uow == nil {
		return nil, nil
	}
	lister, ok := s.uow.(interface {
		ListCanonicalMemoryItems(context.Context, string, string, string, int) ([]string, error)
	})
	if !ok {
		return nil, nil
	}
	items, err := lister.ListCanonicalMemoryItems(ctx, subjectID, "user", subjectID, limit)
	if err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]struct{}{}
	for _, text := range items {
		got := m8core.PersonalMemoryContent(m8core.PayloadDoc{Content: text})
		if got == "" {
			continue
		}
		if _, ok := seen[got]; ok {
			continue
		}
		seen[got] = struct{}{}
		out = append(out, got)
	}
	return out, nil
}

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
