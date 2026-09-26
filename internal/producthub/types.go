package producthub

import "context"

// Card is one atomic product function after merge.
type Card struct {
	StableKey   string     `json:"stable_key"`
	Name        string     `json:"name"`
	NameEN      string     `json:"name_en"`
	Domain      string     `json:"domain"`
	Module      string     `json:"module"`
	Summary     string     `json:"summary"`
	Description string     `json:"description"`
	Attributes  Attributes `json:"attributes"`
	Methods     []Method   `json:"methods"`
	Chain       Chain      `json:"chain"`
	ChainClass  string     `json:"chain_class,omitempty"`
	Scaffold    Scaffold   `json:"scaffold"`
	Principle   string     `json:"principle,omitempty"`
	Logic       string     `json:"logic,omitempty"`
	Tech        string     `json:"tech,omitempty"`
	Analysis    string     `json:"analysis,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	Provenance  string     `json:"provenance"`
	Source      string     `json:"source"`
	Probe       ProbeScore `json:"probe"`
	Version     string     `json:"version"`
}

type Attributes struct {
	Operations   []string `json:"operations"`
	Tools        []string `json:"tools"`
	MCPs         []string `json:"mcps"`
	Skills       []string `json:"skills"`
	Capabilities []string `json:"capabilities"`
}

type Method struct {
	Type       string `json:"type"`
	Entry      string `json:"entry"`
	Continuous string `json:"continuous,omitempty"`
}

type Chain struct {
	Steps    []Step   `json:"steps"`
	Branches []Branch `json:"branches"`
}

type Step struct {
	Index       int    `json:"index"`
	Name        string `json:"name"`
	Detail      string `json:"detail"`
	Description string `json:"description"`
}

type Branch struct {
	Type        string `json:"type"`
	FromStep    int    `json:"from_step"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Retry       *Fork  `json:"retry,omitempty"`
	Fallback    *Fork  `json:"fallback,omitempty"`
}

type Fork struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	OnSuccess   string `json:"on_success,omitempty"`
	OnFail      string `json:"on_fail,omitempty"`
}

// Scaffold is the base the function stands on: page, bridge, setting, runtime.
type Scaffold struct {
	Pages    []string `json:"pages"`
	Bridge   []string `json:"bridge"`
	Settings []string `json:"settings"`
	Runtime  []string `json:"runtime"`
}

type ProbeScore struct {
	Passed int `json:"passed"`
	Total  int `json:"total"`
}

type Candidate struct {
	StableKey   string
	Name        string
	NameEN      string
	Domain      string
	Module      string
	Summary     string
	Description string
	Source      string
	ChainClass  string
	Attributes  Attributes
	Scaffold    Scaffold
	Methods     []Method
}

type Change struct {
	StableKey string   `json:"stable_key"`
	Kind      string   `json:"kind"` // added | updated | removed
	Title     string   `json:"title,omitempty"`
	Summary   string   `json:"summary,omitempty"`
	Impacts   []string `json:"impacts,omitempty"`
}

type Finding struct {
	Severity    string `json:"severity"`
	ErrorCode   string `json:"error_code"`
	StableKey   string `json:"stable_key"`
	Title       string `json:"title"`
	Evidence    string `json:"evidence"`
	RootCause   string `json:"root_cause"`
	Fix         string `json:"fix"`
	Verify      string `json:"verify"`
	Status      string `json:"status"`
	ApplyPrompt string `json:"apply_prompt,omitempty"`
	Plan        string `json:"plan,omitempty"`
	AppliedAt   string `json:"applied_at,omitempty"`
	SkillID     string `json:"skill_id,omitempty"`
	SkillName   string `json:"skill_name,omitempty"`
	SkillOutput string `json:"skill_output,omitempty"`
}

type Enrichment struct {
	StableKey string   `json:"stable_key"`
	Summary   string   `json:"summary,omitempty"`
	Methods   []Method `json:"methods,omitempty"`
	Tags      []string `json:"tags,omitempty"`
}

type ApplyLog struct {
	ID          string `json:"id"`
	ErrorCode   string `json:"error_code"`
	StableKey   string `json:"stable_key"`
	Plan        string `json:"plan"`
	SkillID     string `json:"skill_id,omitempty"`
	SkillName   string `json:"skill_name,omitempty"`
	SkillOutput string `json:"skill_output,omitempty"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

type ApplyResult struct {
	OK          bool   `json:"ok"`
	Applied     bool   `json:"applied"`
	Count       int    `json:"count"`
	Status      string `json:"status"`
	Plan        string `json:"plan"`
	SkillID     string `json:"skillId,omitempty"`
	SkillName   string `json:"skillName,omitempty"`
	SkillOutput string `json:"skillOutput,omitempty"`
	ErrorCode   string `json:"errorCode,omitempty"`
	StableKey   string `json:"stableKey,omitempty"`
}

type ConsultResult struct {
	SkillID   string
	SkillName string
	Output    string
}

type Collaborator interface {
	Consult(ctx context.Context, prompt string) (ConsultResult, error)
}

type Edition struct {
	EditionID      string     `json:"edition_id"`
	GeneratedAt    string     `json:"generated_at"`
	CardCount      int        `json:"card_count"`
	Added          int        `json:"added"`
	Updated        int        `json:"updated"`
	Removed        int        `json:"removed"`
	HealthScore    int        `json:"health_score"`
	LiveProbe      ProbeScore `json:"liveProbe,omitempty"`
	CatalogProbe   ProbeScore `json:"catalogProbe,omitempty"`
	Features       []Card     `json:"features"`
	Changes        []Change   `json:"changes"`
	Findings       []Finding  `json:"findings"`
	ReportMarkdown string     `json:"report_markdown"`
	ReportHTML     string     `json:"report_html"`
	Graph          Graph      `json:"graph"`
	ProductVersion string     `json:"product_version,omitempty"`
	Digest         string     `json:"digest"`
}

type Graph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

type GraphNode struct {
	ID          string `json:"id"`
	StableKey   string `json:"stable_key"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Domain      string `json:"domain,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Description string `json:"description,omitempty"`
	Principle   string `json:"principle,omitempty"`
	Logic       string `json:"logic,omitempty"`
	Tech        string `json:"tech,omitempty"`
	Analysis    string `json:"analysis,omitempty"`
}

type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Rel  string `json:"rel"`
}

type NodeTag struct {
	StableKey  string `json:"stable_key"`
	Vocab      string `json:"vocab"`
	Value      string `json:"value"`
	AssignedBy string `json:"assigned_by"`
}

type DomainStat struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Modules int    `json:"modules"`
	Cards   int    `json:"cards"`
}

type Overview struct {
	Product     string       `json:"product"`
	EditionID   string       `json:"editionId"`
	GeneratedAt string       `json:"generatedAt"`
	CardCount   int          `json:"cardCount"`
	HealthScore int          `json:"healthScore"`
	Added       int          `json:"added"`
	Updated     int          `json:"updated"`
	Removed     int          `json:"removed"`
	ProbePassed int          `json:"probePassed"`
	ProbeTotal  int          `json:"probeTotal"`
	LiveChecked bool         `json:"liveChecked,omitempty"`
	Domains     []DomainStat `json:"domains"`
	Tags        []string     `json:"tags"`
}
