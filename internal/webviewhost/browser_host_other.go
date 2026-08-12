//go:build !windows

package webviewhost

import "context"

type BrowserHost struct{}

type BrowserHostOptions struct {
	InitialURL         string
	UserDataFolder     string
	MainUserDataFolder string
	Title              string
}

func NewBrowserHost(options BrowserHostOptions) (*BrowserHost, error) {
	if _, err := NormalizeBrowserURL(options.InitialURL); err != nil {
		return nil, err
	}
	if _, err := ValidateIsolatedBrowserProfile(options.UserDataFolder, options.MainUserDataFolder); err != nil {
		return nil, err
	}
	return nil, ErrBrowserRuntimeUnsupported
}

func (*BrowserHost) Run(context.Context) error { return ErrBrowserRuntimeUnsupported }
func (*BrowserHost) Close() error              { return ErrBrowserRuntimeUnsupported }
