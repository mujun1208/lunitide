package app

import (
	"context"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/secretlease"
)

var errKBEmbedUnavailable = errors.New("embedding catalog unavailable")

func (e *Engine) embedKBTexts(ctx context.Context, texts []string) ([][]float32, error) {
	if e == nil || e.providers == nil || len(texts) == 0 {
		return nil, errKBEmbedUnavailable
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return nil, err
	}
	boundProviderID, boundModelID := e.resolveRole(ctx, "embed")
	catalog := provider.CatalogForKind(items, provider.KindEmbedding)
	if strings.TrimSpace(boundModelID) != "" {
		filtered := catalog[:0]
		for _, entry := range catalog {
			if (boundProviderID == "" || entry.Provider.ID == boundProviderID) && entry.Model.ModelID == boundModelID {
				filtered = append(filtered, entry)
			}
		}
		catalog = filtered
	}
	for _, entry := range catalog {
		if entry.Provider.Protocol != provider.ProtocolOpenAICompatible {
			continue
		}
		var out [][]float32
		leaseErr := e.withProviderLease(ctx, entry.Provider, secretlease.OperationProviderTest, func(op context.Context, secret []byte) error {
			a, adapterErr := e.adapter(op, entry.Provider)
			if adapterErr != nil {
				return adapterErr
			}
			emb, ok := a.(gateway.Embedder)
			if !ok {
				return errKBEmbedUnavailable
			}
			vecs, embedErr := emb.Embed(op, secret, entry.Model.ModelID, texts)
			if embedErr != nil {
				return embedErr
			}
			out = vecs
			return nil
		})
		if leaseErr == nil && len(out) == len(texts) {
			return out, nil
		}
	}
	return nil, errKBEmbedUnavailable
}

func providerTestUsesEmbed(p provider.Provider, modelID string) bool {
	return modelByID(p, strings.TrimSpace(modelID)).EffectiveKind() == provider.KindEmbedding
}
