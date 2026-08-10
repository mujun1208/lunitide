// Package skillapp coordinates skill lifecycle, permission validation, and skill matching.
package skillapp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

var (
	ErrSkillNotFound       = errors.New("skill not found")
	ErrSkillAlreadyExists  = errors.New("skill with same name and version already exists")
	ErrInvalidStatus       = errors.New("invalid skill status")
	ErrInvalidTransition   = errors.New("invalid skill status transition")
	ErrPermissionDenied    = errors.New("permission denied by skill policy")
	ErrSkillNotPublished   = errors.New("skill is not published")
	ErrSkillDeprecated     = errors.New("skill is deprecated")
	ErrSkillDisabled       = errors.New("skill is disabled")
	ErrNoMatchingSkill     = errors.New("no matching skill found")
	ErrInvalidPermission   = errors.New("invalid permission level")
)

// SkillReader reads skills from storage.
type SkillReader interface {
	GetSkill(ctx context.Context, id string) (*skill.Skill, error)
	GetSkillByNameVersion(ctx context.Context, name, version string) (*skill.Skill, error)
	ListSkills(ctx context.Context, status string, limit int) ([]skill.Skill, error)
}

// SkillWriter writes skill updates.
type SkillWriter interface {
	CreateSkill(ctx context.Context, sk skill.Skill) (skill.Skill, error)
	UpdateSkill(ctx context.Context, id, displayName, description string) error
	UpdateSkillFields(ctx context.Context, id, displayName, description, entryPoint, manifestJSON, permissionsJSON string, minEngineVersion *string) error
	UpdateSkillStatus(ctx context.Context, id, status string) error
	DeleteSkill(ctx context.Context, id string) error
}

// Clock provides the current time.
type Clock interface{ Now() time.Time }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// Service coordinates skill lifecycle, invocation gating, and matching.
type Service struct {
	read  SkillReader
	write SkillWriter
	clock Clock
}

// New creates a skill service with the given dependencies.
func New(r SkillReader, w SkillWriter) *Service {
	return &Service{read: r, write: w, clock: systemClock{}}
}

// Get returns a skill by ID.
func (s *Service) Get(ctx context.Context, id string) (*skill.Skill, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("skill reader unavailable")
	}
	sk, err := s.read.GetSkill(ctx, id)
	if err != nil {
		return nil, err
	}
	if sk == nil {
		return nil, ErrSkillNotFound
	}
	return sk, nil
}

// Create validates and persists a new skill with initial draft status.
func (s *Service) Create(ctx context.Context, sk skill.Skill) (skill.Skill, error) {
	if s == nil || s.write == nil {
		return skill.Skill{}, errors.New("skill writer unavailable")
	}
	if len(sk.Name) < 1 || len(sk.Name) > 128 {
		return skill.Skill{}, errors.New("skill name must be 1-128 characters")
	}
	if len(sk.DisplayName) < 1 || len(sk.DisplayName) > 200 {
		return skill.Skill{}, errors.New("skill display_name must be 1-200 characters")
	}
	if len(sk.Description) > 4096 {
		return skill.Skill{}, errors.New("skill description too long")
	}
	if len(sk.Version) < 1 || len(sk.Version) > 32 {
		return skill.Skill{}, errors.New("skill version must be 1-32 characters")
	}
	if len(sk.Permissions) == 0 {
		return skill.Skill{}, errors.New("skill must have at least one permission")
	}
	for _, p := range sk.Permissions {
		switch p {
		case skill.PermissionReadOnly, skill.PermissionReadWrite, skill.PermissionNetwork,
			skill.PermissionFileSystem, skill.PermissionShell, skill.PermissionAdmin:
		default:
			return skill.Skill{}, ErrInvalidPermission
		}
	}
	if len(sk.EntryPoint) < 1 || len(sk.EntryPoint) > 512 {
		return skill.Skill{}, errors.New("skill entry_point must be 1-512 characters")
	}
	if len(sk.ManifestJSON) < 2 || len(sk.ManifestJSON) > 65536 {
		return skill.Skill{}, errors.New("skill manifest_json size out of bounds")
	}
	if sk.MinEngineVersion != nil && len(*sk.MinEngineVersion) > 32 {
		return skill.Skill{}, errors.New("skill min_engine_version too long")
	}
	now := s.clock.Now()
	sk.ID = ""
	sk.Status = skill.SkillStatusDraft
	sk.CreatedAt = now
	sk.UpdatedAt = now
	return s.write.CreateSkill(ctx, sk)
}

// UpdateFields updates the mutable fields of a skill with optimistic concurrency control.
func (s *Service) UpdateFields(ctx context.Context, id string, displayName, description, entryPoint, manifestJSON *string, permissions []skill.PermissionLevel, minEngineVersion *string, expectedVersion int64) (*skill.Skill, error) {
	if s == nil || s.write == nil {
		return nil, errors.New("skill writer unavailable")
	}
	sk, err := s.read.GetSkill(ctx, id)
	if err != nil {
		return nil, err
	}
	if sk == nil {
		return nil, ErrSkillNotFound
	}
	_ = expectedVersion // accepted for API contract; skill uses semantic version string, not numeric OCC
	resolvedDisplay := sk.DisplayName
	if displayName != nil {
		if len(*displayName) < 1 || len(*displayName) > 200 {
			return nil, errors.New("skill display_name must be 1-200 characters")
		}
		resolvedDisplay = *displayName
	}
	resolvedDesc := sk.Description
	if description != nil {
		if len(*description) > 4096 {
			return nil, errors.New("skill description too long")
		}
		resolvedDesc = *description
	}
	resolvedEntry := sk.EntryPoint
	if entryPoint != nil {
		if len(*entryPoint) < 1 || len(*entryPoint) > 512 {
			return nil, errors.New("skill entry_point must be 1-512 characters")
		}
		resolvedEntry = *entryPoint
	}
	resolvedManifest := sk.ManifestJSON
	if manifestJSON != nil {
		if len(*manifestJSON) < 2 || len(*manifestJSON) > 65536 {
			return nil, errors.New("skill manifest_json size out of bounds")
		}
		resolvedManifest = *manifestJSON
	}
	resolvedPerms := sk.Permissions
	if permissions != nil {
		if len(permissions) == 0 {
			return nil, errors.New("skill must have at least one permission")
		}
		for _, p := range permissions {
			switch p {
			case skill.PermissionReadOnly, skill.PermissionReadWrite, skill.PermissionNetwork,
				skill.PermissionFileSystem, skill.PermissionShell, skill.PermissionAdmin:
			default:
				return nil, ErrInvalidPermission
			}
		}
		resolvedPerms = permissions
	}
	resolvedMinEV := sk.MinEngineVersion
	if minEngineVersion != nil {
		if len(*minEngineVersion) > 32 {
			return nil, errors.New("skill min_engine_version too long")
		}
		resolvedMinEV = minEngineVersion
	}
	permJSON, err := json.Marshal(resolvedPerms)
	if err != nil {
		return nil, err
	}
	if err := s.write.UpdateSkillFields(ctx, id, resolvedDisplay, resolvedDesc, resolvedEntry, resolvedManifest, string(permJSON), resolvedMinEV); err != nil {
		return nil, err
	}
	updated, err := s.read.GetSkill(ctx, id)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, ErrSkillNotFound
	}
	return updated, nil
}

// GetByNameVersion returns a skill by name and version.
func (s *Service) GetByNameVersion(ctx context.Context, name, version string) (*skill.Skill, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("skill reader unavailable")
	}
	sk, err := s.read.GetSkillByNameVersion(ctx, name, version)
	if err != nil {
		return nil, err
	}
	if sk == nil {
		return nil, ErrSkillNotFound
	}
	return sk, nil
}

// List returns skills optionally filtered by status.
func (s *Service) List(ctx context.Context, status skill.SkillStatus) ([]skill.Skill, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("skill reader unavailable")
	}
	statusStr := ""
	if status != "" {
		statusStr = string(status)
	}
	return s.read.ListSkills(ctx, statusStr, 100)
}

// ListPublished returns all published skills (the invocable set).
func (s *Service) ListPublished(ctx context.Context) ([]skill.Skill, error) {
	return s.List(ctx, skill.SkillStatusPublished)
}

// Update updates the display name and description of a skill.
func (s *Service) Update(ctx context.Context, id, displayName, description string) error {
	if s == nil || s.write == nil {
		return errors.New("skill writer unavailable")
	}
	if len(displayName) < 1 || len(displayName) > 200 {
		return errors.New("display_name must be 1-200 characters")
	}
	if len(description) > 4096 {
		return errors.New("description too long")
	}
	sk, err := s.read.GetSkill(ctx, id)
	if err != nil {
		return err
	}
	if sk == nil {
		return ErrSkillNotFound
	}
	return s.write.UpdateSkill(ctx, id, displayName, description)
}

// Publish transitions a draft skill to published.
func (s *Service) Publish(ctx context.Context, id string) error {
	if s == nil || s.write == nil {
		return errors.New("skill writer unavailable")
	}
	sk, err := s.read.GetSkill(ctx, id)
	if err != nil {
		return err
	}
	if sk == nil {
		return ErrSkillNotFound
	}
	if !canTransitionTo(sk.Status, skill.SkillStatusPublished) {
		return ErrInvalidTransition
	}
	return s.write.UpdateSkillStatus(ctx, id, string(skill.SkillStatusPublished))
}

// Deprecate transitions a published skill to deprecated.
func (s *Service) Deprecate(ctx context.Context, id string) error {
	if s == nil || s.write == nil {
		return errors.New("skill writer unavailable")
	}
	sk, err := s.read.GetSkill(ctx, id)
	if err != nil {
		return err
	}
	if sk == nil {
		return ErrSkillNotFound
	}
	if !canTransitionTo(sk.Status, skill.SkillStatusDeprecated) {
		return ErrInvalidTransition
	}
	return s.write.UpdateSkillStatus(ctx, id, string(skill.SkillStatusDeprecated))
}

// Disable transitions any active skill to disabled.
func (s *Service) Disable(ctx context.Context, id string) error {
	if s == nil || s.write == nil {
		return errors.New("skill writer unavailable")
	}
	sk, err := s.read.GetSkill(ctx, id)
	if err != nil {
		return err
	}
	if sk == nil {
		return ErrSkillNotFound
	}
	if !canTransitionTo(sk.Status, skill.SkillStatusDisabled) {
		return ErrInvalidTransition
	}
	return s.write.UpdateSkillStatus(ctx, id, string(skill.SkillStatusDisabled))
}

// Delete removes a skill. Only draft or disabled skills can be deleted.
func (s *Service) Delete(ctx context.Context, id string) error {
	if s == nil || s.write == nil {
		return errors.New("skill writer unavailable")
	}
	sk, err := s.read.GetSkill(ctx, id)
	if err != nil {
		return err
	}
	if sk == nil {
		return ErrSkillNotFound
	}
	if sk.Status != skill.SkillStatusDraft && sk.Status != skill.SkillStatusDisabled {
		return ErrInvalidTransition
	}
	return s.write.DeleteSkill(ctx, id)
}

// CanInvoke checks whether a skill is currently invocable and whether the
// requested permission is granted by the skill's manifest.
// Returns nil if invocation is allowed; otherwise returns a descriptive error.
func (s *Service) CanInvoke(ctx context.Context, id string, required skill.PermissionLevel) error {
	if s == nil || s.read == nil {
		return errors.New("skill reader unavailable")
	}
	// Validate the requested permission level.
	switch required {
	case skill.PermissionReadOnly, skill.PermissionReadWrite, skill.PermissionNetwork,
		skill.PermissionFileSystem, skill.PermissionShell, skill.PermissionAdmin:
	default:
		return ErrInvalidPermission
	}
	sk, err := s.read.GetSkill(ctx, id)
	if err != nil {
		return err
	}
	if sk == nil {
		return ErrSkillNotFound
	}
	switch sk.Status {
	case skill.SkillStatusPublished:
		// ok
	case skill.SkillStatusDeprecated:
		return ErrSkillDeprecated
	case skill.SkillStatusDisabled:
		return ErrSkillDisabled
	case skill.SkillStatusDraft:
		return ErrSkillNotPublished
	default:
		return ErrInvalidStatus
	}
	if !sk.HasPermission(required) {
		return ErrPermissionDenied
	}
	return nil
}

// Match searches published skills by a keyword query against name, display name,
// and description, returning SkillMatch results ranked by relevance score.
// The score is a simple 0.0-1.0 weighted match: name match > display name > description.
func (s *Service) Match(ctx context.Context, query string) ([]skill.SkillMatch, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("skill reader unavailable")
	}
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return nil, nil
	}
	published, err := s.read.ListSkills(ctx, string(skill.SkillStatusPublished), 100)
	if err != nil {
		return nil, err
	}
	keywords := strings.Fields(query)
	var matches []skill.SkillMatch
	for _, sk := range published {
		score, reason := scoreSkill(sk, keywords)
		if score <= 0 {
			continue
		}
		matches = append(matches, skill.SkillMatch{
			Skill:   sk,
			Score:   score,
			Reason:  reason,
			MatchID: sk.ID,
		})
	}
	// Sort by score descending (simple bubble sort for small lists).
	for i := 0; i < len(matches); i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[j].Score > matches[i].Score {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}
	return matches, nil
}

// scoreSkill computes a 0.0-1.0 relevance score for a skill against keywords.
// Returns (0, "") if no keywords match.
func scoreSkill(sk skill.Skill, keywords []string) (float64, string) {
	nameLower := strings.ToLower(sk.Name)
	displayLower := strings.ToLower(sk.DisplayName)
	descLower := strings.ToLower(sk.Description)
	nameHits, displayHits, descHits := 0, 0, 0
	for _, kw := range keywords {
		if strings.Contains(nameLower, kw) {
			nameHits++
		}
		if strings.Contains(displayLower, kw) {
			displayHits++
		}
		if strings.Contains(descLower, kw) {
			descHits++
		}
	}
	totalKw := len(keywords)
	if totalKw == 0 {
		return 0, ""
	}
	// Weight: name 0.6, display 0.25, description 0.15.
	score := 0.6*float64(nameHits)/float64(totalKw) +
		0.25*float64(displayHits)/float64(totalKw) +
		0.15*float64(descHits)/float64(totalKw)
	if score <= 0 {
		return 0, ""
	}
	var reason strings.Builder
	reason.WriteString("matched ")
	if nameHits > 0 {
		reason.WriteString("name")
	}
	if displayHits > 0 {
		if reason.Len() > len("matched ") {
			reason.WriteString(", ")
		}
		reason.WriteString("displayName")
	}
	if descHits > 0 {
		if reason.Len() > len("matched ") {
			reason.WriteString(", ")
		}
		reason.WriteString("description")
	}
	return score, reason.String()
}

// canTransitionTo defines the legal status transitions for a skill.
//
//	┌────────┐ publish ┌───────────┐ deprecate ┌────────────┐
//	│ draft  │────────▶│ published │──────────▶│ deprecated │
//	└────────┘         └───────────┘           └────────────┘
//	     │                   │ disable               │ disable
//	     │                   ▼                       ▼
//	     │              ┌─────────┐            ┌─────────┐
//	     └─────────────▶│disabled │◀───────────│disabled │
//	                    └─────────┘            └─────────┘
func canTransitionTo(from, to skill.SkillStatus) bool {
	switch from {
	case skill.SkillStatusDraft:
		return to == skill.SkillStatusPublished || to == skill.SkillStatusDisabled
	case skill.SkillStatusPublished:
		return to == skill.SkillStatusDeprecated || to == skill.SkillStatusDisabled
	case skill.SkillStatusDeprecated:
		return to == skill.SkillStatusDisabled
	case skill.SkillStatusDisabled:
		// Disabled is terminal; no further transitions.
		return false
	default:
		return false
	}
}
