package m8app

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// ExpertScopeID is the collection scope for one expert's knowledge base.
func ExpertScopeID(expertID string) string { return "expert:" + strings.TrimSpace(expertID) }

// KBSearchHit is one FTS row plus its document, before effectivity filtering.
type KBSearchHit struct {
	Chunk    m8core.KBChunk
	Document m8core.KBDocument
	Score    float64
	Dropped  string
}

// KBSearchInput is the kb.search command.
type KBSearchInput struct {
	ExpertID string
	Query    string
	TopK     int
	TailNo   string
	AsOf     string
	DocType  string
}

// KBCitedHit is one grounded answer fragment.
type KBCitedHit struct {
	ExpertID string  `json:"expertId"`
	DocID    string  `json:"docId"`
	Revision string  `json:"revision"`
	Locator  string  `json:"locator"`
	Quote    string  `json:"quote"`
	Score    float64 `json:"score"`
}

// KBExplanation is the recall-style explanation block.
type KBExplanation struct {
	Reasons    []string `json:"reasons"`
	Redactions []string `json:"redactions"`
	NotAdopted []string `json:"notAdopted"`
	Missing    bool     `json:"missing"`
}

// KBSearchResult is the kb.search outcome.
type KBSearchResult struct {
	TraceID      string        `json:"traceId"`
	Hits         []KBCitedHit  `json:"hits"`
	Explanation  KBExplanation `json:"explanation"`
	IndexVersion string        `json:"indexVersion"`
}

// EnsureExpertCollection creates the expert-scoped collection once.
func (s *KBService) EnsureExpertCollection(ctx context.Context, expertID string) (KBCollection, error) {
	if s == nil || s.uow == nil {
		return KBCollection{}, ErrServiceUnavailable
	}
	expertID = strings.TrimSpace(expertID)
	if len(expertID) != 26 {
		return KBCollection{}, ErrPayloadInvalid
	}
	scope := ExpertScopeID(expertID)
	var out KBCollection
	err := s.uow.TransactKB(ctx, func(tx KBTx) error {
		existing, ok, err := tx.GetKBCollectionByScope(scope)
		if err != nil {
			return err
		}
		if ok {
			out = existing
			return nil
		}
		out = KBCollection{
			CollectionID: ulid.Make().String(),
			SubjectID:    s.subject,
			ScopeID:      scope,
			AuthPolicy:   "local-owner",
			CreatedAt:    s.clock.Now().UTC().Format(time.RFC3339),
		}
		return tx.PutKBCollectionIfAbsent(out)
	})
	return out, err
}

// Search runs FTS over the expert collection and applies effectivity drops.
func (s *KBService) Search(ctx context.Context, in KBSearchInput) (KBSearchResult, error) {
	if s == nil || s.uow == nil {
		return KBSearchResult{}, ErrServiceUnavailable
	}
	q := strings.TrimSpace(in.Query)
	if q == "" || len(in.ExpertID) != 26 {
		return KBSearchResult{}, ErrPayloadInvalid
	}
	topK := in.TopK
	if topK <= 0 {
		topK = 6
	}
	if topK > 50 {
		topK = 50
	}
	out := KBSearchResult{
		TraceID:      ulid.Make().String(),
		Hits:         []KBCitedHit{},
		Explanation:  KBExplanation{Reasons: []string{}, Redactions: []string{}, NotAdopted: []string{}},
		IndexVersion: ftsIndexVersion,
	}
	scope := ExpertScopeID(in.ExpertID)
	var ftsHits []KBSearchHit
	var denseRows []KBChunkEmbedding
	err := s.uow.TransactKB(ctx, func(tx KBTx) error {
		coll, ok, err := tx.GetKBCollectionByScope(scope)
		if err != nil {
			return err
		}
		if !ok {
			out.Explanation.Missing = true
			out.Explanation.Reasons = append(out.Explanation.Reasons, "no collection for expert")
			return nil
		}
		_ = coll
		hits, err := tx.SearchKBChunkFTS(scope, q, topK*3)
		if err != nil {
			return err
		}
		ftsHits = hits
		if s.embedder != nil {
			denseRows, err = tx.ListKBChunkEmbeddings(scope)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	if out.Explanation.Missing {
		return out, nil
	}
	now := time.Now().UTC()
	if s.clock != nil {
		now = s.clock.Now().UTC()
	}
	usedDense := false
	if s.embedder != nil && hasUsableDense(denseRows) {
		if qv, eerr := s.embedder(ctx, []string{q}); eerr == nil && len(qv) > 0 && len(qv[0]) > 0 {
			cands := fuseHybrid(ftsHits, denseRows, qv[0], now, topK)
			out.IndexVersion = hybridIndexVersion
			usedDense = true
			adoptHybridHits(&out, in, cands, topK)
		}
	}
	if !usedDense {
		adoptFTSHits(&out, in, ftsHits, topK)
	}
	if len(out.Hits) == 0 {
		out.Explanation.Missing = true
		if len(out.Explanation.Reasons) == 0 {
			out.Explanation.Reasons = append(out.Explanation.Reasons, "no controlled chunk matched")
		}
	} else if !containsReason(out.Explanation.Reasons, "fts5 body match") && len(ftsHits) > 0 {
		out.Explanation.Reasons = append(out.Explanation.Reasons, "fts5 body match")
	}
	return out, nil
}

func hasUsableDense(rows []KBChunkEmbedding) bool {
	for _, row := range rows {
		if _, ok := decodeChunkVector(row.Chunk.Embedding); ok {
			return true
		}
	}
	return false
}

func adoptFTSHits(out *KBSearchResult, in KBSearchInput, hits []KBSearchHit, topK int) {
	for _, hit := range hits {
		if !adoptOneHit(out, in, hit, hit.Score) {
			continue
		}
		if len(out.Hits) >= topK {
			break
		}
	}
	if len(out.Hits) > 0 && !containsReason(out.Explanation.Reasons, "fts5 body match") {
		out.Explanation.Reasons = append(out.Explanation.Reasons, "fts5 body match")
	}
}

func adoptHybridHits(out *KBSearchResult, in KBSearchInput, cands []hybridCandidate, topK int) {
	for _, c := range cands {
		if !adoptOneHit(out, in, c.hit, c.fused) {
			continue
		}
		if len(out.Hits) >= topK {
			break
		}
	}
}

func adoptOneHit(out *KBSearchResult, in KBSearchInput, hit KBSearchHit, score float64) bool {
	reason := effectivityDrop(hit.Chunk.LocatorJSON, in.TailNo, in.AsOf, in.DocType)
	if reason != "" {
		out.Explanation.NotAdopted = append(out.Explanation.NotAdopted, reason)
		return false
	}
	if status, _ := locatorString(hit.Chunk.LocatorJSON, "status"); strings.EqualFold(status, "uncontrolled") {
		if !containsReason(out.Explanation.Reasons, "uncontrolled document") {
			out.Explanation.Reasons = append(out.Explanation.Reasons, "uncontrolled document")
		}
	}
	quote := hit.Chunk.Body
	if utf8.RuneCountInString(quote) > 240 {
		quote = trimRunes(quote, 240)
	}
	rev, _ := locatorString(hit.Chunk.LocatorJSON, "revision")
	out.Hits = append(out.Hits, KBCitedHit{
		ExpertID: in.ExpertID,
		DocID:    hit.Document.DocumentID,
		Revision: rev,
		Locator:  hit.Chunk.LocatorJSON,
		Quote:    quote,
		Score:    score,
	})
	return true
}

// Cite re-validates that quote is a prefix of the stored chunk body.
func (s *KBService) Cite(ctx context.Context, hit KBCitedHit) (KBCitedHit, error) {
	if s == nil || s.uow == nil {
		return KBCitedHit{}, ErrServiceUnavailable
	}
	if strings.TrimSpace(hit.Locator) == "" || strings.TrimSpace(hit.Quote) == "" {
		return KBCitedHit{}, ErrPayloadInvalid
	}
	var chunkID string
	var loc map[string]any
	if json.Unmarshal([]byte(hit.Locator), &loc) == nil {
		chunkID, _ = loc["chunkId"].(string)
	}
	if chunkID == "" {
		return hit, nil
	}
	err := s.uow.TransactKB(ctx, func(tx KBTx) error {
		got, err := tx.GetKBChunk(chunkID)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(got.Body, hit.Quote) && !strings.HasPrefix(strings.TrimSpace(got.Body), strings.TrimSpace(hit.Quote)) {
			return ErrPayloadInvalid
		}
		return nil
	})
	if err != nil {
		return KBCitedHit{}, err
	}
	return hit, nil
}

func effectivityDrop(locatorJSON, tailNo, asOf, docType string) string {
	var loc map[string]any
	if json.Unmarshal([]byte(locatorJSON), &loc) != nil {
		return ""
	}
	if docType != "" {
		got, _ := loc["docType"].(string)
		if got != "" && !strings.EqualFold(got, docType) {
			return "effectivity docType"
		}
	}
	if tailNo != "" {
		if tails, ok := loc["tails"].([]any); ok && len(tails) > 0 {
			okTail := false
			for _, t := range tails {
				s, _ := t.(string)
				if s == tailNo {
					okTail = true
					break
				}
			}
			if !okTail {
				return "effectivity tail"
			}
		}
	}
	from, _ := loc["effFrom"].(string)
	to, _ := loc["effTo"].(string)
	if asOf != "" && ((from != "" && asOf < from) || (to != "" && asOf > to)) {
		return "effectivity date"
	}
	return ""
}

func containsReason(reasons []string, want string) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}

func locatorString(raw, key string) (string, bool) {
	var loc map[string]any
	if json.Unmarshal([]byte(raw), &loc) != nil {
		return "", false
	}
	s, ok := loc[key].(string)
	return s, ok
}

// KnowledgeStats is the expert.knowledge.get projection.
type KnowledgeStats struct {
	CollectionID  string `json:"collectionId"`
	DocumentCount int    `json:"documentCount"`
	ReadyCount    int    `json:"readyCount"`
	ChunkCount    int    `json:"chunkCount"`
	NodeCount     int    `json:"nodeCount"`
	MemoryCount   int    `json:"memoryCount"`
	Missing       bool   `json:"missing"`
}

// KnowledgeGet answers collection counters for one expert.
func (s *KBService) KnowledgeGet(ctx context.Context, expertID string) (KnowledgeStats, error) {
	if s == nil || s.uow == nil {
		return KnowledgeStats{}, ErrServiceUnavailable
	}
	var out KnowledgeStats
	err := s.uow.TransactKB(ctx, func(tx KBTx) error {
		coll, ok, err := tx.GetKBCollectionByScope(ExpertScopeID(expertID))
		if err != nil {
			return err
		}
		if !ok {
			out.Missing = true
			return nil
		}
		out.CollectionID = coll.CollectionID
		docs, ready, chunks, err := tx.CountKBStats(coll.CollectionID)
		if err != nil {
			return err
		}
		out.DocumentCount, out.ReadyCount, out.ChunkCount = docs, ready, chunks
		return nil
	})
	return out, err
}
