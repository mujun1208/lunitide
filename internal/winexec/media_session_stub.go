//go:build !windows

package winexec

import (
	"context"
	"errors"
)

func MediaSessionAction(context.Context, []string, string, bool) (MediaSessionResult, error) {
	return MediaSessionResult{}, errors.New("media sessions require Windows")
}
