//go:build !windows

package toolruntime

import (
	"context"
	"errors"
)

func ReadLocation(context.Context) (LocationFix, error) {
	return LocationFix{}, errors.New("这台系统没有本机定位")
}
