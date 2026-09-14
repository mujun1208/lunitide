package modelfit

type FitPhase string

const (
	FitInProgress FitPhase = "in_progress"
	FitFailed     FitPhase = "failed"
	FitExpired    FitPhase = "expired"
	FitActivated  FitPhase = "activated"
)

type FitView struct {
	Phase         string
	Label         string
	Source        string
	LiveQualified bool
}

func PresentModelFit(phase FitPhase, source, status string) FitView {
	label := map[FitPhase]string{
		FitInProgress: "探测进行中",
		FitFailed:     "探测失败",
		FitExpired:    "探测已过期",
		FitActivated:  "已激活",
	}[phase]
	if label == "" {
		label = string(phase)
	}
	return FitView{
		Phase:         string(phase),
		Source:        source,
		Label:         label,
		LiveQualified: false,
	}
}
