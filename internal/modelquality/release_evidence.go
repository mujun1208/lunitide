package modelquality

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var (
	ErrReleaseEvidenceAbsent     = errors.New("release evidence file is absent")
	ErrReleaseEvidenceIncomplete = errors.New("release evidence is incomplete")
)

var (
	shaHex    = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
	digestHex = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type ReleaseEvidenceReport struct {
	Complete bool     `json:"complete"`
	LayerE   string   `json:"layerE"`
	Reasons  []string `json:"reasons,omitempty"`
}

type frEvidenceSlot struct {
	Status               string `json:"status"`
	ImplementationCommit string `json:"implementationCommit"`
	TestCommand          string `json:"testCommand"`
	TestResult           string `json:"testResult"`
	RunEvidenceDigest    string `json:"runEvidenceDigest"`
}

type namedEvidence struct {
	Status                 string `json:"status"`
	LatestAppliedMigration string `json:"latestAppliedMigration"`
}

type releaseEvidenceDoc struct {
	SchemaVersion        int                       `json:"schemaVersion"`
	Commit               string                    `json:"commit"`
	Layers               map[string]string         `json:"layers"`
	Requirements         map[string]frEvidenceSlot `json:"requirements"`
	ModelQualification   namedEvidence             `json:"modelQualification"`
	TemplateCertificates namedEvidence             `json:"templateCertificates"`
	MigrationRecord      namedEvidence             `json:"migrationRecord"`
}

func RepoReleaseEvidencePath() string {
	return filepath.Join(upgradeAuditDir(), "release-evidence.json")
}

func upgradeBaselinePath() string {
	return filepath.Join(upgradeAuditDir(), "baseline.json")
}

func upgradeAuditDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("docs", "audits", "model-office-upgrade")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "docs", "audits", "model-office-upgrade"))
}

func AssessReleaseEvidence(path string) (ReleaseEvidenceReport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ReleaseEvidenceReport{}, fmt.Errorf("%w: %s", ErrReleaseEvidenceAbsent, path)
		}
		return ReleaseEvidenceReport{}, err
	}
	return AssessReleaseEvidenceBytes(raw)
}

func AssessReleaseEvidenceBytes(raw []byte) (ReleaseEvidenceReport, error) {
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ReleaseEvidenceReport{}, fmt.Errorf("%w: %v", ErrReleaseEvidenceIncomplete, err)
	}
	reqs, _ := probe["requirements"].(map[string]any)
	if reqs == nil {
		return ReleaseEvidenceReport{}, fmt.Errorf("%w: requirements missing", ErrReleaseEvidenceIncomplete)
	}
	for i := 1; i <= 30; i++ {
		id := fmt.Sprintf("FR%02d", i)
		slot, ok := reqs[id]
		if !ok || slot == nil {
			return ReleaseEvidenceReport{}, fmt.Errorf("%w: %s missing", ErrReleaseEvidenceIncomplete, id)
		}
		obj, ok := slot.(map[string]any)
		if !ok || len(obj) == 0 {
			return ReleaseEvidenceReport{}, fmt.Errorf("%w: %s empty", ErrReleaseEvidenceIncomplete, id)
		}
		status := strings.TrimSpace(fmt.Sprint(obj["status"]))
		if status == "inventory" || status == "inventory-only" {
			return ReleaseEvidenceReport{}, fmt.Errorf("%w: %s inventory-only", ErrReleaseEvidenceIncomplete, id)
		}
		for _, key := range []string{"implementationCommit", "testCommand", "testResult", "runEvidenceDigest"} {
			if _, exists := obj[key]; !exists {
				return ReleaseEvidenceReport{}, fmt.Errorf("%w: %s missing %s", ErrReleaseEvidenceIncomplete, id, key)
			}
		}
	}
	if _, ok := probe["modelQualification"].(map[string]any); !ok {
		return ReleaseEvidenceReport{}, fmt.Errorf("%w: modelQualification missing", ErrReleaseEvidenceIncomplete)
	}
	if _, ok := probe["templateCertificates"].(map[string]any); !ok {
		return ReleaseEvidenceReport{}, fmt.Errorf("%w: templateCertificates missing", ErrReleaseEvidenceIncomplete)
	}
	if _, ok := probe["migrationRecord"].(map[string]any); !ok {
		return ReleaseEvidenceReport{}, fmt.Errorf("%w: migrationRecord missing", ErrReleaseEvidenceIncomplete)
	}

	var doc releaseEvidenceDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ReleaseEvidenceReport{}, fmt.Errorf("%w: %v", ErrReleaseEvidenceIncomplete, err)
	}
	layerE := ""
	if doc.Layers != nil {
		layerE = doc.Layers["E"]
	}
	report := ReleaseEvidenceReport{LayerE: layerE}
	complete := true
	for i := 1; i <= 30; i++ {
		id := fmt.Sprintf("FR%02d", i)
		if !slotReleaseComplete(doc.Requirements[id]) {
			complete = false
			report.Reasons = append(report.Reasons, id+" not complete")
		}
	}
	if !namedReleaseComplete(doc.ModelQualification.Status) {
		complete = false
		report.Reasons = append(report.Reasons, "modelQualification not complete")
	}
	if !namedReleaseComplete(doc.TemplateCertificates.Status) {
		complete = false
		report.Reasons = append(report.Reasons, "templateCertificates not complete")
	}
	if !namedReleaseComplete(doc.MigrationRecord.Status) || strings.TrimSpace(doc.MigrationRecord.LatestAppliedMigration) == "" {
		complete = false
		report.Reasons = append(report.Reasons, "migrationRecord not complete")
	}
	report.Complete = complete
	if layerE == "done" && !complete {
		return report, fmt.Errorf("%w: layers.E=done without complete FR evidence", ErrReleaseEvidenceIncomplete)
	}
	return report, nil
}

func slotReleaseComplete(slot frEvidenceSlot) bool {
	if incompleteReleaseStatus(slot.Status) {
		return false
	}
	if !shaHex.MatchString(strings.ToLower(slot.ImplementationCommit)) {
		return false
	}
	if strings.TrimSpace(slot.TestCommand) == "" {
		return false
	}
	result := strings.ToLower(strings.TrimSpace(slot.TestResult))
	if result != "pass" && result != "passed" {
		return false
	}
	digest := strings.ToLower(strings.TrimSpace(slot.RunEvidenceDigest))
	return digestHex.MatchString(digest)
}

func namedReleaseComplete(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "complete", "passed", "certified", "live":
		return true
	default:
		return false
	}
}

func incompleteReleaseStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "missing", "pending", "first_lock_only", "inventory", "inventory-only":
		return true
	default:
		return false
	}
}
