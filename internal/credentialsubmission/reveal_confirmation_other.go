//go:build !windows

package credentialsubmission

import (
	"context"
	"errors"
)

func confirmCredentialRevealNative(context.Context, RevealTarget) (bool, error) {
	return false, errors.New("native credential reveal confirmation unavailable")
}
