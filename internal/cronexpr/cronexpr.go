// Package cronexpr implements the restricted standard 5-field cron
// expression (minute hour day-of-month month day-of-week) with the subset
// Lunitide automation needs: "*", "n", "a-b", "*/s", "a-b/s", and comma
// lists of those. Semantics follow classic cron, including the day-of-month
// OR day-of-week rule; Next answers the first fire time strictly after the
// given instant with a bounded 5-year scan.
package cronexpr

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// fieldBounds is the inclusive min/max of one cron field plus the alias
// entries accepted for it (e.g. 7 => Sunday on the weekday field).
type fieldBounds struct {
	min, max uint8
	aliases  map[string]uint8
}

var bounds = [5]fieldBounds{
	{0, 59, nil}, // minute
	{0, 23, nil}, // hour
	{1, 31, nil}, // day of month
	{1, 12, map[string]uint8{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}}, // month
	{0, 6, map[string]uint8{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}},                                                    // day of week
}

// maxScanYears bounds the forward scan so malformed expressions cannot loop
// forever (e.g. "0 0 31 2 *" never fires).
const maxScanYears = 5

// Expression is a compiled cron schedule.
type Expression struct {
	fields   [5]uint64 // bitmask of accepted values, bit v = value v
	restricted [2]bool  // day-of-month / day-of-week restricted (non-*)
}

// Parse compiles a 5-field cron expression.
func Parse(expr string) (*Expression, error) {
	parts := strings.Fields(strings.TrimSpace(expr))
	if len(parts) != 5 {
		return nil, fmt.Errorf("cronexpr: want 5 fields, got %d", len(parts))
	}
	e := &Expression{}
	for i, part := range parts {
		mask, all, err := parseField(part, bounds[i])
		if err != nil {
			return nil, fmt.Errorf("cronexpr: field %d %q: %w", i+1, part, err)
		}
		e.fields[i] = mask
		if i == 2 || i == 4 {
			e.restricted[i/2-1] = !all
		}
	}
	return e, nil
}

// parseField answers the value bitmask plus whether the field is the
// unrestricted "*" form (needed for the day OR rule).
func parseField(part string, b fieldBounds) (mask uint64, all bool, err error) {
	if part == "*" {
		return fullMask(b.min, b.max), true, nil
	}
	for _, item := range strings.Split(part, ",") {
		m, err := parseItem(item, b)
		if err != nil {
			return 0, false, err
		}
		mask |= m
	}
	if mask == 0 {
		return 0, false, fmt.Errorf("empty field")
	}
	return mask, false, nil
}

func fullMask(min, max uint8) uint64 {
	var m uint64
	for v := min; v <= max; v++ {
		m |= 1 << v
	}
	return m
}

// parseItem parses one comma item: n, a-b, */s, a-b/s.
func parseItem(item string, b fieldBounds) (uint64, error) {
	step := 1
	rangePart := item
	if i := strings.IndexByte(item, '/'); i >= 0 {
		rangePart = item[:i]
		s, err := strconv.Atoi(item[i+1:])
		if err != nil || s < 1 {
			return 0, fmt.Errorf("step %q invalid", item[i+1:])
		}
		step = s
	}
	lo, hi := int(b.min), int(b.max)
	switch {
	case rangePart == "*":
		// keep full range with step
	case strings.Contains(rangePart, "-"):
		bits := strings.SplitN(rangePart, "-", 2)
		l, err := parseValue(bits[0], b)
		if err != nil {
			return 0, err
		}
		h, err := parseValue(bits[1], b)
		if err != nil {
			return 0, err
		}
		if l > h {
			return 0, fmt.Errorf("range %q inverted", rangePart)
		}
		lo, hi = int(l), int(h)
	default:
		v, err := parseValue(rangePart, b)
		if err != nil {
			return 0, err
		}
		lo, hi = int(v), int(v)
	}
	if lo < int(b.min) || hi > int(b.max) {
		return 0, fmt.Errorf("value out of %d-%d", b.min, b.max)
	}
	var m uint64
	for v := lo; v <= hi; v += step {
		m |= 1 << uint(v)
	}
	return m, nil
}

func parseValue(s string, b fieldBounds) (uint8, error) {
	if s == "" {
		return 0, fmt.Errorf("empty value")
	}
	if v, ok := b.aliases[strings.ToLower(s)]; ok {
		return v, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("value %q invalid", s)
	}
	if n == 7 && b.max == 6 {
		n = 0 // Sunday may be written as 7
	}
	return uint8(n), nil
}

func (e *Expression) matchMinute(t time.Time) bool { return e.fields[0]&(1<<uint(t.Minute())) != 0 }
func (e *Expression) matchHour(t time.Time) bool   { return e.fields[1]&(1<<uint(t.Hour())) != 0 }
func (e *Expression) matchMonth(t time.Time) bool  { return e.fields[3]&(1<<uint(int(t.Month()))) != 0 }

// matchDay enforces the classic cron day rule: when both day-of-month and
// day-of-week are restricted the day matches when EITHER does; when only one
// is restricted that one governs; when neither, every day matches.
func (e *Expression) matchDay(t time.Time) bool {
	dom := e.fields[2]&(1<<uint(t.Day())) != 0
	dow := e.fields[4]&(1<<uint(int(t.Weekday()))) != 0
	switch {
	case e.restricted[0] && e.restricted[1]:
		return dom || dow
	case e.restricted[0]:
		return dom
	case e.restricted[1]:
		return dow
	}
	return true
}

// Next answers the first fire time strictly after t (second/nanosecond
// zeroed). The zero time answers when nothing fires within the scan bound.
func (e *Expression) Next(t time.Time) time.Time {
	// Start at the next whole minute strictly after t.
	cur := t.Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(maxScanYears, 0, 0)
	for cur.Before(limit) {
		if e.matchMonth(cur) && e.matchDay(cur) && e.matchHour(cur) && e.matchMinute(cur) {
			return cur
		}
		cur = cur.Add(time.Minute)
	}
	return time.Time{}
}
