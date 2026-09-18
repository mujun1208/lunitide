package ocrapp

type OCRScope struct {
	OwnerSubjectID string
	ScopeKind      string
	ScopeID        string
}

type PackGate struct {
	PackID                       string
	InstallEnabled               bool
	AutoRouteEnabled             bool
	VerifiedRuntimeProfileDigest *string
	DisabledReason               string
	Revision                     int64
	UpdatedAt                    string
}

type LegacyRegistration struct {
	RegistrationID string
	State          string
	Available      bool
	MarkerDetected bool
}

func (s OCRScope) Valid() bool {
	if s.OwnerSubjectID == "" || len(s.OwnerSubjectID) > 128 {
		return false
	}
	switch s.ScopeKind {
	case "user":
		return s.ScopeID == s.OwnerSubjectID
	case "project":
		return s.ScopeID != "" && len(s.ScopeID) <= 128
	default:
		return false
	}
}
