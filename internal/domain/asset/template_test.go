package asset

import "testing"

func TestValidateTemplateFile(t *testing.T) {
	t.Parallel()
	if err := ValidateTemplateFile(TemplateTypeDocument, "report.docx"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTemplateFile(TemplateTypeDocument, "report.dot"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTemplateFile(TemplateTypeScaffold, "starter.zip"); err != nil {
		t.Fatal(err)
	}
	if ValidateTemplateFile(TemplateTypeScaffold, "readme.md") == nil {
		t.Fatal("scaffold must reject non-archive")
	}
	if err := ValidateTemplateFile(TemplateTypePPT, "deck.pptx"); err != nil {
		t.Fatal(err)
	}
	if ValidateTemplateFile(TemplateTypePPT, "deck.ppt") == nil {
		t.Fatal("legacy ppt")
	}
	if ValidateTemplateFile(TemplateTypePPT, "deck.docx") == nil {
		t.Fatal("ppt template must reject a word file")
	}
	if err := ValidateTemplateFile(TemplateTypeWord, "brief.docx"); err != nil {
		t.Fatal(err)
	}
	if ValidateTemplateFile(TemplateTypeWord, "brief.xlsx") == nil {
		t.Fatal("word template must reject a spreadsheet")
	}
	if err := ValidateTemplateFile(TemplateTypeExcel, "ledger.xlsx"); err != nil {
		t.Fatal(err)
	}
	if ValidateTemplateFile(TemplateTypeExcel, "ledger.pptx") == nil {
		t.Fatal("excel template must reject a deck")
	}
	if !ValidTemplateType(TemplateTypePPT) || !ValidTemplateType(TemplateTypeWord) || !ValidTemplateType(TemplateTypeExcel) {
		t.Fatal("office template types must be valid")
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
