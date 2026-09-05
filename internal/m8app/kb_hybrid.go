package m8app

import (
	"math"
	"time"

	"github.com/lunitide/lunitide/internal/gateway"
)

const (
	hybridFTSWeight     = 0.5
	hybridCosWeight     = 0.4
	hybridRecencyWeight = 0.1
	hybridMMRLambda     = 0.7
	hybridIndexVersion  = "fts5+dense-v1"
	ftsIndexVersion     = "fts5-trigram"
)

type hybridCandidate struct {
	hit     KBSearchHit
	fts     float64
	cos     float64
	recency float64
	fused   float64
	vec     []float32
}

func decodeChunkVector(blob []byte) ([]float32, bool) {
	return gateway.DecodeEmbeddingBLOB(blob)
}

func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func recencyScore(createdAt string, now time.Time) float64 {
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return 0
	}
	years := now.Sub(t).Hours() / (24 * 365)
	if years < 0 {
		years = 0
	}
	return math.Exp(-years)
}

func fuseHybrid(fts []KBSearchHit, dense []KBChunkEmbedding, query []float32, now time.Time, topK int) []hybridCandidate {
	ftsSet := map[string]KBSearchHit{}
	for _, h := range fts {
		ftsSet[h.Chunk.ChunkID] = h
	}
	byID := map[string]*hybridCandidate{}
	add := func(hit KBSearchHit, blob []byte, fromFTS bool) {
		id := hit.Chunk.ChunkID
		if id == "" {
			return
		}
		cur, ok := byID[id]
		if !ok {
			cur = &hybridCandidate{hit: hit}
			byID[id] = cur
		} else if cur.hit.Chunk.Body == "" && hit.Chunk.Body != "" {
			cur.hit = hit
		}
		if fromFTS {
			cur.fts = 1
		}
		if vec, ok := decodeChunkVector(blob); ok {
			if cur.vec == nil {
				cur.vec = vec
			}
			if len(query) > 0 && len(vec) == len(query) {
				c := cosine(query, vec)
				cur.cos = (c + 1) / 2
			}
		}
		created := hit.Chunk.CreatedAt
		if created == "" {
			created = hit.Document.CreatedAt
		}
		cur.recency = recencyScore(created, now)
		cur.fused = hybridFTSWeight*cur.fts + hybridCosWeight*cur.cos + hybridRecencyWeight*cur.recency
	}
	for _, h := range fts {
		add(h, nil, true)
	}
	for _, row := range dense {
		hit := KBSearchHit{Chunk: row.Chunk, Document: row.Document, Score: 0}
		if existing, ok := ftsSet[row.Chunk.ChunkID]; ok {
			hit = existing
		}
		add(hit, row.Chunk.Embedding, false)
	}
	cands := make([]hybridCandidate, 0, len(byID))
	for _, c := range byID {
		cands = append(cands, *c)
	}
	return mmrSelect(cands, topK)
}

func mmrSelect(cands []hybridCandidate, topK int) []hybridCandidate {
	if topK < 1 {
		topK = 6
	}
	if len(cands) <= topK {
		sortHybrid(cands)
		return cands
	}
	selected := make([]hybridCandidate, 0, topK)
	used := make([]bool, len(cands))
	for len(selected) < topK {
		best := -1
		bestScore := math.Inf(-1)
		for i, c := range cands {
			if used[i] {
				continue
			}
			penalty := 0.0
			for _, s := range selected {
				if sim := cosine(c.vec, s.vec); sim > penalty {
					penalty = sim
				}
			}
			score := hybridMMRLambda*c.fused - (1-hybridMMRLambda)*penalty
			if score > bestScore {
				bestScore = score
				best = i
			}
		}
		if best < 0 {
			break
		}
		used[best] = true
		selected = append(selected, cands[best])
	}
	return selected
}

func sortHybrid(cands []hybridCandidate) {
	for i := 0; i < len(cands); i++ {
		for j := i + 1; j < len(cands); j++ {
			if cands[j].fused > cands[i].fused {
				cands[i], cands[j] = cands[j], cands[i]
			}
		}
	}
}
