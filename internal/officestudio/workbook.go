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

type FactBinding struct {
	FactID           string `json:"factId"`
	Value            string `json:"value"`
	Unit             string `json:"unit,omitempty"`
	Currency         string `json:"currency,omitempty"`
	Period           string `json:"period,omitempty"`
	Rounding         string `json:"rounding,omitempty"`
	SourceVersionSHA string `json:"sourceVersionSha,omitempty"`
	SourceNodeDigest string `json:"sourceNodeDigest"`
	TargetNodeDigest string `json:"targetNodeDigest,omitempty"`
	Verification     string `json:"verification,omitempty"`
}

func LocateFactNode(insp Inspection, fact Fact) (Node, bool) {
	b, ok := BindFact(insp, fact)
	if !ok {
		return Node{}, false
	}
	for _, n := range insp.Nodes {
		if n.Digest == b.SourceNodeDigest {
			return n, true
		}
	}
	return Node{}, false
}

func BindFact(insp Inspection, fact Fact) (FactBinding, bool) {
	var hits []Node
	for _, n := range insp.Nodes {
		if !factNodeUsable(n) || !typedValueEqual(n.Text, fact.Value) {
			continue
		}
		if !factContextOK(insp, n, fact) {
			continue
		}
		hits = append(hits, n)
	}
	if len(hits) == 0 {
		return FactBinding{}, false
	}
	if strings.TrimSpace(fact.Currency) == "" && strings.TrimSpace(fact.Period) == "" && distinctFactContexts(insp, hits) > 1 {
		return FactBinding{}, false
	}
	chosen := hits[0]
	for _, n := range hits {
		if n.Text == fact.Value {
			chosen = n
			break
		}
	}
	return FactBinding{
		FactID: fact.FactID, Value: fact.Value, Unit: fact.Unit, Currency: fact.Currency, Period: fact.Period,
		SourceNodeDigest: chosen.Digest, Verification: "typed-node",
	}, true
}

func factAppears(insp Inspection, f Fact) bool {
	for _, n := range insp.Nodes {
		if !factNodeUsable(n) || !typedValueEqual(n.Text, f.Value) {
			continue
		}
		if n.Text == f.Value {
			return true
		}
		if f.Unit == "" || strings.Contains(n.Text, f.Unit) {
			return true
		}
	}
	return false
}

func typedValueEqual(text, value string) bool {
	if text == value || value == "" {
		return text == value
	}
	for start := 0; start <= len(text)-len(value); start++ {
		if text[start:start+len(value)] != value {
			continue
		}
		if start > 0 && isASCIIDigit(text[start-1]) {
			continue
		}
		if start+len(value) < len(text) && isASCIIDigit(text[start+len(value)]) {
			continue
		}
		return true
	}
	return false
}

func isASCIIDigit(b byte) bool { return b >= '0' && b <= '9' }

func factContextOK(insp Inspection, n Node, f Fact) bool {
	ctx := nodeFactContext(insp, n)
	if f.Unit != "" && !strings.Contains(ctx, f.Unit) {
		return false
	}
	if f.Currency != "" && !strings.Contains(ctx, f.Currency) {
		return false
	}
	if f.Period != "" && !strings.Contains(ctx, f.Period) {
		return false
	}
	return true
}

func nodeFactContext(insp Inspection, n Node) string {
	if !strings.HasPrefix(n.Locator, "cell:") {
		return n.Text
	}
	_, row, ok := splitA1(strings.TrimPrefix(n.Locator, "cell:"))
	if !ok {
		return n.Text
	}
	var b strings.Builder
	for _, o := range insp.Nodes {
		if o.Part != n.Part {
			continue
		}
		_, r, ok := splitA1(strings.TrimPrefix(o.Locator, "cell:"))
		if ok && r == row {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(o.Text)
		}
	}
	if b.Len() == 0 {
		return n.Text
	}
	return b.String()
}

func distinctFactContexts(insp Inspection, nodes []Node) int {
	seen := map[string]bool{}
	for _, n := range nodes {
		ctx := nodeFactContext(insp, n)
		seen[ctx] = true
	}
	return len(seen)
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
