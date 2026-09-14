package modelquality

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/officestudio"
)

func ObserveLiveCase(suite *Suite, c EvalCase, modelText string) (Observation, Verdict, string) {
	scenario := LiveScenario(c)
	if c.FixtureOnly {
		return Observation{ScenarioID: scenario}, failVerdict("fixtureOnly"), "skipped_fixture_only"
	}
	switch c.CaseID {
	case "R01", "R02", "R03", "R04", "L01", "L02", "L03", "L04":
		return Observation{ScenarioID: scenario}, failVerdict("needs_host_runtime"), "needs_host_runtime"
	case "C01":
		obs := observeC01(modelText)
		obs.ScenarioID = scenario
		v := suite.Evaluate(c.CaseID, scenario, obs)
		return obs, withLiveIncrement(c, scenario, obs, v), statusOf(v)
	case "C02", "C03":
		obs := Observation{Text: extractCSV(modelText), ScenarioID: scenario}
		v := suite.Evaluate(c.CaseID, scenario, obs)
		return obs, withLiveIncrement(c, scenario, obs, v), statusOf(v)
	case "C04":
		obs, err := observeC04(modelText)
		if err != nil {
			return Observation{ScenarioID: scenario}, failVerdict("needs_structured_output"), "needs_structured_output"
		}
		obs.ScenarioID = scenario
		v := suite.Evaluate(c.CaseID, scenario, obs)
		return obs, withLiveIncrement(c, scenario, obs, v), statusOf(v)
	default:
		spec, err := parseSpecJSON(modelText)
		if err != nil {
			return Observation{ScenarioID: scenario}, failVerdict("needs_structured_output"), "needs_structured_output"
		}
		data, err := officestudio.Generate(spec)
		if err != nil {
			return Observation{ScenarioID: scenario}, failVerdict("generate_failed"), "oracle_fail"
		}
		obs := observationFromArtifact(c, spec, data)
		obs.ScenarioID = scenario
		v := suite.Evaluate(c.CaseID, scenario, obs)
		return obs, withLiveIncrement(c, scenario, obs, v), statusOf(v)
	}
}

func withLiveIncrement(c EvalCase, scenario string, obs Observation, v Verdict) Verdict {
	if c.FixtureOnly || scenario == "missing-glyph" || isFaultScenario(c, scenario) {
		return v
	}
	if c.CaseID == "C01" {
		if c01LiveSafe(obs) {
			v.LiveSuccessIncrement = true
		}
		return v
	}
	if v.Pass {
		v.LiveSuccessIncrement = true
	}
	return v
}

func c01LiveSafe(obs Observation) bool {
	inputs := []string{"2026-01-31T12:00:00Z", "2024-02-28T12:00:00Z"}
	for _, in := range inputs {
		ts, err := time.Parse(time.RFC3339, in)
		if err != nil {
			return false
		}
		want := IndependentNextDay(ts).UTC().Format(time.RFC3339)
		if obs.Values[in] != want {
			return false
		}
	}
	return true
}

func statusOf(v Verdict) string {
	if v.Pass {
		return "oracle_pass"
	}
	return "oracle_fail"
}

func observationFromArtifact(c EvalCase, spec officestudio.Spec, data []byte) Observation {
	kind := spec.Kind
	if kind == "" {
		kind = officestudio.Kind(c.Format)
	}
	insp, err := officestudio.Inspect(kind, data)
	text := ""
	if err == nil {
		text = inspectText(insp)
	}
	obs := Observation{Artifact: data, Kind: kind, Text: text, Values: map[string]string{}}
	switch c.CaseID {
	case "P01":
		obs.Values["slides"] = strconv.Itoa(len(spec.Slides))
		charts := 0
		for _, s := range spec.Slides {
			charts += len(s.Charts)
		}
		obs.Values["charts"] = strconv.Itoa(charts)
	case "P02":
		if obs.Text == "" {
			obs.Text = spec.Title
			for _, s := range spec.Slides {
				obs.Text += "\n" + s.Title
				obs.Text += "\n" + strings.Join(s.Bullets, "\n")
			}
		}
	case "F03":
		want := "人民币￥100；达成率％；长度μm；温度℃；生僻字𠮷。"
		if obs.Text != "" && strings.Contains(obs.Text, want) {
			obs.Values["allGlyphsVisible"] = "true"
			obs.Values["pdfTextExact"] = "true"
		}
	}
	return obs
}

func observeC01(modelText string) Observation {
	vals := map[string]string{}
	raw := extractJSONObject(modelText)
	var obj map[string]string
	if json.Unmarshal(raw, &obj) == nil {
		for k, v := range obj {
			vals[k] = v
		}
	}
	return Observation{Values: vals}
}

func observeC04(modelText string) (Observation, error) {
	raw := extractJSONObject(modelText)
	var obj map[string]string
	if err := json.Unmarshal(raw, &obj); err != nil {
		return Observation{}, err
	}
	obs := Observation{Values: map[string]string{}}
	for _, format := range []string{"docx", "pptx", "xlsx", "pdf"} {
		if strings.TrimSpace(obj[format]) == "" {
			return Observation{}, fmt.Errorf("missing %s", format)
		}
		obs.Values[format] = obj[format]
	}
	return obs, nil
}

func parseSpecJSON(text string) (officestudio.Spec, error) {
	raw := extractJSONObject(text)
	if len(raw) == 0 {
		return officestudio.Spec{}, fmt.Errorf("needs_structured_output")
	}
	var spec officestudio.Spec
	if err := json.Unmarshal(raw, &spec); err == nil && spec.Kind != "" {
		return spec, nil
	}
	var wrap struct {
		Spec officestudio.Spec `json:"spec"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil && wrap.Spec.Kind != "" {
		return wrap.Spec, nil
	}
	return officestudio.Spec{}, fmt.Errorf("needs_structured_output")
}

func extractJSONObject(text string) []byte {
	s := strings.TrimSpace(text)
	if i := strings.Index(s, "```json"); i >= 0 {
		s = s[i+7:]
		if j := strings.Index(s, "```"); j >= 0 {
			s = s[:j]
		}
	} else if i := strings.Index(s, "```"); i >= 0 {
		s = s[i+3:]
		if j := strings.Index(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return nil
	}
	return []byte(s[start : end+1])
}

func extractCSV(text string) string {
	s := strings.TrimSpace(text)
	if i := strings.Index(s, "```"); i >= 0 {
		s = s[i+3:]
		if strings.HasPrefix(strings.ToLower(s), "csv") {
			s = s[3:]
		}
		if j := strings.Index(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	return strings.TrimSpace(s)
}

func inspectText(i officestudio.Inspection) string {
	var b strings.Builder
	b.WriteString(i.Preview)
	for _, n := range i.Nodes {
		b.WriteString(n.Text)
	}
	return b.String()
}

func LiveCasePrompt(c EvalCase) string {
	var b strings.Builder
	if c.Prompt != "" {
		b.WriteString(c.Prompt)
		b.WriteByte('\n')
	}
	switch c.CaseID {
	case "C01":
		b.WriteString("Return ONLY JSON mapping each input RFC3339 timestamp to the next calendar day in UTC RFC3339. Keys must be exactly 2026-01-31T12:00:00Z and 2024-02-28T12:00:00Z.\n")
	case "C02":
		b.WriteString("Return ONLY CSV with header id,updated_at,date. Keep 6-digit leading-zero IDs. Keep the latest updated_at per id. Keep empty dates empty.\nSource:\n")
		b.WriteString(c02PromptSourceCSV(c.Inputs))
	case "C03":
		b.WriteString("Return ONLY CSV with header month,region,amountCents summing the input rows. Do not invent rows.\nInputs: ")
		b.Write(c.Inputs)
		b.WriteByte('\n')
	case "C04":
		b.WriteString("Return ONLY JSON {\"docx\":\"...\",\"pptx\":\"...\",\"xlsx\":\"...\",\"pdf\":\"...\"} where each string contains every locked fact value, unit, and period exactly.\nFacts: ")
		b.Write(c.Inputs)
		b.WriteByte('\n')
	case "R01", "R02", "R03", "R04", "L01", "L02", "L03", "L04":
		b.WriteString("This case requires host runtime evidence. Reply with JSON {\"unsupported\":\"needs_host_runtime\"}.\n")
	default:
		b.WriteString("Return ONLY a JSON officestudio.Spec (schemaVersion 2) with kind, title, and content. Include every locked fact/value exactly. No markdown.\nInputs: ")
		b.Write(c.Inputs)
		b.WriteByte('\n')
	}
	return b.String()
}

func c02PromptSourceCSV(inputs json.RawMessage) string {
	rows, err := independentC02SourceRows(inputs)
	if err != nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("id,updated_at,date\n")
	for _, r := range rows {
		b.WriteString(r.ID)
		b.WriteByte(',')
		b.WriteString(r.UpdatedAt)
		b.WriteByte(',')
		b.WriteString(r.Date)
		b.WriteByte('\n')
	}
	return b.String()
}
