package desktopfiles

import "errors"

var pickForms = pickOSForms
var pickNative = pickOSNative

func pickOS(folder, multiple bool) ([]Item, []string, error) {
	items, skipped, err := pickForms(folder, multiple)
	if err == nil || errors.Is(err, ErrCanceled) {
		return items, skipped, err
	}
	return pickNative(folder, multiple)
}
