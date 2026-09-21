package skillapp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

// EnsureCatalogPublished resumes an interrupted catalog install without
// publishing a same-name draft whose executable content was changed locally.
func (s *Service) EnsureCatalogPublished(ctx context.Context, templateID string) (skill.Skill, error) {
	var tpl *CatalogTemplate
	for i := range catalogTemplates {
		if catalogTemplates[i].ID == templateID {
			tpl = &catalogTemplates[i]
			break
		}
	}
	if tpl == nil {
		return skill.Skill{}, ErrTemplateUnknown
	}
	sk, err := s.InstallFromCatalog(ctx, templateID)
	if errors.Is(err, ErrTemplateInstalled) {
		existing, readErr := s.GetByNameVersion(ctx, tpl.Name, tpl.Version)
		if readErr != nil {
			return skill.Skill{}, readErr
		}
		sk = *existing
		err = nil
	}
	if err != nil {
		return sk, err
	}
	var actual, expected any
	if json.Unmarshal([]byte(sk.ManifestJSON), &actual) != nil || json.Unmarshal([]byte(manifestFor(*tpl)), &expected) != nil || !reflect.DeepEqual(actual, expected) || sk.EntryPoint != tpl.EntryPoint || !reflect.DeepEqual(sk.Permissions, tpl.Permissions) {
		return sk, ErrSkillVersionConflict
	}
	if sk.Status == skill.SkillStatusPublished {
		return sk, nil
	}
	if sk.Status != skill.SkillStatusDraft {
		return sk, ErrInvalidTransition
	}
	// Use the checked revision directly; a concurrent draft edit must not be
	// read afresh and silently approved by Publish.
	if err = s.write.UpdateSkillStatus(ctx, sk.ID, string(skill.SkillStatusPublished), sk.Rev); err != nil {
		return sk, err
	}
	updated, err := s.Get(ctx, sk.ID)
	if err != nil {
		return sk, err
	}
	return *updated, nil
}

func catalogTemplateByID(templateID string) *CatalogTemplate {
	for i := range catalogTemplates {
		if catalogTemplates[i].ID == templateID {
			return &catalogTemplates[i]
		}
	}
	return nil
}

func skillUsableForPack(sk skill.Skill) bool {
	return sk.Status == skill.SkillStatusPublished || sk.Status == skill.SkillStatusDraft
}

func (s *Service) adoptExistingSkill(ctx context.Context, tpl CatalogTemplate) (skill.Skill, error) {
	if existing, err := s.GetByNameVersion(ctx, tpl.Name, tpl.Version); err == nil && existing != nil && skillUsableForPack(*existing) {
		return *existing, nil
	}
	listed, err := s.List(ctx, "")
	if err != nil {
		return skill.Skill{}, err
	}
	key := SkillNameKey(tpl.Name)
	for _, item := range listed {
		if SkillNameKey(item.Name) != key {
			continue
		}
		if skillUsableForPack(item) {
			return item, nil
		}
	}
	return skill.Skill{}, ErrSkillNotFound
}

// EnsureCatalogAvailable publishes the catalog template when possible, but a
// pack must still succeed when the library already has the same skill with a
// local or imported revision.
func (s *Service) EnsureCatalogAvailable(ctx context.Context, templateID string) (skill.Skill, error) {
	sk, err := s.EnsureCatalogPublished(ctx, templateID)
	if err == nil {
		return sk, nil
	}
	tpl := catalogTemplateByID(templateID)
	if tpl == nil {
		return sk, err
	}
	if adopted, adoptErr := s.adoptExistingSkill(ctx, *tpl); adoptErr == nil {
		return adopted, nil
	}
	return sk, err
}
