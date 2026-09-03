package m8app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

const defaultGrowthLadder = `[{"name":"知识积累","state":"next"}]`
const defaultGrowthCoverage = `{"docTypes":[],"gaps":[]}`

// GrowthPath is one expert_growth_paths row.
type GrowthPath struct {
	ExpertID        string `json:"expertId"`
	MissionSnapshot string `json:"missionSnapshot"`
	LadderJSON      string `json:"ladderJson"`
	CoverageJSON    string `json:"coverageJson"`
	UpdatedAt       string `json:"updatedAt"`
}

// GrowthService reads and refreshes expert growth paths.
type GrowthService struct {
	uow   KBUnitOfWork
	clock Clock
}

// NewGrowthService wires growth storage onto the KB unit of work.
func NewGrowthService(uow KBUnitOfWork) *GrowthService {
	return &GrowthService{uow: uow, clock: systemClock{}}
}

// SetClock substitutes the clock (tests).
func (s *GrowthService) SetClock(c Clock) { s.clock = c }

// Get answers the stored path without inserting.
func (s *GrowthService) Get(ctx context.Context, expertID string) (GrowthPath, bool, error) {
	if s == nil || s.uow == nil {
		return GrowthPath{}, false, ErrServiceUnavailable
	}
	var out GrowthPath
	var ok bool
	err := s.uow.TransactKB(ctx, func(tx KBTx) error {
		got, found, err := tx.GetGrowthPath(expertID)
		if err != nil {
			return err
		}
		out, ok = got, found
		return nil
	})
	return out, ok, err
}

// GetOrInit inserts the default ladder once, then answers the row.
func (s *GrowthService) GetOrInit(ctx context.Context, expertID, mission string) (GrowthPath, error) {
	if s == nil || s.uow == nil {
		return GrowthPath{}, ErrServiceUnavailable
	}
	if len(expertID) != 26 {
		return GrowthPath{}, ErrPayloadInvalid
	}
	mission = strings.TrimSpace(mission)
	if mission == "" {
		mission = "知识积累"
	}
	if len(mission) > 4096 {
		mission = mission[:4096]
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	var out GrowthPath
	err := s.uow.TransactKB(ctx, func(tx KBTx) error {
		got, ok, err := tx.GetGrowthPath(expertID)
		if err != nil {
			return err
		}
		if ok {
			out = got
			return nil
		}
		out = GrowthPath{
			ExpertID:        expertID,
			MissionSnapshot: mission,
			LadderJSON:      defaultGrowthLadder,
			CoverageJSON:    defaultGrowthCoverage,
			UpdatedAt:       now,
		}
		return tx.PutGrowthPath(out)
	})
	return out, err
}

// RefreshCoverage scans ready document locators into coverage.docTypes.
func (s *GrowthService) RefreshCoverage(ctx context.Context, expertID string) (GrowthPath, error) {
	if s == nil || s.uow == nil {
		return GrowthPath{}, ErrServiceUnavailable
	}
	if len(expertID) != 26 {
		return GrowthPath{}, ErrPayloadInvalid
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	var out GrowthPath
	err := s.uow.TransactKB(ctx, func(tx KBTx) error {
		got, ok, err := tx.GetGrowthPath(expertID)
		if err != nil {
			return err
		}
		if !ok {
			got = GrowthPath{
				ExpertID:        expertID,
				MissionSnapshot: "知识积累",
				LadderJSON:      defaultGrowthLadder,
				CoverageJSON:    defaultGrowthCoverage,
			}
		}
		coll, hasColl, err := tx.GetKBCollectionByScope(ExpertScopeID(expertID))
		if err != nil {
			return err
		}
		types := map[string]struct{}{}
		if hasColl {
			docs, err := tx.ListKBDocumentsByCollection(coll.CollectionID)
			if err != nil {
				return err
			}
			for _, d := range docs {
				if d.IndexState != m8core.KBIndexReady {
					continue
				}
				loc := parseSourceLocator(d.SourceLocator)
				if dt, _ := loc["docType"].(string); dt != "" {
					types[dt] = struct{}{}
				}
			}
		}
		docTypes := make([]string, 0, len(types))
		for k := range types {
			docTypes = append(docTypes, k)
		}
		gaps := []string{}
		if len(docTypes) == 0 {
			gaps = []string{"文档"}
		}
		cov, err := json.Marshal(map[string]any{"docTypes": docTypes, "gaps": gaps})
		if err != nil {
			return err
		}
		got.CoverageJSON = string(cov)
		got.UpdatedAt = now
		if err := tx.PutGrowthPath(got); err != nil {
			return err
		}
		out = got
		return nil
	})
	return out, err
}

// EnsureExpertFoundations creates KB collections and growth rows for
// colleague specialists and the three PM builtins. Market persona cards
// are skipped.
func EnsureExpertFoundations(ctx context.Context, experts *ExpertService, kb *KBService, growth *GrowthService) error {
	if experts == nil || kb == nil {
		return nil
	}
	listed, err := experts.List(ctx, ExpertFilter{})
	if err != nil {
		return err
	}
	missions := map[string]string{}
	for _, spec := range builtinExpertSpecs {
		missions[spec.name] = spec.six.Mission
	}
	for _, item := range ConversationExperts() {
		missions[item.Name] = item.SixSection.Mission
	}
	for _, row := range listed.Experts {
		mission, ok := missions[row.Name]
		if !ok {
			continue
		}
		if _, err := kb.EnsureExpertCollection(ctx, row.ExpertID); err != nil {
			return err
		}
		if growth != nil {
			if _, err := growth.GetOrInit(ctx, row.ExpertID, mission); err != nil {
				return err
			}
		}
	}
	return nil
}
