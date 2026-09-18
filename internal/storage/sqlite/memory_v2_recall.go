package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/domain/token"
)

func (s *Store) HybridRecallCanonical(ctx context.Context, q m8core.HybridRecallQuery) (m8core.HybridRecallResult, error) {
	return hybridRecallCanonical(ctx, s.db, q)
}

func (r *AgentRuntimeRepository) HybridRecallCanonical(ctx context.Context, q m8core.HybridRecallQuery) (m8core.HybridRecallResult, error) {
	return hybridRecallCanonical(ctx, r.db, q)
}

type recallDoc struct {
	FactID        string
	Version       int64
	Kind          string
	Text          string
	UpdatedAt     time.Time
	KeywordRank   int
	TemporalRank  int
	DenseRank     int
	RelationRank  int
	KeywordScore  float64
	TemporalScore float64
	DenseScore    float64
	RelationScore float64
	FeedbackScore float64
	Fused         float64
}

func hybridRecallCanonical(ctx context.Context, db *sql.DB, q m8core.HybridRecallQuery) (m8core.HybridRecallResult, error) {
	out := m8core.HybridRecallResult{TraceID: ulid.Make().String(), TokenMode: "estimated"}
	if q.SubjectID == "" || strings.TrimSpace(q.Query) == "" {
		return out, nil
	}
	if q.ScopeKind == "" {
		q.ScopeKind = "user"
	}
	if q.ScopeKind == "user" && q.ScopeID == "" {
		q.ScopeID = q.SubjectID
	}
	if q.TopK < 1 {
		q.TopK = 6
	}
	deadline := m8core.MemoryRecallDeadline
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	if m8core.SkipGenericRecall(q.Query) {
		out.Degraded = "skip_generic"
		return out, persistRecallHits(ctx, db, out)
	}
	docs, err := loadRecallDocs(ctx, db, q)
	if err != nil {
		return out, err
	}
	if m8core.ProfileOnlyRecall(q.Query) {
		filtered := docs[:0]
		for _, d := range docs {
			if d.Kind == m8core.MemoryKindProfile {
				filtered = append(filtered, d)
			}
		}
		docs = filtered
	}
	if len(docs) == 0 {
		out.Degraded = "empty"
		return out, persistRecallHits(ctx, db, out)
	}
	applyKeywordRanks(q.Query, docs)
	applyTemporalRanks(q.Query, docs)
	denseIncomplete, degraded := applyDenseRanks(ctx, db, q, docs)
	out.DenseIncomplete = denseIncomplete
	out.Degraded = degraded
	applyRelationRanks(ctx, db, q, docs)
	applyFeedbackRanks(ctx, db, docs)
	rrfMerge(docs)
	selected := mmrSelect(docs, q.TopK*3)
	budget := q.BudgetTokens
	if budget <= 0 {
		budget = m8core.MemoryTotalBudget(4000, q.Companion)
	}
	out.BudgetTokens = budget
	tokenMode := "estimated"
	adopted := 0
	for i := range selected {
		hit := selected[i]
		serialized := m8core.SerializeMemoryInject(hit.FactID, hit.Version, hit.Text)
		counted := token.CountTokensWithMode(q.ModelID, serialized)
		tokenMode = counted.Mode
		reason := "fused"
		keep := adopted < q.TopK && out.UsedTokens+counted.Tokens <= budget
		if !keep && adopted == 0 && counted.Tokens > budget {
			reason = "over_budget"
		} else if !keep {
			reason = "dropped_budget"
		} else {
			adopted++
			out.UsedTokens += counted.Tokens
		}
		out.Hits = append(out.Hits, m8core.HybridRecallHit{
			FactID:         hit.FactID,
			Version:        hit.Version,
			Kind:           hit.Kind,
			Text:           hit.Text,
			KeywordScore:   hit.KeywordScore,
			DenseScore:     hit.DenseScore,
			TemporalScore:  hit.TemporalScore,
			RelationScore:  hit.RelationScore,
			FeedbackScore:  hit.FeedbackScore,
			FusedScore:     hit.Fused,
			TokenCount:     counted.Tokens,
			TokenMode:      counted.Mode,
			Adopted:        keep && reason == "fused",
			ReasonCode:     reason,
			SerializedText: serialized,
		})
		if keep {
			out.Hits[len(out.Hits)-1].Adopted = true
		}
	}
	out.TokenMode = tokenMode
	if err := persistRecallHits(ctx, db, out); err != nil {
		return out, err
	}
	return out, nil
}

func loadRecallDocs(ctx context.Context, db *sql.DB, q m8core.HybridRecallQuery) ([]*recallDoc, error) {
	if q.AsOf != nil {
		return loadHistoricalRecallDocs(ctx, db, q)
	}
	query := `SELECT h.fact_id,h.current_version,v.kind,b.canonical_text,h.updated_at
		FROM memory_fact_heads h
		JOIN memory_content_versions v ON v.fact_id=h.fact_id AND v.fact_version=h.current_version
		JOIN memory_content_bodies b ON b.fact_id=h.fact_id AND b.fact_version=h.current_version
		WHERE h.subject_id=? AND h.scope_kind=? AND h.scope_id=? AND h.is_forgotten=0`
	args := []any{q.SubjectID, q.ScopeKind, q.ScopeID}
	if q.Kind != "" {
		query += ` AND v.kind=?`
		args = append(args, q.Kind)
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*recallDoc
	for rows.Next() {
		var d recallDoc
		var updated string
		if err = rows.Scan(&d.FactID, &d.Version, &d.Kind, &d.Text, &updated); err != nil {
			return nil, err
		}
		d.UpdatedAt = parseRecallTime(updated)
		out = append(out, &d)
	}
	return out, rows.Err()
}

func loadHistoricalRecallDocs(ctx context.Context, db *sql.DB, q m8core.HybridRecallQuery) ([]*recallDoc, error) {
	asOf := q.AsOf.UTC().Format(time.RFC3339Nano)
	rows, err := db.QueryContext(ctx, `SELECT v.fact_id,v.fact_version,v.kind,b.canonical_text,v.created_at
		FROM memory_content_versions v
		JOIN memory_fact_heads h ON h.fact_id=v.fact_id
		JOIN memory_content_bodies b ON b.fact_id=v.fact_id AND b.fact_version=v.fact_version
		WHERE h.subject_id=? AND h.scope_kind=? AND h.scope_id=? AND h.is_forgotten=0
		  AND v.created_at<=?
		  AND NOT EXISTS (
		    SELECT 1 FROM memory_fact_supersessions s
		    WHERE s.fact_id=v.fact_id AND s.old_version=v.fact_version AND s.effective_at<=?
		  )`, q.SubjectID, q.ScopeKind, q.ScopeID, asOf, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*recallDoc
	for rows.Next() {
		var d recallDoc
		var created string
		if err = rows.Scan(&d.FactID, &d.Version, &d.Kind, &d.Text, &created); err != nil {
			return nil, err
		}
		d.UpdatedAt = parseRecallTime(created)
		out = append(out, &d)
	}
	return out, rows.Err()
}

func applyKeywordRanks(query string, docs []*recallDoc) {
	type scored struct {
		doc   *recallDoc
		score float64
	}
	var ranked []scored
	for _, d := range docs {
		score := keywordScore(query, d.Text)
		d.KeywordScore = score
		if score > 0 {
			ranked = append(ranked, scored{d, score})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].doc.FactID < ranked[j].doc.FactID
	})
	limit := m8core.MemoryRecallFTSLimit
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	for i, item := range ranked {
		item.doc.KeywordRank = i + 1
	}
}

func applyTemporalRanks(query string, docs []*recallDoc) {
	historic := strings.Contains(query, "以前") || strings.Contains(query, "曾经") || strings.Contains(query, "去年") || strings.Contains(query, "当时")
	current := strings.Contains(query, "现在") || strings.Contains(query, "目前") || strings.Contains(query, "当前")
	type scored struct {
		doc   *recallDoc
		score float64
	}
	now := time.Now().UTC()
	var ranked []scored
	for _, d := range docs {
		ageHours := now.Sub(d.UpdatedAt).Hours()
		if ageHours < 0 {
			ageHours = 0
		}
		fresh := 1.0 - ageHours/(365*24)
		if fresh < 0 {
			fresh = 0
		}
		score := fresh
		if historic {
			score = 1 - fresh
		}
		if current && (strings.Contains(d.Text, "现在") || strings.Contains(d.Text, "目前")) {
			score += 0.25
		}
		if historic && (strings.Contains(d.Text, "以前") || strings.Contains(d.Text, "曾经")) {
			score += 0.25
		}
		d.TemporalScore = score
		ranked = append(ranked, scored{d, score})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].doc.FactID < ranked[j].doc.FactID
	})
	limit := m8core.MemoryRecallTemporalLimit
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	for i, item := range ranked {
		item.doc.TemporalRank = i + 1
	}
}

func applyDenseRanks(ctx context.Context, db *sql.DB, q m8core.HybridRecallQuery, docs []*recallDoc) (incomplete bool, degraded string) {
	if len(q.QueryVector) == 0 {
		return false, "no_embedding"
	}
	start := time.Now()
	type scored struct {
		doc   *recallDoc
		score float64
	}
	var ranked []scored
	space := q.EmbeddingSpace
	if space == "" {
		space = "default"
	}
	for _, d := range docs {
		if time.Since(start) > m8core.MemoryRecallDenseBudget || ctx.Err() != nil {
			incomplete = true
			break
		}
		var blob []byte
		var dims int
		err := db.QueryRowContext(ctx, `SELECT vector_blob,dimensions FROM memory_embeddings
			WHERE fact_id=? AND fact_version=? AND embedding_space_id=? AND state='ready'`, d.FactID, d.Version, space).
			Scan(&blob, &dims)
		if err != nil {
			continue
		}
		vec, err := DecodeEmbeddingBLOB(blob, dims)
		if err != nil || len(vec) != len(q.QueryVector) {
			continue
		}
		score := cosine(q.QueryVector, vec)
		d.DenseScore = score
		ranked = append(ranked, scored{d, score})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].doc.FactID < ranked[j].doc.FactID
	})
	limit := m8core.MemoryRecallDenseLimit
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	for i, item := range ranked {
		item.doc.DenseRank = i + 1
	}
	if len(ranked) == 0 && degraded == "" {
		degraded = "no_embedding"
	}
	return incomplete, degraded
}

func applyRelationRanks(ctx context.Context, db *sql.DB, q m8core.HybridRecallQuery, docs []*recallDoc) {
	index := map[string]*recallDoc{}
	for _, d := range docs {
		index[d.FactID] = d
	}
	rows, err := db.QueryContext(ctx, `SELECT fact_id FROM memory_relations WHERE state='active' LIMIT 200`)
	if err != nil {
		return
	}
	defer rows.Close()
	rank := 1
	for rows.Next() && rank <= m8core.MemoryRecallTemporalLimit {
		var factID string
		if err := rows.Scan(&factID); err != nil {
			return
		}
		if d, ok := index[factID]; ok && d.RelationRank == 0 {
			d.RelationRank = rank
			d.RelationScore = 1.0 / float64(rank)
			rank++
		}
	}
}

func applyFeedbackRanks(ctx context.Context, db *sql.DB, docs []*recallDoc) {
	if len(docs) == 0 {
		return
	}
	index := map[string]*recallDoc{}
	args := make([]any, 0, len(docs)*2)
	placeholders := make([]string, 0, len(docs))
	for _, d := range docs {
		key := d.FactID + ":" + strconv.FormatInt(d.Version, 10)
		index[key] = d
		placeholders = append(placeholders, "(?,?)")
		args = append(args, d.FactID, d.Version)
	}
	q := `SELECT fact_id, fact_version,
		SUM(CASE WHEN outcome IN ('helpful','used') THEN 1 ELSE 0 END),
		SUM(CASE WHEN outcome IN ('contradicted','user_corrected') THEN 1 ELSE 0 END)
		FROM memory_feedback_events
		WHERE (fact_id, fact_version) IN (` + strings.Join(placeholders, ",") + `)
		GROUP BY fact_id, fact_version`
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var factID string
		var version, helpful, contradicted int64
		if err := rows.Scan(&factID, &version, &helpful, &contradicted); err != nil {
			return
		}
		d, ok := index[factID+":"+strconv.FormatInt(version, 10)]
		if !ok {
			continue
		}
		d.FeedbackScore = 0.01*float64(helpful) - 0.01*float64(contradicted)
	}
}

func rrfMerge(docs []*recallDoc) {
	k := float64(m8core.MemoryRecallRRFK)
	for _, d := range docs {
		if d.KeywordRank > 0 {
			d.Fused += 1.0 / (k + float64(d.KeywordRank))
		}
		if d.DenseRank > 0 {
			d.Fused += 1.0 / (k + float64(d.DenseRank))
		}
		if d.TemporalRank > 0 {
			d.Fused += 1.0 / (k + float64(d.TemporalRank))
		}
		if d.RelationRank > 0 {
			d.Fused += 1.0 / (k + float64(d.RelationRank))
		}
		if d.FeedbackScore != 0 {
			score := d.FeedbackScore
			if score > 0.03 {
				score = 0.03
			}
			if score < -0.03 {
				score = -0.03
			}
			d.Fused += score
		}
	}
	sort.SliceStable(docs, func(i, j int) bool {
		if docs[i].Fused != docs[j].Fused {
			return docs[i].Fused > docs[j].Fused
		}
		return docs[i].FactID < docs[j].FactID
	})
}

func mmrSelect(docs []*recallDoc, limit int) []*recallDoc {
	if limit <= 0 || len(docs) <= limit {
		return docs
	}
	lambda := m8core.MemoryRecallMMRLambda
	var selected []*recallDoc
	remaining := append([]*recallDoc(nil), docs...)
	for len(selected) < limit && len(remaining) > 0 {
		bestI := 0
		bestScore := math.Inf(-1)
		for i, cand := range remaining {
			penalty := 0.0
			for _, got := range selected {
				sim := keywordScore(cand.Text, got.Text)
				if sim > penalty {
					penalty = sim
				}
			}
			score := lambda*cand.Fused - (1-lambda)*penalty
			if score > bestScore {
				bestScore = score
				bestI = i
			}
		}
		selected = append(selected, remaining[bestI])
		remaining = append(remaining[:bestI], remaining[bestI+1:]...)
	}
	return selected
}

func persistRecallHits(ctx context.Context, db *sql.DB, result m8core.HybridRecallResult) error {
	if result.TraceID == "" {
		return nil
	}
	for i, hit := range result.Hits {
		kw := sql.NullFloat64{Float64: hit.KeywordScore, Valid: hit.KeywordScore > 0}
		dense := sql.NullFloat64{Float64: hit.DenseScore, Valid: hit.DenseScore > 0}
		temporal := sql.NullFloat64{Float64: hit.TemporalScore, Valid: hit.TemporalScore > 0}
		rel := sql.NullFloat64{Float64: hit.RelationScore, Valid: hit.RelationScore > 0}
		fb := sql.NullFloat64{Float64: hit.FeedbackScore, Valid: hit.FeedbackScore != 0}
		adopted := 0
		if hit.Adopted {
			adopted = 1
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO memory_recall_hit_details(
			trace_id,rank,fact_id,fact_version,keyword_score,dense_score,temporal_score,relation_score,feedback_score,fused_score,adopted,reason_code,token_count)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			result.TraceID, i+1, hit.FactID, hit.Version, kw, dense, temporal, rel, fb, hit.FusedScore, adopted, hit.ReasonCode, hit.TokenCount); err != nil {
			return err
		}
	}
	return nil
}

func keywordScore(query, body string) float64 {
	qg, bg := recallTrigrams(query), recallTrigrams(body)
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

func recallTrigrams(s string) map[string]bool {
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
		g := string(runes[i : i+3])
		if !unicode.IsSpace(runes[i]) {
			out[g] = true
		}
	}
	return out
}

func parseRecallTime(raw string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts
		}
	}
	return time.Time{}
}

// DecodeEmbeddingBLOB rejects NaN/Inf and dimension mismatches.
func DecodeEmbeddingBLOB(blob []byte, dims int) ([]float32, error) {
	if dims < 1 || len(blob) != dims*4 {
		return nil, fmt.Errorf("embedding dimensions invalid")
	}
	out := make([]float32, dims)
	for i := 0; i < dims; i++ {
		bits := binary.LittleEndian.Uint32(blob[i*4:])
		v := math.Float32frombits(bits)
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil, fmt.Errorf("embedding contains NaN/Inf")
		}
		out[i] = v
	}
	return out, nil
}

func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		av, bv := float64(a[i]), float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
