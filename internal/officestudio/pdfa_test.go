package officestudio

import (
	"errors"
	"strings"
	"testing"
)

func TestPDFACheckUnsupportedWithoutValidator(t *testing.T) {
	t.Setenv("LUNITIDE_PDFA_VALIDATOR", "")
	c := IndependentPDFACheck([]byte("%PDF"))
	if c.Status != "unsupported" || strings.Contains(c.Message, "已符合") {
		t.Fatalf("%#v", c)
	}
}

func TestPDFACheckMissingWithoutPDF(t *testing.T) {
	t.Setenv("LUNITIDE_PDFA_VALIDATOR", "verapdf")
	c := checkPDFA(nil, "verapdf", func(string, []byte) error { return nil })
	if c.Status != "missing" {
		t.Fatalf("%#v", c)
	}
}

func TestPDFACheckPassedOnlyAfterValidator(t *testing.T) {
	c := checkPDFA([]byte("%PDF"), "verapdf", func(string, []byte) error { return nil })
	if c.Status != "passed" {
		t.Fatalf("%#v", c)
	}
	if strings.Contains(c.Message, "已符合 PDF/UA") {
		t.Fatalf("overclaimed: %q", c.Message)
	}
}

func TestPDFACheckFailedWhenValidatorRejects(t *testing.T) {
	c := checkPDFA([]byte("%PDF"), "verapdf", func(string, []byte) error { return errors.New("not pdfa") })
	if c.Status != "failed" {
		t.Fatalf("%#v", c)
	}
}
