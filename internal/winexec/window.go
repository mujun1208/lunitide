package winexec

// WindowHint is a visible top-level window used to confirm desktop.open
// actually brought a target app up — not Lunitide itself.
type WindowHint struct {
	Title   string
	Process string
}
