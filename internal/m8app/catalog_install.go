package m8app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/domain/skill"
)

var (
	// ErrCatalogUnknown: the requested catalog id is not in the shipped roster.
	ErrCatalogUnknown = errors.New("m8app: catalog item unknown")
	// ErrCatalogInstalled: this catalog id (or its skill/expert) is already present.
	ErrCatalogInstalled = errors.New("m8app: catalog item already installed")
)

// CatalogSkillStore is the skill surface catalog install needs. skillapp.Service
// satisfies it; tests use an in-memory fake.
type CatalogSkillStore interface {
	GetByNameVersion(ctx context.Context, name, version string) (*skill.Skill, error)
	Create(ctx context.Context, sk skill.Skill) (skill.Skill, error)
	Publish(ctx context.Context, id string) error
}

// CatalogInstallResult is the expert.install / catalog install outcome.
type CatalogInstallResult struct {
	ID        string `json:"id"`
	Usage     string `json:"usage"`
	ExpertID  string `json:"expertId,omitempty"`
	SkillID   string `json:"skillId,omitempty"`
	SkillName string `json:"skillName,omitempty"`
}

// LookupCatalogItem answers one roster row by id.
func LookupCatalogItem(id string) (CatalogItem, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return CatalogItem{}, false
	}
	for _, item := range AgencyAgentsCatalog() {
		if item.ID == id {
			return item, true
		}
	}
	return CatalogItem{}, false
}

// InstallAgencyAgent materializes one catalog role: chat usage publishes a
// skill, project usage creates a local expert, both does each in turn.
func InstallAgencyAgent(ctx context.Context, experts *ExpertService, skills CatalogSkillStore, id string) (CatalogInstallResult, error) {
	item, ok := LookupCatalogItem(id)
	if !ok {
		return CatalogInstallResult{}, ErrCatalogUnknown
	}
	if item.NeedsChat() && skills == nil {
		return CatalogInstallResult{}, ErrServiceUnavailable
	}
	if item.NeedsProject() && experts == nil {
		return CatalogInstallResult{}, ErrServiceUnavailable
	}
	out := CatalogInstallResult{ID: item.ID, Usage: item.Usage}
	if item.NeedsChat() {
		skillID, skillName, err := installCatalogSkill(ctx, skills, item)
		if err != nil {
			return CatalogInstallResult{}, err
		}
		out.SkillID, out.SkillName = skillID, skillName
	}
	if item.NeedsProject() {
		expertID, err := installCatalogExpert(ctx, experts, item)
		if err != nil {
			return CatalogInstallResult{}, err
		}
		out.ExpertID = expertID
	}
	return out, nil
}

func installCatalogSkill(ctx context.Context, skills CatalogSkillStore, item CatalogItem) (string, string, error) {
	name := item.SkillName()
	version := item.Version
	if version == "" {
		version = "1.0.0"
	}
	if existing, err := skills.GetByNameVersion(ctx, name, version); err == nil && existing != nil {
		return "", "", ErrCatalogInstalled
	}
	display := item.DisplayName
	if display == "" {
		display = item.Name
	}
	desc := item.Description
	if len(desc) > 4096 {
		desc = string([]rune(desc)[:4096])
	}
	raw, err := json.Marshal(map[string]any{
		"catalogId": item.ID,
		"usage":     item.Usage,
		"prompt":    item.SixSection.Identity,
		"triggers":  []string{item.Name, display},
	})
	if err != nil {
		return "", "", err
	}
	created, err := skills.Create(ctx, skill.Skill{
		Name: name, DisplayName: display, Description: desc, Version: version,
		Permissions:  []skill.PermissionLevel{skill.PermissionReadOnly},
		EntryPoint:   "builtin://" + name,
		ManifestJSON: string(raw),
	})
	if err != nil {
		return "", "", err
	}
	if err := skills.Publish(ctx, created.ID); err != nil {
		return "", "", err
	}
	return created.ID, name, nil
}

func installCatalogExpert(ctx context.Context, experts *ExpertService, item CatalogItem) (string, error) {
	listed, err := experts.List(ctx, ExpertFilter{})
	if err != nil {
		return "", err
	}
	for _, row := range listed.Experts {
		if row.Name == item.Name && row.State != m8core.ExpertArchived {
			return "", ErrCatalogInstalled
		}
	}
	desc := item.Description
	if desc == "" {
		desc = item.DisplayName
	}
	if len(desc) > 2000 {
		desc = string([]rune(desc)[:2000])
	}
	version := item.Version
	if version == "" {
		version = "1.0.0"
	}
	res, err := experts.Create(ctx, CreateInput{
		Source: m8core.ExpertSourceLocal,
		Frontmatter: m8core.Frontmatter{
			Name: item.Name, Division: item.Division,
			Description: desc, Semver: version,
		},
		SixSection: item.SixSection,
		RequestID:  "catalog-" + item.ID,
		Actor:      "catalog-install",
	})
	if err != nil {
		if errors.Is(err, ErrExpertDuplicate) {
			return "", ErrCatalogInstalled
		}
		return "", err
	}
	if experts.skills != nil && len(item.PreferredSkills) > 0 {
		if err := experts.skills.SeedExpertSkillsIfEmpty(ctx, res.ExpertID, append([]string{}, item.PreferredSkills...)); err != nil {
			return "", err
		}
	}
	return res.ExpertID, nil
}
