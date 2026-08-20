// Prompt bundle entity chain (migration 0084): m6_prompt_bundle /
// m6_prompt_bundle_version. Approval of a prompt_bundle import candidate
// compiles template + vars into immutable compiled prompt text.
package m6supply

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	PromptBundleVerified    = "verified"
	PromptBundleQuarantined = "quarantined"
)

const PromptBundleManifestSchema = "lunitide.prompt_bundle/v1"

// PromptBundle is the named head of the prompt bundle version chain.
type PromptBundle struct {
	ID               string
	Name             string
	Publisher        string
	Status           string
	CurrentVersionID string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// PromptBundleVersion is one pinned, immutable compiled prompt bundle version.
type PromptBundleVersion struct {
	ID              string
	BundleID        string
	Semver          string
	ManifestRef     string
	TemplateRef     string
	PackageHash     string
	CompiledDigest  string
	CompiledBody    string
	SignatureStatus string
	CreatedAt       time.Time
}

// PromptBundleManifest is the consumed shape of a prompt bundle manifest.
type PromptBundleManifest struct {
	Schema      string            `json:"schema"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Publisher   string            `json:"publisher"`
	Template    string            `json:"template,omitempty"`
	TemplateRef string            `json:"templateRef,omitempty"`
	Vars        map[string]string `json:"vars,omitempty"`
}

// ParsePromptBundleManifest validates and decodes a prompt bundle manifest.
func ParsePromptBundleManifest(raw []byte) (*PromptBundleManifest, error) {
	if len(raw) == 0 || len(raw) > MaxManifestBytes {
		return nil, fmt.Errorf("%w: size out of range", ErrManifestInvalid)
	}
	var m PromptBundleManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}
	if m.Schema != PromptBundleManifestSchema {
		return nil, fmt.Errorf("%w: schema must be %s", ErrManifestInvalid, PromptBundleManifestSchema)
	}
	if len(m.Name) < 1 || len(m.Name) > 128 {
		return nil, fmt.Errorf("%w: name length must be 1..128", ErrManifestInvalid)
	}
	if len(m.Version) < 1 || len(m.Version) > 64 {
		return nil, fmt.Errorf("%w: version length must be 1..64", ErrManifestInvalid)
	}
	if len(m.Publisher) < 1 || len(m.Publisher) > 256 {
		return nil, fmt.Errorf("%w: publisher length must be 1..256", ErrManifestInvalid)
	}
	if m.Template == "" && m.TemplateRef == "" {
		m.TemplateRef = "lunitide-prompt.tpl"
	}
	if m.Vars == nil {
		m.Vars = map[string]string{}
	}
	return &m, nil
}
