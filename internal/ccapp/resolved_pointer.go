package ccapp

import "fmt"

// Accessibility Invoke means one ordinary activation; it cannot implement
// a context menu, double click or Ctrl/Shift selection. A resolved node ID
// must also keep its coordinates instead of invoking the first equal name.
func (s *Service) clickResolvedPointer(sx, sy int, hit, button string, clicks int, modifiers []string) error {
	if _, ok := s.host.(clickHitter); !ok {
		return fmt.Errorf("%w: target hit-test unavailable; cannot perform %s click", ErrCcExecFailed, button)
	}
	ox, oy := s.host.ScreenOrigin()
	w, h := s.host.ScreenSize()
	if w <= 0 || h <= 0 || sx < ox || sy < oy || sx >= ox+w || sy >= oy+h {
		return fmt.Errorf("%w: resolved target outside current desktop; observe again", ErrCcInputFiltered)
	}
	if err := s.refuseSelfWindowPixels(); err != nil {
		return err
	}
	if err := s.verifyClickHit(hit, sx, sy); err != nil {
		return err
	}
	if err := s.controlHost().MouseMove(sx, sy); err != nil {
		return err
	}
	return s.withModifiers(modifiers, func() error {
		// Recheck immediately before dispatch; the action may itself replace
		// this element, so the following verifyAfter screenshot is the evidence.
		if err := s.verifyClickHit(hit, sx, sy); err != nil {
			return err
		}
		return s.controlHost().MouseClick(button, clicks)
	})
}
