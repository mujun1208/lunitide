package officeapp

import (
	"encoding/json"
	"fmt"
	"strings"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/officestudio"
)

func BriefFromCheckpoint(raw json.RawMessage) officestudio.Brief {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return officestudio.Brief{}
	}
	var brief officestudio.Brief
	_ = json.Unmarshal(fields["brief"], &brief)
	return brief
}

func WithTaskBrief(previous json.RawMessage, brief officestudio.Brief) json.RawMessage {
	fields := map[string]json.RawMessage{}
	_ = json.Unmarshal(previous, &fields)
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	fields["brief"] = encode(brief)
	return encode(fields)
}

func applyTaskBrief(task domain.Task, spec officestudio.Spec) officestudio.Spec {
	raw := BriefFromCheckpoint(task.Checkpoint)
	brief := NormalizeBrief(raw)
	if len(brief.Facts) > 0 {
		spec.Facts = append(append([]officestudio.Fact(nil), brief.Facts...), spec.Facts...)
	}
	if plan, err := officestudio.PlanNarrative(spec, brief); err == nil {
		spec = officestudio.ApplyNarrativePlan(spec, plan)
	}
	if spec.Kind == officestudio.PDF {
		if strings.TrimSpace(spec.Audience) == "" {
			spec.Audience = strings.TrimSpace(raw.Audience)
		}
		if strings.TrimSpace(spec.Purpose) == "" {
			spec.Purpose = strings.TrimSpace(raw.Purpose)
		}
		if strings.TrimSpace(spec.Confidentiality) == "" {
			spec.Confidentiality = strings.TrimSpace(raw.Confidentiality)
		}
	}
	conf := strings.TrimSpace(raw.Confidentiality)
	if spec.Kind == officestudio.DOCX {
		if conf != "" {
			if spec.Document == nil {
				spec.Document = &officestudio.DocumentOptions{}
			}
			if spec.Document.Header == "" {
				spec.Document.Header = conf
			} else if !strings.Contains(spec.Document.Header, conf) {
				spec.Document.Header = conf + " · " + spec.Document.Header
			}
		}
	}
	if spec.Kind == officestudio.PPTX && conf != "" {
		mark := "密级：" + conf
		for i := range spec.Slides {
			if strings.Contains(spec.Slides[i].Notes, conf) {
				continue
			}
			if spec.Slides[i].Notes == "" {
				spec.Slides[i].Notes = mark
			} else {
				spec.Slides[i].Notes += "\n" + mark
			}
		}
	}
	if spec.Kind == officestudio.XLSX && conf != "" {
		mark := "密级：" + conf
		for i := range spec.Sheets {
			if spec.Sheets[i].Name != "说明" {
				continue
			}
			if len(spec.Sheets[i].Rows) == 0 || len(spec.Sheets[i].Rows[0]) == 0 {
				spec.Sheets[i].Rows = [][]officestudio.Cell{{{Type: "text", Value: mark}}}
				break
			}
			cell := &spec.Sheets[i].Rows[0][0]
			if !strings.Contains(cell.Value, conf) {
				if cell.Value == "" {
					cell.Value = mark
				} else {
					cell.Value += " " + mark
				}
			}
			break
		}
	}
	return spec
}

func FormatBriefEvidence(raw officestudio.Brief) string {
	audience := strings.TrimSpace(raw.Audience)
	purpose := strings.TrimSpace(raw.Purpose)
	conf := strings.TrimSpace(raw.Confidentiality)
	line := "Task brief"
	if audience != "" {
		line += " audience=" + audience
	} else {
		line += " audience unset"
	}
	if purpose != "" {
		line += " purpose=" + purpose
	} else {
		line += " purpose unset"
	}
	if raw.TargetLength > 0 {
		line += fmt.Sprintf(" targetLength=%d", raw.TargetLength)
	} else {
		line += " targetLength unset"
	}
	if conf != "" {
		line += " confidentiality=" + conf
	}
	line += ". Unset fields are not authored; do not invent weekly-report rates, savings percentages, or a stricter classification."
	return line
}

func NormalizeBrief(in officestudio.Brief) officestudio.Brief {
	out := in
	if out.Audience == "" {
		out.Audience = "管理层"
	}
	if out.Purpose == "" {
		out.Purpose = "经营汇报"
	}
	if out.Language == "" {
		out.Language = "zh-CN"
	}
	if out.TargetLength == 0 {
		out.TargetLength = 12
	}
	if len(out.Deliverables) == 0 {
		out.Deliverables = []officestudio.Kind{officestudio.PPTX}
	}
	return out
}
