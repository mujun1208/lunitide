//go:build !windows

package winexec

import (
	"context"
	"errors"
)

func MediaSessionAction(_ context.Context, _ []string, action string, _ bool) (MediaSessionResult, error) {
	if err := ValidateMediaSessionAction(action); err != nil {
		return MediaSessionResult{}, err
	}
	return MediaSessionResult{}, errors.New("media sessions require Windows")
}
