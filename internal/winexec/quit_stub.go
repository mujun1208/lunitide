//go:build !windows

package winexec

import (
	"context"
	"errors"
)

func QuitProcessImages(context.Context, []string) (int, error) {
	return 0, errors.New("full application exit is only available on Windows")
}
