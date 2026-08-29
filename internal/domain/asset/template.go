package asset

import (
	"errors"
	"mime"
	"path/filepath"
	"strings"
	"time"
)

type Status string

const (
	StatusDraft    Status = "draft"
	StatusEnabled  Status = "enabled"
	StatusDisabled Status = "disabled"
	StatusVoid     Status = "void"
)

type TemplateType string

const (
	TemplateTypeDocument  TemplateType = "document"
	TemplateTypeScaffold  TemplateType = "scaffold"
)

// DocumentType is one of the 25 controlled deliverable labels from the M7 PRD.
type DocumentType string

const (
	DocumentTypeBRD              DocumentType = "业务需求分析报告"
	DocumentTypeImplementation   DocumentType = "系统实现评估报告"
	DocumentTypeRequirementTasks DocumentType = "需求任务清单"
	DocumentTypeArchitecture     DocumentType = "系统架构设计文档"
	DocumentTypeHardware         DocumentType = "系统硬件配置文档"
	DocumentTypeBusinessSpec     DocumentType = "系统业务规范"
	DocumentTypeDevSpec          DocumentType = "系统开发规范"
	DocumentTypeTechSpec         DocumentType = "系统技术规范"
	DocumentTypeProjectStructure DocumentType = "项目结构规范"
	DocumentTypeBusinessFlow     DocumentType = "业务流程图"
	DocumentTypeBusinessFlowList DocumentType = "业务流程清单"
	DocumentTypeBusinessBlueprint DocumentType = "业务蓝图文档"
	DocumentTypeInterfaceList    DocumentType = "接口清单"
	DocumentTypeFeatureDevList   DocumentType = "功能开发清单"
	DocumentTypeFeatureDesign    DocumentType = "功能详细设计文档"
	DocumentTypeInterfaceDesign  DocumentType = "接口详细设计文档"
	DocumentTypeDatabaseDesign   DocumentType = "数据库详细设计文档"
	DocumentTypeUIDesign         DocumentType = "UI界面详细设计"
	DocumentTypeUnitTest         DocumentType = "单元测试报告"
	DocumentTypeOpsChecklist     DocumentType = "系统运维清单"
	DocumentTypeIntegrationCases DocumentType = "集成测试场景清单"
	DocumentTypeIntegrationTest  DocumentType = "集成测试报告"
	DocumentTypeGoLiveStrategy   DocumentType = "上线策略和风险评估报告"
	DocumentTypeEmergencyPlan    DocumentType = "应急预案报告"
	DocumentTypeGoLiveIssues     DocumentType = "上线问题清单"
)

// AllDocumentTypes is the canonical 25-item controlled list.
var AllDocumentTypes = []DocumentType{
	DocumentTypeBRD,
	DocumentTypeImplementation,
	DocumentTypeRequirementTasks,
	DocumentTypeArchitecture,
	DocumentTypeHardware,
	DocumentTypeBusinessSpec,
	DocumentTypeDevSpec,
	DocumentTypeTechSpec,
	DocumentTypeProjectStructure,
	DocumentTypeBusinessFlow,
	DocumentTypeBusinessFlowList,
	DocumentTypeBusinessBlueprint,
	DocumentTypeInterfaceList,
	DocumentTypeFeatureDevList,
	DocumentTypeFeatureDesign,
	DocumentTypeInterfaceDesign,
	DocumentTypeDatabaseDesign,
	DocumentTypeUIDesign,
	DocumentTypeUnitTest,
	DocumentTypeOpsChecklist,
	DocumentTypeIntegrationCases,
	DocumentTypeIntegrationTest,
	DocumentTypeGoLiveStrategy,
	DocumentTypeEmergencyPlan,
	DocumentTypeGoLiveIssues,
}

var (
	ErrNotFound           = errors.New("asset template not found")
	ErrInvalidTransition  = errors.New("asset template transition is invalid")
	ErrTemplateReferenced = errors.New("asset template is referenced")
	ErrVersionConflict    = errors.New("asset template version conflict")
)

type AssetTemplate struct {
	ID           string       `json:"id"`
	TemplateCode string       `json:"templateCode"`
	Name         string       `json:"name"`
	TemplateType TemplateType `json:"templateType"`
	DocumentType DocumentType `json:"documentType"`
	Description  string       `json:"description"`
	Client       string       `json:"client"`
	MimeType     string       `json:"mimeType"`
	FileName     string       `json:"fileName"`
	FilePath     string       `json:"filePath"`
	Status       Status       `json:"status"`
	CreatedAt    time.Time    `json:"createdAt"`
	UpdatedAt    time.Time    `json:"updatedAt"`
	Version      int64        `json:"version"`
}

type Filter struct {
	Status       Status
	TemplateType TemplateType
	DocumentType DocumentType
}

func ValidStatus(s Status) bool {
	switch s {
	case StatusDraft, StatusEnabled, StatusDisabled, StatusVoid:
		return true
	default:
		return false
	}
}

func ValidTemplateType(t TemplateType) bool {
	return t == TemplateTypeDocument || t == TemplateTypeScaffold
}

// documentTypeKeys are the English keys the workbench UI upserts.
// Stored as-is so the panel can look documents up by def.key.
var documentTypeKeys = map[string]struct{}{
	"biz_req_analysis": {}, "impl_assessment": {}, "req_task_list": {}, "arch_design": {},
	"hw_config": {}, "biz_standard": {}, "dev_standard": {}, "tech_standard": {},
	"project_structure": {}, "biz_flow_diagram": {}, "biz_flow_list": {}, "biz_blueprint": {},
	"api_list": {}, "feature_dev_list": {}, "feature_detail": {}, "api_detail": {},
	"db_detail": {}, "ui_detail": {}, "integration_test_list": {}, "interface_list": {},
	"db_design": {}, "dev_checklist": {}, "test_checklist": {},
}

func ValidDocumentType(d DocumentType) bool {
	if d == "" {
		return true
	}
	for _, item := range AllDocumentTypes {
		if item == d {
			return true
		}
	}
	_, ok := documentTypeKeys[string(d)]
	return ok
}

func NormalizeName(raw string) (string, error) {
	name := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if name == "" || len([]rune(name)) > 200 {
		return "", errors.New("template name must contain 1 to 200 characters")
	}
	return name, nil
}

func CanEnable(cur Status) bool {
	return cur == StatusDraft || cur == StatusDisabled
}

func CanVoid(cur Status) bool {
	return cur == StatusEnabled || cur == StatusDisabled
}

func CanDelete(cur Status) bool {
	return cur == StatusDraft
}

func CanRestore(cur Status) bool {
	return cur == StatusVoid
}

var documentExtensions = map[string]struct{}{
	".md": {}, ".txt": {}, ".pdf": {}, ".doc": {}, ".docx": {}, ".dot": {},
	".xls": {}, ".xlsx": {}, ".ppt": {}, ".pptx": {},
	".html": {}, ".htm": {}, ".json": {}, ".yaml": {}, ".yml": {}, ".csv": {},
}

// ValidateTemplateFile enforces allowed extensions per template type.
func ValidateTemplateFile(t TemplateType, fileName string) error {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	if lower == "" {
		return errors.New("template file name is required")
	}
	switch t {
	case TemplateTypeScaffold:
		if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".zip") {
			return nil
		}
		return errors.New("scaffold template must be .zip or .tar.gz")
	default:
		if _, ok := documentExtensions[filepath.Ext(lower)]; ok {
			return nil
		}
		return errors.New("document template file type not allowed")
	}
}

// DetectMimeType infers MIME from the file name extension.
func DetectMimeType(fileName string) string {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	if strings.HasSuffix(lower, ".tar.gz") {
		return "application/gzip"
	}
	ext := filepath.Ext(lower)
	if ext == "" {
		return "application/octet-stream"
	}
	if detected := mime.TypeByExtension(ext); detected != "" {
		return detected
	}
	switch ext {
	case ".md":
		return "text/markdown"
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".ppt":
		return "application/vnd.ms-powerpoint"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	default:
		return "application/octet-stream"
	}
}

// StatusLabel returns the product-facing status label.
func StatusLabel(s Status) string {
	switch s {
	case StatusDraft:
		return "创建"
	case StatusEnabled:
		return "可用"
	case StatusVoid:
		return "作废"
	case StatusDisabled:
		return "停用"
	default:
		return string(s)
	}
}
