package skill

import (
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
)

// SkillStatus represents the lifecycle state of a skill.
type SkillStatus string

const (
	SkillStatusDraft     SkillStatus = "draft"
	SkillStatusPublished SkillStatus = "published"
	SkillStatusDeprecated SkillStatus = "deprecated"
	SkillStatusDisabled  SkillStatus = "disabled"
)

// PermissionLevel defines the access level required to invoke a skill.
type PermissionLevel string

const (
	PermissionReadOnly  PermissionLevel = "read_only"
	PermissionReadWrite PermissionLevel = "read_write"
	PermissionNetwork   PermissionLevel = "network"
	PermissionFileSystem PermissionLevel = "file_system"
	PermissionShell     PermissionLevel = "shell"
	PermissionAdmin     PermissionLevel = "admin"
)

// RiskLevel groups permissions by risk for governance.
func (p PermissionLevel) RiskLevel() string {
	switch p {
	case PermissionReadOnly:
		return "low"
	case PermissionReadWrite, PermissionNetwork:
		return "medium"
	case PermissionFileSystem:
		return "high"
	case PermissionShell, PermissionAdmin:
		return "critical"
	default:
		return "unknown"
	}
}

// Skill is a pluggable capability with a manifest, version, permissions,
// and execution entry point. Skills are the primary extension mechanism
// for the Lunitide platform.
type Skill struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	DisplayName     string          `json:"displayName"`
	Description     string          `json:"description"`
	Version         string          `json:"version"`
	Status          SkillStatus     `json:"status"`
	Permissions     []PermissionLevel `json:"permissions"`
	EntryPoint      string          `json:"entryPoint"`
	ManifestJSON    string          `json:"manifestJson"`
	Signature       *string         `json:"signature,omitempty"`
	PublisherID     *string         `json:"publisherId,omitempty"`
	MinEngineVersion *string        `json:"minEngineVersion,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

// Validate checks invariants for a skill.
func (s Skill) Validate() error {
	if !canonicalULID(s.ID) {
		return errors.New("skill id is not a canonical ULID")
	}
	if len(s.Name) < 1 || len(s.Name) > 128 {
		return errors.New("skill name must be 1-128 characters")
	}
	if len(s.DisplayName) < 1 || len(s.DisplayName) > 200 {
		return errors.New("skill display_name must be 1-200 characters")
	}
	if len(s.Description) > 4096 {
		return errors.New("skill description too long")
	}
	if len(s.Version) < 1 || len(s.Version) > 32 {
		return errors.New("skill version must be 1-32 characters")
	}
	switch s.Status {
	case SkillStatusDraft, SkillStatusPublished, SkillStatusDeprecated, SkillStatusDisabled:
	default:
		return errors.New("skill status invalid")
	}
	if len(s.Permissions) == 0 {
		return errors.New("skill must have at least one permission")
	}
	for _, p := range s.Permissions {
		switch p {
		case PermissionReadOnly, PermissionReadWrite, PermissionNetwork,
			PermissionFileSystem, PermissionShell, PermissionAdmin:
		default:
			return errors.New("skill permission invalid")
		}
	}
	if len(s.EntryPoint) < 1 || len(s.EntryPoint) > 512 {
		return errors.New("skill entry_point must be 1-512 characters")
	}
	if len(s.ManifestJSON) < 2 || len(s.ManifestJSON) > 65536 {
		return errors.New("skill manifest_json size out of bounds")
	}
	if s.Signature != nil && len(*s.Signature) > 1024 {
		return errors.New("skill signature too long")
	}
	if s.PublisherID != nil && !canonicalULID(*s.PublisherID) {
		return errors.New("skill publisher_id is not a canonical ULID")
	}
	if s.MinEngineVersion != nil && len(*s.MinEngineVersion) > 32 {
		return errors.New("skill min_engine_version too long")
	}
	if s.CreatedAt.IsZero() || s.CreatedAt.Location() != time.UTC {
		return errors.New("skill created_at must be UTC")
	}
	if s.UpdatedAt.IsZero() || s.UpdatedAt.Location() != time.UTC {
		return errors.New("skill updated_at must be UTC")
	}
	if s.UpdatedAt.Before(s.CreatedAt) {
		return errors.New("skill updated_at must be >= created_at")
	}
	return nil
}

// HasPermission checks if the skill has a specific permission.
func (s Skill) HasPermission(perm PermissionLevel) bool {
	for _, p := range s.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}

// MaxRiskLevel returns the highest risk level among the skill's permissions.
func (s Skill) MaxRiskLevel() string {
	maxRisk := "low"
	for _, p := range s.Permissions {
		r := p.RiskLevel()
		switch {
		case r == "critical":
			return "critical"
		case r == "high" && maxRisk != "critical":
			maxRisk = "high"
		case r == "medium" && maxRisk == "low":
			maxRisk = "medium"
		}
	}
	return maxRisk
}

// SkillMatch represents a skill matching result for a given context.
type SkillMatch struct {
	Skill      Skill   `json:"skill"`
	Score      float64 `json:"score"`
	Reason     string  `json:"reason"`
	MatchID    string  `json:"matchId"`
}

func canonicalULID(v string) bool {
	u, err := ulid.ParseStrict(v)
	return err == nil && u.String() == v && v[0] <= '7'
}