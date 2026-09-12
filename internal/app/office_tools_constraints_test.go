package app

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

func TestOfficeInspectEmptySessionNeverCreatesTask(t *testing.T) {
	e, store := officeEngineFixture(t)
	existing := officeCreatedTask(t, e, "existing")
	parent, err := store.GetSession(context.Background(), existing.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	r := officeCall(t, e, "session.create", "inspect-only-session", map[string]any{"projectId": parent.ProjectID, "title": "只读检查会话"})
	if !r.OK {
		t.Fatal(r.Error)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := decodeResponsePayload(r.Payload, &created); err != nil || created.ID == "" {
		t.Fatal("missing session", err)
	}
	ctx := context.Background()
	before, err := store.ListOfficeTasks(ctx, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		file, name, output, err := e.executeOfficeTool(ctx, created.ID, "office.inspect", json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			TaskID   string           `json:"taskId"`
			Heads    []domain.Head    `json:"heads"`
			Versions []domain.Version `json:"versions"`
			Notice   string           `json:"notice"`
		}
		if err = json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatal(err)
		}
		if len(file) != 0 || name != "" || result.TaskID != "" || result.Heads == nil || result.Versions == nil || len(result.Heads) != 0 || len(result.Versions) != 0 || !strings.Contains(result.Notice, "尚无") {
			t.Fatal("empty inspect response incorrect", output)
		}
	}
	after, err := store.ListOfficeTasks(ctx, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("read-only inspect wrote task data: before=%+v after=%+v", before, after)
	}
	rows, err := store.ListOfficeTasks(ctx, created.ID, 100)
	if err != nil || len(rows) != 0 {
		t.Fatal("inspect created task", rows, err)
	}
}

func TestOfficeToolLayoutsMatchGeneratorAndRangeDoesNotOfferFormatting(t *testing.T) {
	definitions := map[string]map[string]any{}
	for _, definition := range officeToolDefinitions() {
		var schema map[string]any
		if err := json.Unmarshal(definition.Schema, &schema); err != nil {
			t.Fatal(err)
		}
		definitions[definition.Name] = schema
	}
	properties := func(v map[string]any) map[string]any { return v["properties"].(map[string]any) }
	rangeTool := properties(definitions["office.range.patch"])
	rangeSpec := rangeTool["ranges"].(map[string]any)["items"].(map[string]any)
	row := properties(rangeSpec)["rows"].(map[string]any)["items"].(map[string]any)
	cell := row["items"].(map[string]any)
	if _, offered := properties(cell)["format"]; offered {
		t.Fatal("range tool offers unsupported formatting")
	}
	if cell["additionalProperties"] != false {
		t.Fatal("range cell accepts unspecified mutation fields")
	}
	spec := properties(definitions["office.generate"])["spec"].(map[string]any)
	version := properties(spec)["schemaVersion"].(map[string]any)
	seen := map[int]bool{}
	enumVals, _ := version["enum"].([]any)
	for _, item := range enumVals {
		switch n := item.(type) {
		case float64:
			seen[int(n)] = true
		case int:
			seen[n] = true
		}
	}
	if !seen[1] || !seen[2] || len(enumVals) != 2 {
		t.Fatalf("office.generate must allow only schemaVersion 1 and 2, got %#v", version["enum"])
	}
	if _, ok := properties(spec)["brandId"]; !ok {
		t.Fatal("office.generate spec missing brandId")
	}
	if _, ok := properties(spec)["facts"]; !ok {
		t.Fatal("office.generate spec missing facts")
	}
	slide := properties(spec)["slides"].(map[string]any)["items"].(map[string]any)
	if _, ok := properties(slide)["metrics"]; !ok {
		t.Fatal("office.generate slides missing v2 metrics")
	}
	if _, ok := properties(slide)["evidence"]; !ok {
		t.Fatal("office.generate slides missing v2 evidence")
	}
	layouts := properties(slide)["layout"].(map[string]any)["enum"].([]any)
	deck := content.Spec{SchemaVersion: 1, Kind: content.PPTX, Title: "全部工具布局"}
	for _, value := range layouts {
		layout := value.(string)
		item := content.Slide{Title: layout, Layout: layout, Bullets: []string{"已支持内容"}}
		if layout == "metrics" {
			item.Bullets = []string{"1|样本"}
		}
		if layout == "table" {
			item.Bullets = nil
			item.Rows = [][]string{{"项目", "数量"}, {"验证", "1"}}
		}
		deck.Slides = append(deck.Slides, item)
	}
	if _, err := content.Generate(deck); err != nil {
		t.Fatal("tool advertises an unusable layout", err)
	}
}
