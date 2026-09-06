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
