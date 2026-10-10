package ccapp

import (
	"errors"
	"testing"
)

// focusSequenceHost answers a different UIA role per probe so tests can
// reproduce a window whose caret lands in the composer only after the
// activation transition.
type focusSequenceHost struct {
	executionTestHost
	roles []string
	calls int
}

func (h *focusSequenceHost) FocusedRole() (string, bool) {
	i := h.calls
	h.calls++
	if len(h.roles) == 0 {
		return "", false
	}
	if i >= len(h.roles) {
		i = len(h.roles) - 1
	}
	return h.roles[i], true
}

// A click→type pair on a freshly activated chat window (豆包/元宝 are
// Electron: the caret reaches the composer a beat after the click) must not
// be refused on the first probe — the re-probe sees the caret and types.
func TestRefuseTypingWithoutFocusRetrySurvivesActivationTransition(t *testing.T) {
	s, _, _ := executionTestService(t)
	h := &focusSequenceHost{roles: []string{"button", "edit"}}
	s.SetHost(h)
	s.noteTypingFocus(probeFocus(s.host)) // caller's first probe saw a button
	if err := s.refuseTypingWithoutFocusRetry(); err != nil {
		t.Fatalf("re-probe should accept the composer caret: %v", err)
	}
	if h.calls != 2 {
		t.Fatalf("expected caller probe + one re-probe, got %d", h.calls)
	}
}

// Focus that stays off text fields after the re-probe still refuses.
func TestRefuseTypingWithoutFocusRetryStillRefuses(t *testing.T) {
	s, _, _ := executionTestService(t)
	h := &focusSequenceHost{roles: []string{"button", "list"}}
	s.SetHost(h)
	s.noteTypingFocus(probeFocus(s.host))
	if err := s.refuseTypingWithoutFocusRetry(); !errors.Is(err, ErrCcInputFiltered) {
		t.Fatalf("expected input-filtered refusal, got %v", err)
	}
	if h.calls != 2 {
		t.Fatalf("expected caller probe + one re-probe, got %d", h.calls)
	}
}

// An armed composer bypasses the refusal without any probe.
func TestRefuseTypingWithoutFocusRetryHonorsArmedComposer(t *testing.T) {
	s, _, _ := executionTestService(t)
	h := &focusSequenceHost{}
	s.SetHost(h)
	s.noteTypingFocus("button", true)
	s.setComposerArmed(true)
	if err := s.refuseTypingWithoutFocusRetry(); err != nil {
		t.Fatalf("armed composer must type: %v", err)
	}
	if h.calls != 0 {
		t.Fatalf("armed composer must not probe, got %d", h.calls)
	}
}
