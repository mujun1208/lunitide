package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

func TestAdaptOfficeGenerateArgsWrapsDocxGenShape(t *testing.T) {
	raw := json.RawMessage(`{"path":"周报.docx","title":"工作周报","kind":"report","blocks":[{"type":"paragraph","text":"本周完成测试复核"}],"desktop":true}`)
	adapted := adaptOfficeGenerateArgs(raw)
	var p struct {
		Name string       `json:"name"`
		Spec content.Spec `json:"spec"`
	}
	if err := decodePayload(adapted, &p); err != nil {
		t.Fatalf("docx.gen-shaped office.generate must decode after adapt: %v %s", err, adapted)
	}
	if p.Name != "周报.docx" || p.Spec.Kind != content.DOCX || p.Spec.Title != "工作周报" || len(p.Spec.Blocks) != 1 {
		t.Fatalf("adapted spec: %+v", p)
	}
}

func TestAdaptOfficeGenerateArgsMapsPathWhenSpecAlreadyPresent(t *testing.T) {
	raw := json.RawMessage(`{"path":"周报.docx","spec":{"schemaVersion":1,"kind":"docx","title":"工作周报","blocks":[{"type":"paragraph","text":"本周完成"}]}}`)
	adapted := adaptOfficeGenerateArgs(raw)
	var p struct {
		Name string       `json:"name"`
		Spec content.Spec `json:"spec"`
	}
	if err := decodePayload(adapted, &p); err != nil || p.Name != "周报.docx" {
		t.Fatalf("path+spec must become name+spec: %+v %v %s", p, err, adapted)
	}
}

func TestAdaptOfficeGenerateArgsStripsUnknownTopLevel(t *testing.T) {
	raw := json.RawMessage(`{"name":"周报.docx","spec":{"schemaVersion":1,"kind":"docx","title":"工作周报","blocks":[{"type":"paragraph","text":"本周完成"}]},"author":"模型","desktop":true}`)
	if decodePayload(raw, &struct {
		Name string       `json:"name"`
		Spec content.Spec `json:"spec"`
	}{}) == nil {
		t.Fatal("fixture must keep unknown top-level fields so the test is real")
	}
	adapted := adaptOfficeGenerateArgs(raw)
	var p struct {
		Name string       `json:"name"`
		Spec content.Spec `json:"spec"`
	}
	if err := decodePayload(adapted, &p); err != nil || p.Name != "周报.docx" || p.Spec.Title != "工作周报" {
		t.Fatalf("strip extras: %+v %v %s", p, err, adapted)
	}
}

func TestAdaptOfficeGenerateArgsWrapsExcelGenSheets(t *testing.T) {
	raw := json.RawMessage(`{"path":"周报汇总.xlsx","title":"本周汇总","sheets":[{"name":"事项","headers":["项","状态"],"rows":[["测试复核","完成"]]}]}`)
	adapted := adaptOfficeGenerateArgs(raw)
	var p struct {
		Name string       `json:"name"`
		Spec content.Spec `json:"spec"`
	}
	if err := decodePayload(adapted, &p); err != nil {
		t.Fatalf("excel.gen-shaped office.generate must decode after adapt: %v %s", err, adapted)
	}
	if p.Name != "周报汇总.xlsx" || p.Spec.Kind != content.XLSX || len(p.Spec.Sheets) != 1 || len(p.Spec.Sheets[0].Rows) != 2 {
		t.Fatalf("adapted workbook: %+v", p.Spec)
	}
}

func TestOfficeGenerateAcceptsExcelGenShapedArgs(t *testing.T) {
	e, _ := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "weekly-excel-shape")
	args := json.RawMessage(`{"path":"周报汇总.xlsx","title":"本周汇总","sheets":[{"name":"事项","headers":["项","状态"],"rows":[["测试复核","完成"],["产物卡片","完成"]]}]}`)
	data, name, output, err := e.executeOfficeTool(context.Background(), task.SessionID, "office.generate", args)
	if err != nil || len(data) == 0 {
		t.Fatalf("weekly-report office.generate with excel.gen args: name=%q output=%q err=%v", name, output, err)
	}
	if !strings.Contains(output, "文件已存档") || !strings.Contains(name, ".xlsx") {
		t.Fatalf("delivery: name=%q output=%q", name, output)
	}
}

func TestOfficeGenerateAcceptsDocxGenShapedArgs(t *testing.T) {
	e, _ := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "weekly-docx-shape")
	args := json.RawMessage(`{"path":"周报.docx","title":"工作周报","kind":"report","blocks":[{"type":"heading","text":"本周完成"},{"type":"paragraph","text":"完成周报工具链复核，并补上产物卡片。"}]}`)
	data, name, output, err := e.executeOfficeTool(context.Background(), task.SessionID, "office.generate", args)
	if err != nil || len(data) == 0 {
		t.Fatalf("weekly-report office.generate with docx.gen args: name=%q output=%q err=%v", name, output, err)
	}
	if !strings.Contains(output, "文件已存档") || !strings.Contains(name, ".docx") {
		t.Fatalf("delivery: name=%q output=%q", name, output)
	}
}

func TestOfficeToolAfterMutateKeepsFileWhenCheckRecordFails(t *testing.T) {
	e, _ := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "check-record-fail")
	version, err := e.officeStudio.Generate(context.Background(), task.ID, "周报.docx", content.Spec{
		SchemaVersion: 1, Kind: content.DOCX, Title: "工作周报",
		Blocks: []content.Block{{Type: "paragraph", Text: "文件已经进档案。"}},
	}, "keep-file")
	if err != nil || version.ID == "" {
		t.Fatalf("seed generate: %+v %v", version, err)
	}
	data, name, output, err := e.officeToolAfterMutate(context.Background(), task.ID, version, errors.New("文件已存档，但检查记录失败：qa sidecar"))
	if err != nil || len(data) == 0 || !strings.Contains(name, ".docx") {
		t.Fatalf("archived file must still deliver: name=%q err=%v", name, err)
	}
	if !strings.Contains(output, "文件已存档") || !strings.Contains(output, "检查记录失败") {
		t.Fatalf("summary must keep the check warning: %q", output)
	}
	_, _, _, err = e.officeToolAfterMutate(context.Background(), task.ID, domain.Version{}, errors.New("未生成任何版本"))
	if err == nil {
		t.Fatal("empty version must still fail")
	}
}
