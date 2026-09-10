package modelfit

const (
	QualifyUntested     = "untested"
	QualifyFixturePass  = "fixture_pass"
	QualifyBlocked      = "blocked"
)

type Qualification struct {
	Family       Family
	ModelID      string
	CodecVersion string
	Status       string
	Evidence     string
	Adopted      bool
}

func DefaultQualification(family, modelID, codecVersion string) Qualification {
	return Qualification{
		Family:       Family(family),
		ModelID:      modelID,
		CodecVersion: codecVersion,
		Status:       QualifyUntested,
	}
}
