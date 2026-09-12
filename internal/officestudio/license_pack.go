package officestudio

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// LicensePackItem records one asset in the license pack checklist.
type LicensePackItem struct {
	SourceURL        string `json:"sourceUrl"`
	Author           string `json:"author"`
	License          string `json:"license"`
	Digest           string `json:"digest"`
	Commercial       string `json:"commercial"`
	Redistributable  string `json:"redistributable"`
}

// LicensePackChecklist tracks license review status for release assets.
type LicensePackChecklist struct {
	Reviewed       bool              `json:"reviewed"`
	Notice         string            `json:"notice"`
	RequiredFields []string          `json:"requiredFields"`
	Items          []LicensePackItem `json:"items"`
}

// LoadLicensePackChecklist reads a license pack checklist from path.
func LoadLicensePackChecklist(path string) (LicensePackChecklist, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return LicensePackChecklist{}, err
	}
	var pack LicensePackChecklist
	if err := json.Unmarshal(raw, &pack); err != nil {
		return LicensePackChecklist{}, err
	}
	if err := pack.Validate(); err != nil {
		return LicensePackChecklist{}, err
	}
	return pack, nil
}

// Validate rejects reviewed packs with missing item digests.
func (p LicensePackChecklist) Validate() error {
	if !p.Reviewed {
		return nil
	}
	if len(p.Items) == 0 {
		return errors.New("license pack reviewed with no items")
	}
	for i, item := range p.Items {
		if item.Digest == "" {
			return fmt.Errorf("license pack item %d missing digest", i)
		}
	}
	return nil
}
