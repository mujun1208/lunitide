package modelquality

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUpgradeReleaseEvidenceComplete(t *testing.T) {
	if err := requireBaselineEPending(); err != nil {
		t.Fatal(err)
	}

	t.Run("absent_file_fails_closed", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "release-evidence.json")
		_, err := AssessReleaseEvidence(missing)
		if err == nil {
			t.Fatal("absent release-evidence.json must fail closed")
		}
		if !errors.Is(err, ErrReleaseEvidenceAbsent) {
			t.Fatalf("err=%v, want ErrReleaseEvidenceAbsent", err)
		}
	})

	path := RepoReleaseEvidencePath()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("release-evidence.json absent fails closed: %v", err)
	}

	report, err := AssessReleaseEvidence(path)
	if err != nil {
		t.Fatal(err)
	}
	if report.Complete {
		t.Fatal("inventory-only or first-lock evidence must not look like a release")
	}
	if report.LayerE == "done" {
		t.Fatal("layers.E must stay pending")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}

	t.Run("delete_one_required_item_fails", func(t *testing.T) {
		mut := cloneJSON(t, doc)
		reqs := mut["requirements"].(map[string]any)
		delete(reqs, "FR15")
		_, err := AssessReleaseEvidenceBytes(mustJSON(t, mut))
		if err == nil {
			t.Fatal("deleting one required evidence item must fail the check")
		}
		if !errors.Is(err, ErrReleaseEvidenceIncomplete) {
			t.Fatalf("err=%v, want ErrReleaseEvidenceIncomplete", err)
		}
	})

	t.Run("empty_slot_fails", func(t *testing.T) {
		mut := cloneJSON(t, doc)
		reqs := mut["requirements"].(map[string]any)
		reqs["FR07"] = map[string]any{}
		_, err := AssessReleaseEvidenceBytes(mustJSON(t, mut))
		if err == nil {
			t.Fatal("empty FR slot must fail the check")
		}
		if !errors.Is(err, ErrReleaseEvidenceIncomplete) {
			t.Fatalf("err=%v, want ErrReleaseEvidenceIncomplete", err)
		}
	})

	t.Run("inventory_only_slot_fails", func(t *testing.T) {
		mut := cloneJSON(t, doc)
		reqs := mut["requirements"].(map[string]any)
		reqs["FR01"] = map[string]any{"status": "inventory-only", "id": "FR01"}
		_, err := AssessReleaseEvidenceBytes(mustJSON(t, mut))
		if err == nil {
			t.Fatal("inventory-only slot must fail the check")
		}
		if !errors.Is(err, ErrReleaseEvidenceIncomplete) {
			t.Fatalf("err=%v, want ErrReleaseEvidenceIncomplete", err)
		}
	})

	t.Run("e_done_without_complete_evidence_fails", func(t *testing.T) {
		mut := cloneJSON(t, doc)
		layers, _ := mut["layers"].(map[string]any)
		if layers == nil {
			layers = map[string]any{}
			mut["layers"] = layers
		}
		layers["E"] = "done"
		_, err := AssessReleaseEvidenceBytes(mustJSON(t, mut))
		if err == nil {
			t.Fatal("layers.E=done without complete FR evidence must fail")
		}
		if !errors.Is(err, ErrReleaseEvidenceIncomplete) {
			t.Fatalf("err=%v, want ErrReleaseEvidenceIncomplete", err)
		}
	})
}

func requireBaselineEPending() error {
	raw, err := os.ReadFile(upgradeBaselinePath())
	if err != nil {
		return err
	}
	var baseline struct {
		Layers struct {
			E string `json:"E"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(raw, &baseline); err != nil {
		return err
	}
	if baseline.Layers.E != "pending" {
		return errors.New("layers.E must stay pending")
	}
	return nil
}

func cloneJSON(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	raw := mustJSON(t, doc)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
