package officestudio

type DeliveryPolicy struct {
	Revision            string   `json:"revision,omitempty"`
	TargetRenderer      string   `json:"targetRenderer,omitempty"`
	Tier                string   `json:"tier,omitempty"` // basic / assured
	RequiredCheckIDs    []string `json:"requiredCheckIDs,omitempty"`
	RequireEditable     bool     `json:"requireEditable,omitempty"`
	RequireSearchable   bool     `json:"requireSearchable,omitempty"`
	RequireVisualReview bool     `json:"requireVisualReview,omitempty"`
}

func FontActualRequired(tier string) bool { return tier == "assured" }

func CanonicalCheckID(id string) string {
	switch id {
	case "font_availability":
		return "font-availability"
	case "font_actual_substitution":
		return "font-actual"
	case "native_render":
		return "actual-render"
	default:
		return id
	}
}

func ResolveRegisteredPolicy(policy DeliveryPolicy) (revision, tier string) {
	switch policy.Revision {
	case "office-basic-v2":
		return "office-basic-v2", "basic"
	case "office-assured-v2":
		return "office-assured-v2", "assured"
	}
	if policy.Tier == "assured" {
		return "office-assured-v2", "assured"
	}
	return "office-basic-v2", "basic"
}

func RegisteredRequiredCheckIDs(revision, kind string) []string {
	required := []string{"file-integrity", "source-content", "locked-facts"}
	switch kind {
	case "docx", "pptx", "xlsx":
		required = append(required, "font-availability", "actual-render")
	}
	if revision == "office-assured-v2" {
		required = append(required, "font-actual", "page-coverage")
	}
	return required
}

// FormalDecision is the unique service-layer delivery verdict for a version,
// source SHA and registered policy revision.
type FormalDecision struct {
	DecisionID     string   `json:"decisionId,omitempty"`
	VersionID      string   `json:"versionId,omitempty"`
	SourceSHA256   string   `json:"sourceSha256,omitempty"`
	PolicyRevision string   `json:"policyRevision,omitempty"`
	Tier           string   `json:"tier,omitempty"`
	Allowed        bool     `json:"allowed,omitempty"`
	State          string   `json:"state,omitempty"` // draft / checking / needs_review / verified / blocked
	BlockingCodes  []string `json:"blockingCodes,omitempty"`
	MissingChecks  []string `json:"missingChecks,omitempty"`
	EvidenceRefs   []string `json:"evidenceRefs,omitempty"`
}
