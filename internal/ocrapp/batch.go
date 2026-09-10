package ocrapp

type BatchPage struct {
	File     string
	Page     int
	HasText  bool
	Status   string
	Coverage string
}

func IncompletePages(pages []BatchPage) []BatchPage {
	var out []BatchPage
	for _, p := range pages {
		if p.Status == "failed" || !p.HasText {
			out = append(out, p)
		}
	}
	return out
}

func ConfidenceOrUnknown(raw string) string {
	if raw == "" {
		return "unknown"
	}
	return raw
}
