package modelquality

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/provider"
)

const (
	AuthorizedDeepSeekProviderID = "2XRBJW5R29H413TGKFBD6375BP"
	AuthorizedGLMProviderID      = "01M19DKAJQ2H9YB7DH0KZT286R"
	AuthorizedDeepSeekSuiteModel = "deepseek-v4-pro"
	AuthorizedDeepSeekProbeModel = "deepseek-flash"
	AuthorizedGLMSuiteModel      = "glm-5.3"
	ProfileDeepSeekFlashModel    = "deepseek-v4-flash"
	closeoutLiveOutputTokens     = 1024
	raisedLiveOutputTokens       = 4096
)

type InstalledModel struct {
	ModelID   string
	Kind      string
	IsDefault bool
}

type InstalledProvider struct {
	ID            string
	Name          string
	Protocol      string
	BaseURL       string
	CredentialRef string
	Status        string
	Models        []InstalledModel
}

type AuthorizedTarget struct {
	ProviderID   string `json:"providerId"`
	ProviderName string `json:"providerName"`
	OriginHost   string `json:"originHost"`
	Protocol     string `json:"protocol"`
	BaseURL      string `json:"baseUrl,omitempty"`
	ModelID      string `json:"modelId"`
	Role         string `json:"role"`
	Mismatch     string `json:"profileMismatch,omitempty"`
}

type ProfileMismatch struct {
	ProfileModelID   string `json:"profileModelId"`
	InstalledModelID string `json:"installedModelId"`
	Note             string `json:"note"`
}

type RejectedProvider struct {
	ProviderID   string `json:"providerId,omitempty"`
	ProviderName string `json:"providerName"`
	Reason       string `json:"reason"`
}

type TargetSelection struct {
	Targets         []AuthorizedTarget `json:"targets"`
	ProfileMismatch []ProfileMismatch  `json:"profileMismatch,omitempty"`
	Rejected        []RejectedProvider `json:"rejected,omitempty"`
}

type TokenUsage struct {
	InputTokens  int `json:"inputTokens,omitempty"`
	OutputTokens int `json:"outputTokens,omitempty"`
	TotalTokens  int `json:"totalTokens,omitempty"`
}

type ProbeRecord struct {
	ProviderName string      `json:"providerName"`
	OriginHost   string      `json:"originHost"`
	ModelID      string      `json:"modelId"`
	Role         string      `json:"role,omitempty"`
	Status       string      `json:"status"`
	LatencyMS    int64       `json:"latencyMs"`
	HTTPClass    string      `json:"httpClass,omitempty"`
	TokenUsage   *TokenUsage `json:"tokenUsage,omitempty"`
	ErrorCode    string      `json:"errorCode,omitempty"`
}

type CaseRecord struct {
	CaseID               string      `json:"caseId"`
	ScenarioID           string      `json:"scenarioId,omitempty"`
	FixtureOnly          bool        `json:"fixtureOnly,omitempty"`
	Status               string      `json:"status"`
	OraclePass           bool        `json:"oraclePass"`
	LiveSuccessIncrement bool        `json:"liveSuccessIncrement"`
	EvidenceKind         string      `json:"evidenceKind,omitempty"`
	LatencyMS            int64       `json:"latencyMs,omitempty"`
	HTTPClass            string      `json:"httpClass,omitempty"`
	TokenUsage           *TokenUsage `json:"tokenUsage,omitempty"`
	ErrorCode            string      `json:"errorCode,omitempty"`
	OutputTokensCap      int         `json:"outputTokensCap,omitempty"`
	CapRaised            bool        `json:"capRaised,omitempty"`
	Reasons              []string    `json:"reasons,omitempty"`
}

type TargetRun struct {
	ProviderName string       `json:"providerName"`
	OriginHost   string       `json:"originHost"`
	ModelID      string       `json:"modelId"`
	LivePass     int          `json:"livePass"`
	LiveFail     int          `json:"liveFail"`
	Skipped      int          `json:"skipped"`
	Accounting   Accounting   `json:"accounting"`
	Cases        []CaseRecord `json:"cases"`
}

type LiveEvidence struct {
	SchemaVersion   int                `json:"schemaVersion"`
	CheckedAt       string             `json:"checkedAt"`
	LiveQualified   bool               `json:"liveQualified"`
	LayersQ         string             `json:"layersQ"`
	ProfileMismatch []ProfileMismatch  `json:"profileMismatch,omitempty"`
	Rejected        []RejectedProvider `json:"rejected,omitempty"`
	Probes          []ProbeRecord      `json:"probes"`
	Runs            []TargetRun        `json:"runs,omitempty"`
	Notes           []string           `json:"notes,omitempty"`
}

func SelectAuthorizedTargets(rows []InstalledProvider) TargetSelection {
	var sel TargetSelection
	for _, row := range rows {
		if reason, reject := rejectInstalled(row); reject {
			sel.Rejected = append(sel.Rejected, RejectedProvider{ProviderID: row.ID, ProviderName: row.Name, Reason: reason})
			continue
		}
		host := originHost(row.BaseURL)
		switch row.ID {
		case AuthorizedDeepSeekProviderID:
			installed := installedDefaultModel(row)
			if installed == "" {
				installed = AuthorizedDeepSeekSuiteModel
			}
			sel.ProfileMismatch = append(sel.ProfileMismatch, ProfileMismatch{
				ProfileModelID:   ProfileDeepSeekFlashModel,
				InstalledModelID: installed,
				Note:             "profile testdata lists deepseek-v4-flash; installed DeepSeek default is not relabeled",
			})
			sel.Targets = append(sel.Targets,
				AuthorizedTarget{
					ProviderID: row.ID, ProviderName: row.Name, OriginHost: host,
					Protocol: row.Protocol, BaseURL: row.BaseURL,
					ModelID: AuthorizedDeepSeekSuiteModel, Role: "suite",
					Mismatch: "profile testdata lists deepseek-v4-flash; installed default is " + installed,
				},
				AuthorizedTarget{
					ProviderID: row.ID, ProviderName: row.Name, OriginHost: host,
					Protocol: row.Protocol, BaseURL: row.BaseURL,
					ModelID: AuthorizedDeepSeekProbeModel, Role: "probe",
				},
			)
		case AuthorizedGLMProviderID:
			sel.Targets = append(sel.Targets, AuthorizedTarget{
				ProviderID: row.ID, ProviderName: row.Name, OriginHost: host,
				Protocol: row.Protocol, BaseURL: row.BaseURL,
				ModelID: AuthorizedGLMSuiteModel, Role: "suite",
			})
		}
	}
	return sel
}

func rejectInstalled(row InstalledProvider) (string, bool) {
	name := strings.ToLower(row.Name)
	host := strings.ToLower(originHost(row.BaseURL))
	if strings.Contains(name, "lm studio") || host == "127.0.0.1" || host == "localhost" || strings.Contains(host, "127.0.0.1") {
		return "lm_studio", true
	}
	if row.Protocol == string(provider.ProtocolVolcSpeech) || strings.Contains(name, "speech") {
		return "volc_speech", true
	}
	if row.ID != AuthorizedDeepSeekProviderID && row.ID != AuthorizedGLMProviderID {
		if mediaOnly(row) {
			return "media_kind", true
		}
		return "unauthorized_provider", true
	}
	return "", false
}

func mediaOnly(row InstalledProvider) bool {
	if len(row.Models) == 0 {
		return false
	}
	for _, m := range row.Models {
		switch strings.ToLower(m.Kind) {
		case "ocr", "image", "video", "speech":
		default:
			return false
		}
	}
	return true
}

func installedDefaultModel(row InstalledProvider) string {
	for _, m := range row.Models {
		if m.IsDefault {
			return m.ModelID
		}
	}
	return ""
}

func originHost(raw string) string {
	origin, err := provider.NormalizeOrigin(raw)
	if err != nil {
		u, uerr := url.Parse(strings.TrimSpace(raw))
		if uerr != nil {
			return ""
		}
		return strings.ToLower(u.Hostname())
	}
	u, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func MarshalLiveEvidence(ev LiveEvidence) ([]byte, error) {
	ev = sanitizeEvidence(ev)
	raw, err := json.MarshalIndent(ev, "", "  ")
	if err != nil {
		return nil, err
	}
	return []byte(sanitizeText(string(raw))), nil
}

func sanitizeEvidence(ev LiveEvidence) LiveEvidence {
	ev.LiveQualified = false
	ev.LayersQ = sanitizeLayersQ(ev.LayersQ)
	ev.CheckedAt = sanitizeText(ev.CheckedAt)
	for i := range ev.Notes {
		ev.Notes[i] = sanitizeText(ev.Notes[i])
	}
	for i := range ev.Probes {
		ev.Probes[i].ProviderName = sanitizeText(ev.Probes[i].ProviderName)
		ev.Probes[i].OriginHost = sanitizeText(ev.Probes[i].OriginHost)
		ev.Probes[i].ModelID = sanitizeText(ev.Probes[i].ModelID)
		ev.Probes[i].Status = sanitizeText(ev.Probes[i].Status)
		ev.Probes[i].HTTPClass = sanitizeText(ev.Probes[i].HTTPClass)
		ev.Probes[i].ErrorCode = sanitizeErrorCode(ev.Probes[i].ErrorCode)
	}
	for i := range ev.Runs {
		ev.Runs[i].ProviderName = sanitizeText(ev.Runs[i].ProviderName)
		ev.Runs[i].OriginHost = sanitizeText(ev.Runs[i].OriginHost)
		ev.Runs[i].ModelID = sanitizeText(ev.Runs[i].ModelID)
		ev.Runs[i].Accounting.LiveQualified = false
		ev.Runs[i].Accounting.EvalComplete = false
		ev.Runs[i].Accounting.HoldOutComplete = false
		for j := range ev.Runs[i].Cases {
			ev.Runs[i].Cases[j].ErrorCode = sanitizeErrorCode(ev.Runs[i].Cases[j].ErrorCode)
			ev.Runs[i].Cases[j].Status = sanitizeText(ev.Runs[i].Cases[j].Status)
			ev.Runs[i].Cases[j].EvidenceKind = sanitizeText(ev.Runs[i].Cases[j].EvidenceKind)
			for k := range ev.Runs[i].Cases[j].Reasons {
				ev.Runs[i].Cases[j].Reasons[k] = sanitizeText(ev.Runs[i].Cases[j].Reasons[k])
			}
		}
	}
	return ev
}

func sanitizeLayersQ(q string) string {
	q = strings.TrimSpace(strings.ToLower(q))
	switch q {
	case "probed_failed", "partial", "ran":
		return q
	default:
		return ""
	}
}

func sanitizeErrorCode(code string) string {
	code = sanitizeText(code)
	code = strings.Join(strings.Fields(code), "_")
	if len(code) > 80 {
		code = code[:80]
	}
	return code
}

var (
	secretSK     = regexp.MustCompile(`(?i)sk-[A-Za-z0-9_\-]+`)
	secretBearer = regexp.MustCompile(`(?i)Bearer\s+\S+`)
	secretAPIKey = regexp.MustCompile(`(?i)api[_-]?key`)
	secretCred   = regexp.MustCompile(`(?i)credential_ref`)
	secretCredID = regexp.MustCompile(`cred-[A-Za-z0-9_\-]+`)
)

func SanitizeLog(s string) string {
	return sanitizeText(s)
}

func sanitizeText(s string) string {
	s = secretBearer.ReplaceAllString(s, "[redacted]")
	s = secretSK.ReplaceAllString(s, "[redacted]")
	s = secretAPIKey.ReplaceAllString(s, "[redacted]")
	s = secretCred.ReplaceAllString(s, "[redacted]")
	s = secretCredID.ReplaceAllString(s, "[redacted]")
	return s
}

func LayersQToken(probes []ProbeRecord, runs []TargetRun) string {
	okSuite := 0
	failSuite := 0
	for _, p := range probes {
		if p.Role == "probe" || p.ModelID == AuthorizedDeepSeekProbeModel {
			continue
		}
		if p.Status == "ok" {
			okSuite++
		} else {
			failSuite++
		}
	}
	if okSuite == 0 {
		return "probed_failed"
	}
	complete := 0
	for _, r := range runs {
		if len(r.Cases) >= len(FixedCaseIDs) {
			complete++
		}
	}
	if complete == 0 || complete < okSuite || failSuite > 0 {
		if complete > 0 {
			return "partial"
		}
		return "partial"
	}
	return "ran"
}

func WriteBaselineQ(path, q string) error {
	q = sanitizeLayersQ(q)
	if q == "" {
		return fmt.Errorf("invalid layers.Q")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !json.Valid(raw) {
		return fmt.Errorf("baseline is not JSON")
	}
	re := regexp.MustCompile(`("Q"\s*:\s*")[^"]*(")`)
	if !re.Match(raw) {
		return fmt.Errorf("baseline missing layers.Q")
	}
	out := re.ReplaceAll(raw, []byte(`${1}`+q+`${2}`))
	return os.WriteFile(path, out, 0o644)
}

func HTTPClass(status int) string {
	switch {
	case status == 0:
		return ""
	case status >= 200 && status < 300:
		return "2xx"
	case status >= 400 && status < 500:
		return "4xx"
	case status >= 500:
		return "5xx"
	default:
		return "other"
	}
}

func LiveScenario(c EvalCase) string {
	if c.CaseID == "F03" {
		return "supported-font"
	}
	for _, sc := range c.Scenarios {
		if sc.Mode == "live" {
			return sc.ScenarioID
		}
	}
	if c.CaseID == "R01" {
		return "live-observed-replay"
	}
	return "default"
}

func LiveOutputTokenCap(c EvalCase) (int, bool) {
	cap := closeoutLiveOutputTokens
	if c.Budget.CloseoutOutputTokens > 0 {
		cap = int(c.Budget.CloseoutOutputTokens)
	}
	switch c.CaseID {
	case "D02", "P01", "P02", "X01", "C02", "C04":
		if cap < raisedLiveOutputTokens {
			return raisedLiveOutputTokens, true
		}
	}
	return cap, false
}
