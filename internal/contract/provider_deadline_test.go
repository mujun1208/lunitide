package contract

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
)

func TestProviderTestDeadlineFitsEnvelopeSchema(t *testing.T) {
	data, err := os.ReadFile("../../api/bridge/v1/envelope.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]struct{ Maximum int } `json:"properties"`
		Examples   struct {
			Positive []bridge.Request `json:"positive"`
		} `json:"x-examples"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	limit := bridge.MaxDeadlineMS("provider.test")
	if limit < 300000 || limit > schema.Properties["deadlineMs"].Maximum || bridge.MaxDeadlineMS("provider.model.sync") != 30000 {
		t.Fatalf("deadline mismatch: %d", limit)
	}
	for _, example := range schema.Examples.Positive {
		if example.Method == "provider.test" && example.DeadlineMS == limit {
			return
		}
	}
	t.Fatal("missing long provider.test schema example")
}
