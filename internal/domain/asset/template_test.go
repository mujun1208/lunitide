package asset

import "testing"

func TestValidDocumentTypeAcceptsWorkbenchKeys(t *testing.T) {
	t.Parallel()
	for _, d := range []DocumentType{
		DocumentTypeRequirementTasks,
		"req_task_list",
		"biz_req_analysis",
		"db_design",
		"",
	} {
		if !ValidDocumentType(d) {
			t.Errorf("expected valid: %q", d)
		}
	}
	if ValidDocumentType("not-a-doc") {
		t.Fatal("unknown type must be rejected")
	}
}
