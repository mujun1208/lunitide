package asset

import "testing"

func TestValidateTemplateFile(t *testing.T) {
	t.Parallel()
	if err := ValidateTemplateFile(TemplateTypeDocument, "report.docx"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTemplateFile(TemplateTypeScaffold, "starter.zip"); err != nil {
		t.Fatal(err)
	}
	if ValidateTemplateFile(TemplateTypeScaffold, "readme.md") == nil {
		t.Fatal("scaffold must reject non-archive")
	}
}

func TestCanRestoreOnlyFromVoid(t *testing.T) {
	t.Parallel()
	if !CanRestore(StatusVoid) {
		t.Fatal("void must restore")
	}
	if CanRestore(StatusDraft) || CanRestore(StatusEnabled) {
		t.Fatal("only void restores")
	}
}

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
