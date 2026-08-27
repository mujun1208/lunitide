package scheduler

import (
	"fmt"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/cronexpr"
)

const atPrefix = "at:"

// ParseSchedule accepts a 5-field cron or a one-shot `at:<RFC3339>` stamp.
func ParseSchedule(cron string) error {
	_, err := nextFireTime(cron, time.Now().UTC())
	return err
}

// IsAtSchedule reports a one-shot --at expression.
func IsAtSchedule(cron string) bool {
	return strings.HasPrefix(strings.TrimSpace(cron), atPrefix)
}

func parseAtTime(cron string) (time.Time, bool) {
	raw := strings.TrimSpace(cron)
	if !strings.HasPrefix(raw, atPrefix) {
		return time.Time{}, false
	}
	stamp := strings.TrimSpace(strings.TrimPrefix(raw, atPrefix))
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// nextFireTime answers the next instant for cron or the at-stamp itself.
func nextFireTime(cron string, now time.Time) (time.Time, error) {
	if at, ok := parseAtTime(cron); ok {
		return at, nil
	}
	expr, err := cronexpr.Parse(cron)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: cron", ErrInvalid)
	}
	return expr.Next(now), nil
}

func normalizeSessionMode(mode string) (string, error) {
	switch strings.TrimSpace(mode) {
	case "", "bound":
		return "bound", nil
	case "isolated":
		return "isolated", nil
	default:
		return "", fmt.Errorf("%w: sessionMode", ErrInvalid)
	}
}
