package project

// StatusChartered is 立项 — published projects enter the workbench from here.
const (
	StatusChartered         Status = "chartered"
	StatusReqArchitecture   Status = "req_architecture"
	StatusReqAssessment     Status = "req_assessment"
	StatusInProgress        Status = "in_progress"
	StatusIntegrationTest   Status = "integration_test"
	StatusGoLivePrep        Status = "go_live_prep"
	StatusLive              Status = "live"
)

// StatusActive is a legacy alias mapped to chartered on read paths.
const legacyActive = StatusActive

func NormalizeStatus(s Status) Status {
	if s == StatusActive {
		return StatusChartered
	}
	return s
}

func validStatus(s Status) bool {
	switch NormalizeStatus(s) {
	case StatusCreated, StatusChartered, StatusReqArchitecture, StatusReqAssessment,
		StatusInProgress, StatusIntegrationTest, StatusGoLivePrep, StatusLive,
		StatusClosed, StatusArchived:
		return true
	default:
		return false
	}
}

func statusesForType(t Type) []Status {
	switch t {
	case TypeOperations:
		return []Status{StatusCreated, StatusChartered, StatusInProgress, StatusClosed}
	case TypeEnhancement:
		return []Status{StatusCreated, StatusChartered, StatusReqAssessment, StatusInProgress, StatusClosed}
	default:
		return []Status{
			StatusCreated, StatusChartered, StatusReqArchitecture, StatusInProgress,
			StatusIntegrationTest, StatusGoLivePrep, StatusLive, StatusClosed,
		}
	}
}

func statusAllowedForType(t Type, s Status) bool {
	s = NormalizeStatus(s)
	for _, allowed := range statusesForType(t) {
		if allowed == s {
			return true
		}
	}
	return false
}

// CanEnterWorkspace allows workbench access after publication; closed projects are read-only viewers.
func (p Project) CanEnterWorkspace() bool {
	s := NormalizeStatus(p.Status)
	return s != StatusCreated && s != StatusArchived
}

// CanEditMutableFields reports whether D–L business fields may change.
func (p Project) CanEditMutableFields() bool {
	s := NormalizeStatus(p.Status)
	return s != StatusClosed && s != StatusArchived
}

// CanEditIdentity reports whether immutable identity fields (B/C) may change — only in created state.
func (p Project) CanEditIdentity() bool {
	return NormalizeStatus(p.Status) == StatusCreated
}

// CanPublish transitions created → chartered.
func (p Project) CanPublish() bool {
	return NormalizeStatus(p.Status) == StatusCreated
}

// CanDeleteOnlyCreated requires created state; artifact checks happen in the app layer.
func (p Project) CanDeleteOnlyCreated() bool {
	return NormalizeStatus(p.Status) == StatusCreated
}

// CloseableStatuses are project statuses where manual close is permitted.
func CloseableStatuses(t Type) []Status {
	switch t {
	case TypeOperations, TypeEnhancement:
		return []Status{StatusChartered, StatusReqAssessment, StatusInProgress}
	default:
		return []Status{
			StatusChartered, StatusReqArchitecture, StatusInProgress, StatusIntegrationTest,
			StatusGoLivePrep, StatusLive,
		}
	}
}

func (p Project) CanClose() bool {
	s := NormalizeStatus(p.Status)
	if s == StatusClosed || s == StatusCreated || s == StatusArchived {
		return false
	}
	for _, allowed := range CloseableStatuses(p.Type) {
		if allowed == s {
			return true
		}
	}
	return false
}

// AdvanceTarget maps a completed workbench phase gate to the next project status.
func AdvanceTarget(t Type, phase int) (Status, bool) {
	switch t {
	case TypeImplementation:
		switch phase {
		case 1:
			return StatusReqArchitecture, true
		case 2:
			return StatusInProgress, true
		}
	case TypeEnhancement:
		switch phase {
		case 1:
			return StatusReqAssessment, true
		case 2:
			return StatusInProgress, true
		}
	case TypeOperations:
		switch phase {
		case 1:
			return StatusInProgress, true
		}
	}
	return "", false
}

func (p Project) StatusLabel() string {
	switch NormalizeStatus(p.Status) {
	case StatusCreated:
		return "创建"
	case StatusChartered:
		return "立项"
	case StatusReqArchitecture:
		return "需求架构"
	case StatusReqAssessment:
		return "需求评估"
	case StatusInProgress:
		return "实施中"
	case StatusIntegrationTest:
		return "集成测试"
	case StatusGoLivePrep:
		return "上线准备"
	case StatusLive:
		return "系统上线"
	case StatusClosed:
		return "项目关闭"
	case StatusArchived:
		return "已删除"
	default:
		return string(p.Status)
	}
}

func (p Project) Validate() error {
	p.Status = NormalizeStatus(p.Status)
	if !validStatus(p.Status) {
		return errInvalid("project status is invalid")
	}
	if !statusAllowedForType(p.Type, p.Status) && p.Status != StatusArchived {
		return errInvalid("project status is invalid for project type")
	}
	return p.validateBase()
}

func errInvalid(msg string) error {
	return &validationError{msg: msg}
}

type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }
