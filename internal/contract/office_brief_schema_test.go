package contract

import (
	"encoding/json"
	"os"
	"testing"
)

func TestOfficeBriefDTOAcceptsAuthoredConfidentialityAndOutline(t *testing.T) {
	body, err := os.ReadFile("../../api/bridge/v1/public.dto.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	if err = json.Unmarshal(body, &schema); err != nil {
		t.Fatal(err)
	}
	brief, ok := schema.Defs["OfficeBriefDTO"]
	if !ok {
		t.Fatal("missing OfficeBriefDTO")
	}
	if _, ok = brief.Properties["confidentiality"]; !ok {
		t.Fatal("OfficeBriefDTO missing confidentiality")
	}
	if _, ok = brief.Properties["outline"]; !ok {
		t.Fatal("OfficeBriefDTO missing outline")
	}
	if _, ok = schema.Defs["OfficeNarrativeNodeDTO"]; !ok {
		t.Fatal("missing OfficeNarrativeNodeDTO")
	}
}
