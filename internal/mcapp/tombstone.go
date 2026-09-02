package mcapp

import (
	"context"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
)

// ── mc.tombstone.check ──────────────────────────────────────────────────────

// TombstoneReport is the outcome of the catalog revocation sweep.
type TombstoneReport struct {
	Fresh   bool             `json:"fresh"`
	Revoked []RevokedSuspect `json:"revoked"`
	Drifted []DriftedItem    `json:"drifted"`
}

// RevokedSuspect links a cached-but-delisted catalog item to installed
// endpoints matching its install fingerprint.
type RevokedSuspect struct {
	MarketItemID string   `json:"marketItemId"`
	Name         string   `json:"name"`
	EndpointIDs  []string `json:"endpointIds"`
}

// DriftedItem is a catalog item whose digest changed upstream.
type DriftedItem struct {
	MarketItemID   string `json:"marketItemId"`
	Name           string `json:"name"`
	CachedDigest   string `json:"cachedDigest"`
	RegistryDigest string `json:"registryDigest"`
}

// TombstoneCheck compares the live registry against the insert-only cache:
// items missing upstream are revocation suspects; digest changes are
// drift. Suspects are matched to installed market endpoints by
// fingerprint so the UI can offer uninstall.
func (s *Service) TombstoneCheck(ctx context.Context) (TombstoneReport, error) {
	if s == nil || s.uow == nil {
		return TombstoneReport{}, ErrMcNotFound
	}
	report := TombstoneReport{Fresh: true, Revoked: []RevokedSuspect{}, Drifted: []DriftedItem{}}
	regItems, err := s.registry(ctx)
	if err != nil {
		report.Fresh = false
		return report, nil
	}
	regByID := make(map[string]m7flow.McpMarketItem, len(regItems))
	for _, it := range regItems {
		regByID[it.ID] = it
	}
	var cached []m7flow.McpMarketItem
	var endpoints []m7flow.McpEndpointConfig
	uerr := s.uow.TransactMc(ctx, func(tx Tx) error {
		list, err := tx.ListMcMarket("", "", "", 1000)
		cached = list
		if err != nil {
			return err
		}
		endpoints, err = tx.ListMcpEndpoints("")
		return err
	})
	if uerr != nil {
		return report, uerr
	}
	for _, it := range cached {
		reg, ok := regByID[it.ID]
		if !ok {
			suspect := RevokedSuspect{MarketItemID: it.ID, Name: it.Name, EndpointIDs: []string{}}
			cfg, perr := parseInstallConfig(it.InstallConfigJSON)
			if perr == nil {
				transport := cfg.Transport
				if transport == "" {
					if cfg.Command != "" {
						transport = m7flow.McpTransportStdio
					} else {
						transport = m7flow.McpTransportHTTPS
					}
				}
				for _, ep := range endpoints {
					if ep.Origin != m7flow.McpOriginMarket {
						continue
					}
					if ep.Transport != transport {
						continue
					}
					if (transport == m7flow.McpTransportStdio && ep.Command == cfg.Command) ||
						(transport == m7flow.McpTransportHTTPS && ep.URL == cfg.URL) {
						suspect.EndpointIDs = append(suspect.EndpointIDs, ep.EndpointID)
					}
				}
			}
			report.Revoked = append(report.Revoked, suspect)
			continue
		}
		if reg.CatalogDigest != it.CatalogDigest {
			report.Drifted = append(report.Drifted, DriftedItem{
				MarketItemID: it.ID, Name: it.Name,
				CachedDigest: it.CatalogDigest, RegistryDigest: reg.CatalogDigest,
			})
		}
	}
	return report, nil
}
