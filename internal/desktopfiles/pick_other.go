//go:build !windows

package desktopfiles

func pickOSForms(folder, multiple bool) ([]Item, []string, error) {
	_, _ = folder, multiple
	return nil, nil, ErrUnavailable
}

func pickOSNative(folder, multiple bool) ([]Item, []string, error) {
	_, _ = folder, multiple
	return nil, nil, ErrUnavailable
}

func pickMediaForms(multiple bool) ([]Item, []string, error) {
	_ = multiple
	return nil, nil, ErrUnavailable
}

func pickMediaNative(multiple bool) ([]Item, []string, error) {
	_ = multiple
	return nil, nil, ErrUnavailable
}
