package projectgen

import (
	"errors"
	"strings"
	"time"
)

const MinGeneratedBytes = 32

var (
	ErrGenerateEmpty         = errors.New("generated deliverable is empty")
	ErrUnknownDocumentType   = errors.New("unknown deliverable document type")
	ErrGenerateSkipApproved  = errors.New("approved deliverable cannot be overwritten")
)

type Input struct {
	ProjectName   string
	ProjectCode   string
	ProjectType   string
	RootPath      string
	PhaseLabel    string
	DocumentType  string
	Title         string
	Answers       map[string]string
	Prior         string
	Council       string
	TemplateBody  string
	OverwriteOK   bool
	AlreadyApproved bool
}

func Render(in Input) (string, error) {
	if in.AlreadyApproved && !in.OverwriteOK {
		return "", ErrGenerateSkipApproved
	}
	body := strings.TrimSpace(in.TemplateBody)
	if body == "" {
		skel, ok := Skeleton(in.DocumentType)
		if !ok {
			return "", ErrUnknownDocumentType
		}
		body = skel
	}
	out := applyTokens(body, in)
	if len([]byte(out)) < MinGeneratedBytes {
		return "", ErrGenerateEmpty
	}
	return out, nil
}

func applyTokens(body string, in Input) string {
	replacer := strings.NewReplacer(
		"{{projectName}}", nz(in.ProjectName, "未命名项目"),
		"{{projectCode}}", nz(in.ProjectCode, "ITM00000"),
		"{{projectType}}", nz(in.ProjectType, "implementation"),
		"{{rootPath}}", nz(in.RootPath, ""),
		"{{date}}", time.Now().UTC().Format("2006-01-02"),
		"{{phaseLabel}}", nz(in.PhaseLabel, ""),
		"{{title}}", nz(in.Title, in.DocumentType),
		"{{answer.core_problem}}", in.Answers["core_problem"],
		"{{answer.system_shape}}", in.Answers["system_shape"],
		"{{answer.stack}}", in.Answers["stack"],
		"{{answer.data_store}}", in.Answers["data_store"],
		"{{answer.tree_choice}}", in.Answers["tree_choice"],
		"{{answer.rule_strictness}}", in.Answers["rule_strictness"],
		"{{answer.main_flows}}", in.Answers["main_flows"],
		"{{answer.api_style}}", in.Answers["api_style"],
		"{{answer.module_cut}}", in.Answers["module_cut"],
		"{{answer.ui_depth}}", in.Answers["ui_depth"],
		"{{answer.integration_cut}}", in.Answers["integration_cut"],
		"{{prior}}", in.Prior,
		"{{council}}", in.Council,
	)
	return replacer.Replace(body)
}

func nz(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func IsChecklistType(documentType string) bool {
	switch documentType {
	case "req_task_list", "biz_flow_list", "api_list", "feature_dev_list", "dev_checklist", "test_checklist", "integration_test_list", "interface_list":
		return true
	default:
		return false
	}
}
