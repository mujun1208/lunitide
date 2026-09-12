package officestudio

import (
	"fmt"
	"strings"
	"time"
)

func WorkbookKinds() []string {
	return []string{"ops-ledger", "sales-pipeline", "project-tracker"}
}

func PlanWorkbook(kind, title string, facts []Fact) (Spec, error) {
	if err := ValidateFactSet(facts); err != nil {
		return Spec{}, err
	}
	label, ok := workbookLabel(kind)
	if !ok {
		return Spec{}, fmt.Errorf("%w: unsupported workbook %s", ErrFormat, kind)
	}
	header := []Cell{{Type: "text", Value: "指标"}, {Type: "text", Value: "数值"}, {Type: "text", Value: "单位"}, {Type: "text", Value: "期间"}}
	raw := [][]Cell{header}
	board := [][]Cell{header}
	for _, f := range facts {
		row := []Cell{
			{Type: "text", Value: f.Locator},
			factValueCell(f.Value),
			{Type: "text", Value: f.Unit},
			periodCell(f.Period),
		}
		raw = append(raw, row)
		board = append(board, row)
	}
	if len(raw) == 1 {
		raw = append(raw, []Cell{{Type: "text", Value: "无已核对事实"}, factValueCell(""), {Type: "text", Value: ""}, {Type: "text", Value: ""}})
		board = raw
	}
	calc := [][]Cell{
		{{Type: "text", Value: "推导项"}, {Type: "text", Value: "公式或说明"}},
		{{Type: "text", Value: "记录条数"}, {Type: "formula", Value: "=COUNTA('原始数据'!A2:A1048576)"}},
	}
	locked := make([]Fact, len(facts))
	copy(locked, facts)
	for i := range locked {
		locked[i].Locked = true
	}
	return Spec{
		SchemaVersion: 2,
		Kind:          XLSX,
		Title:         title,
		TemplateID:    kind,
		Facts:         locked,
		Sheets: []Sheet{
			{Name: "说明", Rows: [][]Cell{{{Type: "text", Value: label + "：输入在原始数据，推导在计算，看板只展示已核对事实。"}}}},
			{Name: "原始数据", FreezeHeader: true, Rows: raw},
			{Name: "计算", Rows: calc},
			{Name: "看板", FreezeHeader: true, Rows: board},
		},
	}, nil
}

func factNodeUsable(n Node) bool {
	kind := strings.TrimPrefix(n.Kind, "cell:")
	return kind != "formula" && kind != "error" && kind != "invalid"
}

func LocateFactNode(insp Inspection, fact Fact) (Node, bool) {
	for _, n := range insp.Nodes {
		if factNodeUsable(n) && n.Text == fact.Value {
			return n, true
		}
	}
	for _, n := range insp.Nodes {
		if factNodeUsable(n) && factAppears(Inspection{Nodes: []Node{n}}, fact) {
			return n, true
		}
	}
	return Node{}, false
}

func factAppears(insp Inspection, f Fact) bool {
	for _, n := range insp.Nodes {
		if !factNodeUsable(n) {
			continue
		}
		if n.Text == f.Value {
			return true
		}
		if strings.Contains(n.Text, f.Value) && (f.Unit == "" || strings.Contains(n.Text, f.Unit)) {
			return true
		}
	}
	return false
}

type FactRef struct {
	FactID string `json:"factId"`
	NodeID string `json:"nodeId"`
	Kind   Kind   `json:"kind"`
	Part   string `json:"part,omitempty"`
}

func FindFactRefs(facts []Fact, inspections []Inspection) []FactRef {
	var out []FactRef
	for _, fact := range facts {
		for _, insp := range inspections {
			node, ok := LocateFactNode(insp, fact)
			if !ok {
				continue
			}
			out = append(out, FactRef{FactID: fact.FactID, NodeID: node.ID, Kind: insp.Kind, Part: node.Part})
		}
	}
	return out
}

func AssertFactSetCoverage(facts []Fact, inspections []Inspection) error {
	if err := ValidateFactSet(facts); err != nil {
		return err
	}
	for _, f := range facts {
		if !f.Locked {
			continue
		}
		for _, insp := range inspections {
			if !factAppears(insp, f) {
				return ErrFactConflict
			}
		}
	}
	return nil
}

func factValueCell(value string) Cell {
	value = strings.TrimSpace(value)
	if value == "" {
		return Cell{Type: "text", Value: "—"}
	}
	if decimalPattern.MatchString(value) {
		return Cell{Type: "number", Value: value}
	}
	return Cell{Type: "text", Value: value}
}

func periodCell(value string) Cell {
	if _, err := time.Parse("2006-01-02", value); err == nil {
		return Cell{Type: "date", Value: value}
	}
	return Cell{Type: "text", Value: value}
}

func workbookLabel(kind string) (string, bool) {
	switch kind {
	case "ops-ledger":
		return "经营台账", true
	case "sales-pipeline":
		return "销售过程", true
	case "project-tracker":
		return "项目跟踪", true
	default:
		return "", false
	}
}
