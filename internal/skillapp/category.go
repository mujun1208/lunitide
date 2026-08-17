// M10 skill-center category service: the 12-category taxonomy mapping with
// resolution priority manual > manifest > keyword and an "other" fallback.
// The mapping store is optional: without it skills still resolve categories
// deterministically from their own row.
package skillapp

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

var (
	// ErrCategoryInvalid is returned for identifiers outside the 12 fixed
	// categories.
	ErrCategoryInvalid = errors.New("skill category invalid")
	// ErrCategoryStoreUnavailable is returned when the mapping store is not
	// wired (skill center degraded, reads still possible).
	ErrCategoryStoreUnavailable = errors.New("skill category store unavailable")
)

// CategoryReader reads sk_category_map rows (migration 0073).
type CategoryReader interface {
	GetSkillCategoryRow(ctx context.Context, skillID string) (skill.CategoryMap, error)
	ListSkillCategories(ctx context.Context) ([]skill.CategoryMap, error)
}

// CategoryWriter persists category mappings.
type CategoryWriter interface {
	UpsertSkillCategoryManual(ctx context.Context, skillID string, category skill.Category) (skill.CategoryMap, error)
	SeedSkillCategory(ctx context.Context, skillID string, category skill.Category, source skill.CategorySource) error
}

// CategoryResolution is the effective (category, source) for one skill.
type CategoryResolution struct {
	Category skill.Category       `json:"category"`
	Source   skill.CategorySource `json:"categorySource"`
}

// SkillCategoryView pairs one skill with its resolved category for list
// projections (chips, counts, badges).
type SkillCategoryView struct {
	Skill    skill.Skill          `json:"skill"`
	Category skill.Category       `json:"category"`
	Source   skill.CategorySource `json:"categorySource"`
}

// SetCategoryStore wires the optional M10 category mapping store.
func (s *Service) SetCategoryStore(r CategoryReader, w CategoryWriter) {
	if s != nil {
		s.catRead, s.catWrite = r, w
	}
}

// ResolveCategory applies manual > manifest > keyword with the other
// fallback. Manifest and keyword resolutions are pure functions of the
// skill row, so only manual stored mappings can override them.
func ResolveCategory(sk skill.Skill, stored *skill.CategoryMap) CategoryResolution {
	if stored != nil && stored.Source == skill.CategorySourceManual && skill.ValidCategory(string(stored.Category)) {
		return CategoryResolution{Category: stored.Category, Source: skill.CategorySourceManual}
	}
	if c, ok := skill.CategoryFromManifest(sk.ManifestJSON); ok {
		return CategoryResolution{Category: c, Source: skill.CategorySourceManifest}
	}
	if c, ok := skill.CategoryFromKeywords(sk.Name+" "+sk.DisplayName, sk.Description); ok {
		return CategoryResolution{Category: c, Source: skill.CategorySourceKeyword}
	}
	// Rule-table fallback: everything unmatched lands in other.
	return CategoryResolution{Category: skill.CategoryOther, Source: skill.CategorySourceKeyword}
}

// SetCategory manually assigns one of the 12 fixed categories. Manual
// assignments always win over manifest declarations and keyword rules.
func (s *Service) SetCategory(ctx context.Context, skillID string, category skill.Category) (skill.CategoryMap, error) {
	if s == nil || s.catWrite == nil {
		return skill.CategoryMap{}, ErrCategoryStoreUnavailable
	}
	if !skill.ValidCategory(string(category)) {
		return skill.CategoryMap{}, ErrCategoryInvalid
	}
	if _, err := s.Get(ctx, skillID); err != nil {
		return skill.CategoryMap{}, err
	}
	return s.catWrite.UpsertSkillCategoryManual(ctx, skillID, category)
}

// CategoryFor resolves one skill's effective category.
func (s *Service) CategoryFor(ctx context.Context, sk skill.Skill) (CategoryResolution, error) {
	if s == nil {
		return ResolveCategory(sk, nil), nil
	}
	var stored *skill.CategoryMap
	if s.catRead != nil {
		m, err := s.catRead.GetSkillCategoryRow(ctx, sk.ID)
		if err != nil {
			return CategoryResolution{}, err
		}
		if m.SkillID != "" {
			stored = &m
		}
	}
	return ResolveCategory(sk, stored), nil
}

// ListWithCategories lists skills with each resolved category.
func (s *Service) ListWithCategories(ctx context.Context, status skill.SkillStatus) ([]SkillCategoryView, error) {
	items, err := s.List(ctx, status)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]skill.CategoryMap)
	if s != nil && s.catRead != nil {
		all, err := s.catRead.ListSkillCategories(ctx)
		if err != nil {
			return nil, err
		}
		for _, m := range all {
			byID[m.SkillID] = m
		}
	}
	views := make([]SkillCategoryView, len(items))
	for i, sk := range items {
		var stored *skill.CategoryMap
		if m, ok := byID[sk.ID]; ok {
			stored = &m
		}
		r := ResolveCategory(sk, stored)
		views[i] = SkillCategoryView{Skill: sk, Category: r.Category, Source: r.Source}
	}
	return views, nil
}

// seedCategoryFor persists the computed mapping of a freshly created skill
// unless a row already exists (INSERT OR IGNORE keeps manual decisions).
func (s *Service) seedCategoryFor(ctx context.Context, sk skill.Skill) {
	if s == nil || s.catWrite == nil {
		return
	}
	r := ResolveCategory(sk, nil)
	_ = s.catWrite.SeedSkillCategory(ctx, sk.ID, r.Category, r.Source)
}
