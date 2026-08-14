//go:build !windows && !darwin && !linux

package commandworker

import "context"

func PinWorkingDirectory(_, _ string) (StartGuard, error) { return noopStartGuard{}, nil }

func run(context.Context, Spec, StartGuard, *cappedSink) (Outcome, error) {
	return Outcome{}, ErrUnsupported
}
