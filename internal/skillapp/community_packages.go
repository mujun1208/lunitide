package skillapp

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

// Community sources are vendored data. Importing a package never executes its
// setup scripts, installs a runtime, or grants the upstream permission claims.
//
//go:embed all:bundled/community
var communityFiles embed.FS

type CommunityResource struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

type CommunityPackage struct {
	ID              string              `json:"id"`
	CatalogID       string              `json:"catalogId"`
	Name            string              `json:"name"`
	Version         string              `json:"version"`
	Repository      string              `json:"repository"`
	Commit          string              `json:"commit"`
	Subdirectory    string              `json:"subdirectory"`
	Entry           string              `json:"entry"`
	License         string              `json:"license"`
	LicenseEvidence string              `json:"licenseEvidence"`
	Description     string              `json:"description"`
	Category        string              `json:"category"`
	Aliases         []string            `json:"aliases"`
	Dependencies    []string            `json:"dependencies"`
	RuntimeNotes    []string            `json:"runtimeNotes"`
	Digest          string              `json:"digest"`
	Resources       []CommunityResource `json:"resources"`
}

type communityBundleRef struct {
	ID     string `json:"id"`
	Commit string `json:"commit"`
	Digest string `json:"digest"`
}

// CommunityPackages returns provenance without exposing the mutable registry.
func CommunityPackages() ([]CommunityPackage, error) {
	b, err := communityFiles.ReadFile("bundled/community/sources.json")
	if err != nil {
		return nil, err
	}
	var entries []CommunityPackage
	if err = json.Unmarshal(b, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// BundledPackageFiles resolves only the exact product-shipped source receipt.
// A normal user skill returns nil; a forged/stale bundle marker is an error,
// never a fallback to an unrelated package with the same name.
func BundledPackageFiles(sk skill.Skill) (map[string][]byte, error) {
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal([]byte(sk.ManifestJSON), &manifest); err != nil {
		return nil, err
	}
	marker, bundled := manifest["bundledPackage"]
	if !bundled {
		return nil, nil
	}
	var ref communityBundleRef
	if err := json.Unmarshal(marker, &ref); err != nil {
		return nil, err
	}
	packages, err := CommunityPackages()
	if err != nil {
		return nil, err
	}
	for _, pkg := range packages {
		if pkg.ID != ref.ID {
			continue
		}
		if ref.Commit != pkg.Commit || ref.Digest != pkg.Digest || sk.Name != pkg.Name || sk.Version != pkg.Version {
			return nil, errors.New("skillapp: bundled package source receipt mismatch")
		}
		var source struct{ Repository, Commit, Path, License, Entry string }
		if json.Unmarshal(manifest["source"], &source) != nil || source.Repository != pkg.Repository || source.Commit != pkg.Commit || source.Path != pkg.Subdirectory || source.License != pkg.License || source.Entry != pkg.Entry {
			return nil, errors.New("skillapp: bundled package provenance mismatch")
		}
		out := make(map[string][]byte, len(pkg.Resources))
		for _, resource := range pkg.Resources {
			if !fs.ValidPath(resource.Path) || strings.Contains(resource.Path, "\\") || strings.Contains(resource.Path, ":") || !strings.HasPrefix(resource.Path, "upstream/") {
				return nil, errors.New("skillapp: invalid bundled resource path")
			}
			b, readErr := communityFiles.ReadFile(path.Join("bundled/community", pkg.ID, resource.Path))
			if readErr != nil {
				return nil, readErr
			}
			digest := sha256.Sum256(b)
			if len(b) != resource.Bytes || hex.EncodeToString(digest[:]) != resource.SHA256 {
				return nil, fmt.Errorf("skillapp: bundled resource integrity mismatch: %s", resource.Path)
			}
			out[resource.Path] = b
		}
		return out, nil
	}
	return nil, errors.New("skillapp: unknown bundled package source")
}
