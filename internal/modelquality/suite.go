package modelquality

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lunitide/lunitide/internal/officestudio"
)

const evalCasesRel = "testdata/upgrade-v1/eval-cases.json"

var FixedCaseIDs = []string{
	"D01", "D02", "D03", "P01", "P02", "P03",
	"X01", "X02", "X03", "F01", "F02", "F03",
	"R01", "R02", "R03", "R04", "L01", "L02", "L03", "L04",
	"C01", "C02", "C03", "C04",
}

type Budget struct {
	MaxTotalTokens       int64 `json:"maxTotalTokens"`
	MaxOutputTokens      int64 `json:"maxOutputTokens"`
	MaxModelAttempts     int   `json:"maxModelAttempts"`
	MaxActiveMillis      int64 `json:"maxActiveMillis"`
	MaxOutputBytes       int64 `json:"maxOutputBytes"`
	CloseoutOutputTokens int64 `json:"closeoutOutputTokens"`
}

type Scenario struct {
	ScenarioID          string `json:"scenarioId"`
	Mode                string `json:"mode"`
	StatisticGroup      string `json:"statisticGroup"`
	ExpectedTaskSuccess bool   `json:"expectedTaskSuccess"`
}

type EvalCase struct {
	CaseID              string          `json:"caseId"`
	Title               string          `json:"title"`
	Category            string          `json:"category"`
	Format              string          `json:"format"`
	Prompt              string          `json:"prompt"`
	FixtureOnly         bool            `json:"fixtureOnly"`
	ExpectedTaskSuccess bool            `json:"expectedTaskSuccess"`
	Budget              Budget          `json:"budget"`
	Inputs              json.RawMessage `json:"inputs"`
	Checks              []string        `json:"checks"`
	Scenarios           []Scenario      `json:"scenarios"`
	OracleLabel         string          `json:"oracle"`
	InputDigest         string          `json:"-"`
}

func (c EvalCase) RowCount() int {
	var in struct {
		Rows json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal(c.Inputs, &in); err != nil || len(in.Rows) == 0 {
		return 0
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(in.Rows, &rows); err != nil {
		return 0
	}
	return len(rows)
}

type HoldOutCase struct {
	CaseID      string
	Title       string
	Timezone    string
	RowCount    int
	Budget      Budget
	InputDigest string
	Inputs      json.RawMessage
}

type Observation struct {
	CaseID     string
	ScenarioID string
	Artifact   []byte
	Kind       officestudio.Kind
	Text       string
	Values     map[string]string
}

type Verdict struct {
	Pass                 bool
	Reasons              []string
	LiveSuccessIncrement bool
}

type Outcome struct {
	CaseID        string
	ScenarioID    string
	FixtureOnly   bool
	Mode          string
	FaultScenario bool
	OraclePass    bool
	EvidenceKind  string
	HoldOut       bool
	Verdict       *Verdict
}

type Accounting struct {
	LiveSuccess       int
	FixturePass       int
	FaultContractPass int
	HostPass          int
	OracleFail        int
	HoldOutSeen       int
	EvalComplete      bool
	HoldOutComplete   bool
	LiveQualified     bool
}

type oracleFn func(s *Suite, scenario string, obs Observation) Verdict

type Suite struct {
	cases   map[string]EvalCase
	oracles map[string]oracleFn
	holdOut map[string]HoldOutCase
}

func LoadEvalSuite() (*Suite, error) {
	raw, err := os.ReadFile(evalCasesPath())
	if err != nil {
		return nil, err
	}
	var file struct {
		Cases []EvalCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	s := &Suite{
		cases:   map[string]EvalCase{},
		oracles: map[string]oracleFn{},
		holdOut: map[string]HoldOutCase{},
	}
	for _, c := range file.Cases {
		c.InputDigest = sha256Hex(c.Inputs)
		s.cases[c.CaseID] = c
	}
	for _, id := range FixedCaseIDs {
		if _, ok := s.cases[id]; !ok {
			return nil, fmt.Errorf("eval suite missing %s", id)
		}
	}
	registerOracles(s)
	registerHoldOut(s)
	return s, nil
}

func HoldOutListed() []string {
	return []string{"H-D01", "H-D02", "H-X01"}
}

func (s *Suite) CaseIDs() []string {
	out := make([]string, len(FixedCaseIDs))
	copy(out, FixedCaseIDs)
	return out
}

func (s *Suite) MustCase(id string) EvalCase {
	return s.cases[id]
}

func (s *Suite) HoldOutCase(id string) HoldOutCase {
	return s.holdOut[id]
}

func (s *Suite) HasIndependentOracle(id string) bool {
	fn, ok := s.oracles[id]
	return ok && fn != nil
}

func (s *Suite) Evaluate(id, scenario string, obs Observation) Verdict {
	fn := s.oracles[id]
	if fn == nil {
		return failVerdict("no independent oracle for " + id)
	}
	if obs.Values == nil {
		obs.Values = map[string]string{}
	}
	if obs.ScenarioID == "" {
		obs.ScenarioID = scenario
	}
	return fn(s, scenario, obs)
}

func (s *Suite) IsEvalComplete() bool { return false }

func (s *Suite) IsLiveQualified() bool { return false }

type FrozenAccounting struct {
	Acc  Accounting
	seen map[string]Outcome
}

func NewFrozenAccounting() *FrozenAccounting {
	return &FrozenAccounting{seen: map[string]Outcome{}}
}

func (s *Suite) RecordFirst(acc *FrozenAccounting, o Outcome) {
	if acc.seen == nil {
		acc.seen = map[string]Outcome{}
	}
	key := o.CaseID + "/" + o.ScenarioID
	if _, ok := acc.seen[key]; ok {
		return
	}
	acc.seen[key] = o
	s.Record(&acc.Acc, o)
}

func (s *Suite) Record(acc *Accounting, o Outcome) {
	acc.EvalComplete = false
	acc.HoldOutComplete = false
	acc.LiveQualified = false
	if _, hold := s.holdOut[o.CaseID]; hold {
		acc.HoldOutSeen++
		return
	}
	c, ok := s.cases[o.CaseID]
	if !ok {
		acc.OracleFail++
		return
	}
	pass := o.OraclePass
	liveInc := false
	if o.Verdict != nil {
		pass = o.Verdict.Pass
		liveInc = o.Verdict.LiveSuccessIncrement
	}
	if !pass {
		acc.OracleFail++
		return
	}
	if isFaultScenario(c, o.ScenarioID) {
		acc.FaultContractPass++
		return
	}
	if c.FixtureOnly || scenarioMode(c, o.ScenarioID) == "fixture" {
		acc.FixturePass++
		return
	}
	if liveInc && !c.FixtureOnly {
		acc.LiveSuccess++
		return
	}
	acc.HostPass++
}

func isFaultScenario(_ EvalCase, scenario string) bool {
	return scenario == "missing-glyph"
}

func scenarioMode(c EvalCase, scenario string) string {
	for _, sc := range c.Scenarios {
		if sc.ScenarioID == scenario {
			return sc.Mode
		}
	}
	if scenario == "" && c.FixtureOnly {
		return "fixture"
	}
	return ""
}

func evalCasesPath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return path.Join("internal", "modelquality", evalCasesRel)
	}
	return filepath.Join(filepath.Dir(file), filepath.FromSlash(evalCasesRel))
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func registerHoldOut(s *Suite) {
	budget := Budget{MaxTotalTokens: 262144, MaxOutputTokens: 65536, MaxModelAttempts: 48, MaxActiveMillis: 900000, MaxOutputBytes: 1048576, CloseoutOutputTokens: 1024}
	d01Facts := []map[string]any{
		{"factId": "revenue_q1", "value": "2100000", "unit": "CNY", "period": "2026Q3", "sourceId": "holdout-sales-v1", "locked": true},
		{"factId": "revenue_q2", "value": "1800000", "unit": "CNY", "period": "2026Q4", "sourceId": "holdout-sales-v1", "locked": true},
	}
	d01Inputs, _ := json.Marshal(map[string]any{"facts": d01Facts, "timezone": "Asia/Tokyo", "title": "留出研究报告（东京时区）"})
	s.holdOut["H-D01"] = HoldOutCase{
		CaseID: "H-D01", Title: "留出研究报告（东京时区）", Timezone: "Asia/Tokyo",
		Budget: budget, Inputs: d01Inputs, InputDigest: sha256Hex(d01Inputs),
	}
	d02Inputs, _ := json.Marshal(map[string]any{"rows": holdOutRows(40), "timezone": "America/New_York"})
	s.holdOut["H-D02"] = HoldOutCase{
		CaseID: "H-D02", Title: "留出长表分页", Timezone: "America/New_York", RowCount: 40,
		Budget: budget, Inputs: d02Inputs, InputDigest: sha256Hex(d02Inputs),
	}
	x01Inputs, _ := json.Marshal(map[string]any{"orders": 12, "expectedTotalCents": 88000, "timezone": "Europe/Berlin"})
	s.holdOut["H-X01"] = HoldOutCase{
		CaseID: "H-X01", Title: "留出订单台账", Timezone: "Europe/Berlin", RowCount: 12,
		Budget: budget, Inputs: x01Inputs, InputDigest: sha256Hex(x01Inputs),
	}
	s.oracles["H-D01"] = evalHoldOutFacts
	s.oracles["H-D02"] = evalHoldOutRows
	s.oracles["H-X01"] = evalHoldOutCents
}

func holdOutRows(n int) []map[string]any {
	out := make([]map[string]any, n)
	for i := 0; i < n; i++ {
		out[i] = map[string]any{"id": i + 1, "title": fmt.Sprintf("留出条目%d", i+1)}
	}
	return out
}

func requireVal(obs Observation, key, want string) string {
	got := obs.Values[key]
	if got != want {
		return fmt.Sprintf("%s=%q want %q", key, got, want)
	}
	return ""
}

func failVerdict(reasons ...string) Verdict {
	var out []string
	for _, r := range reasons {
		if strings.TrimSpace(r) != "" {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		out = []string{"oracle failed"}
	}
	return Verdict{Pass: false, Reasons: out}
}

func passVerdict() Verdict {
	return Verdict{Pass: true}
}
