//go:build !windows

// M5 T-5.3.3 platform stub: the job tree guarantee relies on Windows Job
// Objects; off Windows there is no portable equivalent and StartJob
// answers ErrUnsupported.

package command

// JobOptions mirrors the windows JobOptions so call sites compile on
// every platform.
type JobOptions struct {
	TimeoutMs            int64
	BackgroundAfterMs    int64
	BackgroundAfterBytes int64
	Clock                Clock
}

// Job is a platform stub; callers receive a nil Job with ErrUnsupported.
type Job struct{}

// StartJob is unsupported off Windows.
func StartJob(argv []string, cwd string, env []string, opts JobOptions) (*Job, error) {
	return nil, ErrUnsupported
}
