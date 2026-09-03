package mroapp

import (
	"encoding/json"
	"strings"
)

// CitationBlock is the checklist-side cite DTO. It must not import internal/app.
type CitationBlock struct {
	Revision   string `json:"revision,omitempty"`
	ATA        string `json:"ata,omitempty"`
	Locator    string `json:"locator,omitempty"`
	Quote      string `json:"quote,omitempty"`
	ExpertName string `json:"expertName,omitempty"`
}

// ChecklistStep is one cited step in the downloadable JSON.
type ChecklistStep struct {
	N          int    `json:"n"`
	Text       string `json:"text"`
	Revision   string `json:"revision"`
	ATA        string `json:"ata,omitempty"`
	ExpertName string `json:"expertName,omitempty"`
}

// Checklist is the downloadable workbench artifact.
type Checklist struct {
	Banner string          `json:"banner"`
	Steps  []ChecklistStep `json:"steps"`
}

const checklistBanner = "辅助建议，不构成放行"

// BuildChecklistJSON keeps only index-aligned cited steps and renumbers them.
func BuildChecklistJSON(steps []string, cites []CitationBlock) []byte {
	out := Checklist{Banner: checklistBanner, Steps: []ChecklistStep{}}
	n := 0
	for i, step := range steps {
		text := strings.TrimSpace(step)
		if text == "" || i >= len(cites) || !citePresent(cites[i]) {
			continue
		}
		n++
		row := ChecklistStep{N: n, Text: text, Revision: strings.TrimSpace(cites[i].Revision), ATA: strings.TrimSpace(cites[i].ATA)}
		if name := strings.TrimSpace(cites[i].ExpertName); name != "" {
			row.ExpertName = name
		}
		out.Steps = append(out.Steps, row)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return []byte(`{"banner":"辅助建议，不构成放行","steps":[]}`)
	}
	return raw
}

func citePresent(c CitationBlock) bool {
	return strings.TrimSpace(c.Revision) != "" || strings.TrimSpace(c.ATA) != "" ||
		strings.TrimSpace(c.Locator) != "" || strings.TrimSpace(c.Quote) != ""
}
