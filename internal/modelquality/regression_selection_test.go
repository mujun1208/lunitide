package modelquality

import "testing"

func TestRegressionSelectionRejectsZeroMatches(t *testing.T) {
	if _, err := SelectRegressionTests([]string{"TestCompileParametersMatrix"}, ""); err != ErrEmptyRegressionSelection {
		t.Fatalf("empty pattern: %v", err)
	}
	if _, err := SelectRegressionTests([]string{"TestCompileParametersMatrix"}, "TestDoesNotExist"); err != ErrZeroRegressionMatches {
		t.Fatalf("zero matches: %v", err)
	}
	got, err := SelectRegressionTests([]string{"TestCompileParametersMatrix", "TestOther"}, "TestCompile")
	if err != nil || len(got) != 1 || got[0] != "TestCompileParametersMatrix" {
		t.Fatalf("got %v %v", got, err)
	}
}
