package m8app

import (
	"context"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/gateway"
)

func (s *KBService) embedChunksAfterCommit(ctx context.Context, chunks []m8core.KBChunk) {
	if s == nil || s.embedder == nil || s.uow == nil || len(chunks) == 0 {
		return
	}
	ids := make([]string, 0, len(chunks))
	bodies := make([]string, 0, len(chunks))
	for _, c := range chunks {
		if strings.TrimSpace(c.ChunkID) == "" || strings.TrimSpace(c.Body) == "" {
			continue
		}
		ids = append(ids, c.ChunkID)
		bodies = append(bodies, c.Body)
	}
	if len(bodies) == 0 {
		return
	}
	vecs, err := s.embedder(ctx, bodies)
	if err != nil || len(vecs) != len(bodies) {
		return
	}
	_ = s.uow.TransactKB(ctx, func(tx KBTx) error {
		for i, id := range ids {
			blob := gateway.EncodeEmbeddingBLOB(vecs[i])
			if len(blob) == 0 {
				continue
			}
			if err := tx.UpdateKBChunkEmbedding(id, blob); err != nil {
				return err
			}
		}
		return nil
	})
}
