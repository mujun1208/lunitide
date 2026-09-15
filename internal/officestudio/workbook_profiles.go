package officestudio

import (
	"fmt"
	"strconv"
	"strings"
)

type OrderLine struct {
	OrderID             string
	Date                string
	AmountCents         int64
	DiscountBasisPoints int
	Region              string
	Note                string
}

func WorkbookProfileIDs() []string {
	return []string{"ops", "brand", "editorial"}
}

func PlanWorkbookProfile(profile, title string, facts []Fact) (Spec, error) {
	if err := ValidateFactSet(facts); err != nil {
		return Spec{}, err
	}
	locked := lockFacts(facts)
	header := []Cell{{Type: "text", Value: "指标"}, {Type: "text", Value: "数值"}, {Type: "text", Value: "单位"}, {Type: "text", Value: "期间"}}
	data := [][]Cell{header}
	for _, f := range locked {
		data = append(data, []Cell{
			{Type: "text", Value: firstFactLabel([]Fact{f})},
			factValueCell(f.Value),
			{Type: "text", Value: f.Unit},
			periodCell(f.Period),
		})
	}
	if len(data) == 1 {
		data = append(data, []Cell{{Type: "text", Value: "无已核对事实"}, {Type: "text", Value: "—"}, {Type: "text", Value: ""}, {Type: "text", Value: ""}})
	}
	names, ok := workbookProfileSheets(profile)
	if !ok {
		return Spec{}, fmt.Errorf("%w: unsupported workbook profile %s", ErrFormat, profile)
	}
	emptyNote := [][]Cell{{{Type: "text", Value: names[0] + "仅展示已核对事实，无业务数据时不编造示例收入。"}}}
	calc := [][]Cell{
		{{Type: "text", Value: "推导项"}, {Type: "text", Value: "公式"}},
		{{Type: "text", Value: "记录条数"}, {Type: "formula", Value: "=COUNTA('" + names[0] + "'!A2:A1048576)"}},
	}
	templateID := map[string]string{"ops": "ops-ledger", "brand": "sales-pipeline", "editorial": "project-tracker"}[profile]
	return Spec{
		SchemaVersion: 2,
		Kind:          XLSX,
		Title:         title,
		TemplateID:    templateID,
		Facts:         locked,
		Sheets: []Sheet{
			{Name: names[0], FreezeHeader: true, Rows: data},
			{Name: names[1], Rows: calc},
			{Name: names[2], Rows: emptyNote},
		},
	}, nil
}

func workbookProfileSheets(profile string) ([3]string, bool) {
	switch strings.TrimSpace(profile) {
	case "ops":
		return [3]string{"运营明细", "汇总", "异常"}, true
	case "brand":
		return [3]string{"销售漏斗", "预算", "ROI"}, true
	case "editorial":
		return [3]string{"数据字典", "分析", "来源"}, true
	default:
		return [3]string{}, false
	}
}

func X01OrdersSpec(orders []OrderLine) (Spec, error) {
	if len(orders) == 0 {
		return Spec{}, fmt.Errorf("%w: orders required", ErrFormat)
	}
	rows := [][]Cell{{{Type: "text", Value: "订单号"}, {Type: "text", Value: "日期"}, {Type: "text", Value: "金额分"}, {Type: "text", Value: "折扣基点"}, {Type: "text", Value: "区域"}, {Type: "text", Value: "备注"}}}
	for _, o := range orders {
		note := o.Note
		if note == "" {
			note = ""
		}
		rows = append(rows, []Cell{
			{Type: "text", Value: o.OrderID},
			periodCell(o.Date),
			{Type: "number", Value: strconv.FormatInt(o.AmountCents, 10), Format: "0"},
			{Type: "number", Value: strconv.Itoa(o.DiscountBasisPoints), Format: "0"},
			{Type: "text", Value: o.Region},
			{Type: "text", Value: note},
		})
	}
	last := strconv.Itoa(len(orders) + 1)
	return Spec{
		SchemaVersion: 2,
		Kind:          XLSX,
		Title:         "经营订单工作簿",
		TemplateID:    "ops-ledger",
		Sheets: []Sheet{
			{Name: "运营明细", FreezeHeader: true, Rows: rows},
			{Name: "汇总", Rows: [][]Cell{
				{{Type: "text", Value: "项目"}, {Type: "text", Value: "公式"}},
				{{Type: "text", Value: "金额合计分"}, {Type: "formula", Value: "=SUM('运营明细'!C2:C" + last + ")"}},
			}},
			{Name: "异常", Rows: [][]Cell{{{Type: "text", Value: "负向与缺失值保留在运营明细中，此处不重写金额。"}}}},
		},
	}, nil
}

func X02SalesSpec() (Spec, error) {
	return Spec{
		SchemaVersion: 2,
		Kind:          XLSX,
		Title:         "销售看板",
		TemplateID:    "sales-pipeline",
		Sheets: []Sheet{
			{
				Name: "输入", FreezeHeader: true,
				Rows: [][]Cell{
					{{Type: "text", Value: "期间"}, {Type: "text", Value: "销售额"}, {Type: "text", Value: "目标"}},
					{{Type: "text", Value: "2026-05"}, {Type: "number", Value: "12000000"}, {Type: "number", Value: "15000000"}},
					{{Type: "text", Value: "2026-06"}, {Type: "number", Value: "15000000"}, {Type: "number", Value: "16000000"}},
					{{Type: "text", Value: "零分母"}, {Type: "number", Value: "0"}, {Type: "number", Value: "300"}},
				},
				Charts: []SheetChart{{
					Type: "column", Title: "销售额", Categories: "A2:A3",
					Series: []SheetChartSeries{{Name: "销售额", Range: "B2:B3"}},
					Anchor: "E2", Width: 640, Height: 360, Legend: true,
				}},
			},
			{Name: "计算", Rows: [][]Cell{
				{{Type: "text", Value: "指标"}, {Type: "text", Value: "公式"}},
				{{Type: "text", Value: "环比"}, {Type: "formula", Value: "=('输入'!B3-'输入'!B2)/'输入'!B2"}},
				{{Type: "text", Value: "6月达成"}, {Type: "formula", Value: "='输入'!B3/'输入'!C3"}},
				{{Type: "text", Value: "零分母"}, {Type: "formula", Value: "=IF('输入'!B4=0,\"n/a\",'输入'!C4/'输入'!B4)"}},
			}},
			{Name: "看板", FreezeHeader: true, Rows: [][]Cell{
				{{Type: "text", Value: "期间"}, {Type: "text", Value: "销售额"}},
				{{Type: "text", Value: "2026-05"}, {Type: "formula", Value: "='输入'!B2"}},
				{{Type: "text", Value: "2026-06"}, {Type: "formula", Value: "='输入'!B3"}},
			}},
			{Name: "说明", Rows: [][]Cell{{{Type: "text", Value: "输入在输入表，计算用跨表公式，看板只引用已提供月份，不编造收入。"}}}},
		},
	}, nil
}

func X03SumChartSpec() (Spec, error) {
	return Spec{
		SchemaVersion: 2,
		Kind:          XLSX,
		Title:         "重算范围",
		Sheets: []Sheet{{
			Name: "数据", FreezeHeader: true,
			Rows: [][]Cell{
				{{Type: "text", Value: "项目"}, {Type: "text", Value: "数值"}},
				{{Type: "text", Value: "A"}, {Type: "number", Value: "10"}},
				{{Type: "text", Value: "B"}, {Type: "number", Value: "20"}},
				{{Type: "text", Value: "C"}, {Type: "number", Value: "30"}},
				{{Type: "text", Value: "D"}, {Type: "number", Value: "40"}},
				{{Type: "text", Value: "合计"}, {Type: "formula", Value: "=SUM(B2:B5)"}},
			},
			Charts: []SheetChart{{
				Type: "column", Title: "数值", Categories: "A2:A5",
				Series: []SheetChartSeries{{Name: "数值", Range: "B2:B5"}},
				Anchor: "E2", Width: 640, Height: 360, Legend: true,
			}},
		}},
	}, nil
}
