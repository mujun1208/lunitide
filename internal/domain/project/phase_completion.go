package project

// RequiredPhaseDocuments is the server-side completion gate corresponding to
// the workbench's document-bearing phases. Release has a separate evidence
// workflow and cannot be completed through the document gate.
func RequiredPhaseDocuments(t Type, phase int) []string {
	if phase == 1 {
		return []string{"biz_req_analysis", "impl_assessment", "req_task_list", "arch_design", "hw_config", "biz_standard", "dev_standard", "tech_standard", "project_structure"}
	}
	if t == TypeOperations {
		switch phase {
		case 2:
			return []string{"db_design"}
		case 3:
			return []string{"interface_list"}
		case 4:
			return []string{"dev_checklist"}
		case 5:
			return []string{"test_checklist"}
		}
		return nil
	}
	switch phase {
	case 2:
		return []string{"biz_flow_diagram", "biz_flow_list", "biz_blueprint", "api_list", "feature_dev_list", "feature_detail", "api_detail", "db_detail", "ui_detail", "integration_test_list"}
	case 3:
		return []string{"db_design"}
	case 4:
		return []string{"interface_list"}
	case 5:
		return []string{"dev_checklist"}
	case 6:
		return []string{"test_checklist"}
	case 7:
		return []string{"integration_test_list"}
	}
	return nil
}

func PhaseCompletionTarget(p Project, phase int) (Status, bool) {
	if phase == ReleasePhase(p.Type) {
		// Local publication prepares the release; external deployment is a
		// separate configured capability and cannot be inferred as live.
		if p.Type == TypeOperations {
			return StatusInProgress, NormalizeStatus(p.Status) == StatusInProgress
		}
		return StatusGoLivePrep, NormalizeStatus(p.Status) == StatusGoLivePrep
	}
	if len(RequiredPhaseDocuments(p.Type, phase)) == 0 {
		return "", false
	}
	current := NormalizeStatus(p.Status)
	if phase == 1 {
		if current != StatusChartered {
			return "", false
		}
		return AdvanceTarget(p.Type, phase)
	}
	if p.Type == TypeOperations {
		return StatusInProgress, current == StatusInProgress
	}
	if phase == 2 {
		want := StatusReqArchitecture
		if p.Type == TypeEnhancement {
			want = StatusReqAssessment
		}
		return StatusInProgress, current == want
	}
	if phase >= 3 && phase <= 5 {
		return StatusInProgress, current == StatusInProgress
	}
	if phase == 6 {
		return StatusIntegrationTest, current == StatusInProgress
	}
	if phase == 7 {
		return StatusGoLivePrep, current == StatusIntegrationTest
	}
	return "", false
}

func ReleasePhase(t Type) int {
	if t == TypeOperations {
		return 6
	}
	return 8
}
