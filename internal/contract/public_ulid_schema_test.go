package contract

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"
)

func TestPublicULIDSchemaRejectsOverflowFirstCharacterExample(t *testing.T) {
	body, err := os.ReadFile("../../api/bridge/v1/public.dto.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]struct {
			Pattern  string `json:"pattern"`
			Examples struct {
				Negative []string `json:"negative"`
			} `json:"x-examples"`
		} `json:"$defs"`
	}
	if err = json.Unmarshal(body, &schema); err != nil {
		t.Fatal(err)
	}
	ulid := schema.Defs["ULID"]
	pattern := regexp.MustCompile(ulid.Pattern)
	if len(ulid.Examples.Negative) != 1 || pattern.MatchString(ulid.Examples.Negative[0]) {
		t.Fatalf("negative ULID example must fail %q: %#v", ulid.Pattern, ulid.Examples.Negative)
	}
}
