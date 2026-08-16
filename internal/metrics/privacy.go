// Package metrics implements the M9 slice-5 privacy-preserving operational
// aggregation (T-9.5.1, ADR-016 v1.1.0 frozen values, threat model m9-06
// S8/T-20).
//
// Frozen privacy decisions (ADR-016 §5 — do not change without a new ADR):
//   - k threshold = 5: any group with fewer than 5 DISTINCT subjects is
//     suppressed — the output carries no count, no sum, no mean (M9-030).
//   - subject dedup: one subject contributes at most once to the group k.
//   - window: minimum width 24h, aligned to natural day boundaries.
//   - composable filters: only frozen dimensions (org/scope/runner-tier);
//     drilling down by prompt / file / personal trajectory / existence is
//     refused outright (group keys with user:/prompt:/file:/session:
//     prefixes).
//   - cross-window differential control: consecutive queries for the same
//     (org, group) must not use overlapping windows.
//   - progressive rollout: per-org opt-in flag, default off; every query is
//     journaled for access review (访问复核).
//
// Concept error taxonomy (04 错误目录, wire-alias only):
//
//	M9-030 METRIC_PRIVACY_THRESHOLD 低于隐私阈值
package metrics

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ConceptError is an M9 concept-taxonomy error (code + name).
type ConceptError struct {
	Code string
	Name string
}

func (e *ConceptError) Error() string { return e.Code + " " + e.Name }

func (e *ConceptError) Is(target error) bool {
	var other *ConceptError
	if errors.As(target, &other) {
		return other.Code == e.Code
	}
	return false
}

// ErrPrivacyThreshold answers suppressed low-sample output and forbidden
// drill-down queries (M9-030).
var ErrPrivacyThreshold = &ConceptError{"M9-030", "METRIC_PRIVACY_THRESHOLD"}

// M9Code extracts the M9 concept code when err carries one.
func M9Code(err error) string {
	var ce *ConceptError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// FrozenK is the k-anonymity threshold (ADR-016 §5).
const FrozenK = 5

// MinWindow is the minimum aggregation window width (ADR-016 §5).
const MinWindow = 24 * time.Hour

// forbiddenPrefixes are the drill-down dimensions that can single out an
// individual trajectory (ADR-016 §5: 禁止按提示、文件、个人轨迹或存在性下钻).
var forbiddenPrefixes = []string{"user:", "prompt:", "file:", "session:"}

// Sample is one raw observation. Subject is internal-only: it participates
// in the distinct-count, never in any output.
type Sample struct {
	GroupKey string // frozen dimension combination, e.g. "scope=aml/runner=local"
	Subject  string // dedup key (user/prompt identity — never output)
	Value    float64
}

// Aggregate is one group's output row. Suppressed groups carry zero values
// and a fixed notice; the caller renders "样本不足" and nothing else.
type Aggregate struct {
	GroupKey   string
	From, To   time.Time
	Suppressed bool
	Subjects   int // only meaningful when !Suppressed
	Sum        float64
	Mean       float64
}

// ReviewLog is one access-review entry (访问复核).
type ReviewLog struct {
	At        time.Time
	Viewer    string
	OrgID     string
	From, To  time.Time
	Groups    int
	Suppressed int
}

// Engine is the privacy aggregation engine with per-org progressive rollout.
type Engine struct {
	mu       sync.Mutex
	k        int
	rollout  map[string]bool
	windows  map[string]reviewWindow // org|group → last queried window
	reviews  []ReviewLog
}

type reviewWindow struct{ from, to time.Time }

// NewEngine builds an engine with the frozen k (k<=0 falls back to FrozenK).
func NewEngine(k int) *Engine {
	if k <= 0 {
		k = FrozenK
	}
	return &Engine{
		k:       k,
		rollout: make(map[string]bool),
		windows: make(map[string]reviewWindow),
	}
}

// SetRollout flips one org's progressive-release flag (default off).
func (e *Engine) SetRollout(orgID string, enabled bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rollout[orgID] = enabled
}

// Rollout reports whether privacy aggregation is enabled for one org.
func (e *Engine) Rollout(orgID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rollout[orgID]
}

// windowOK validates natural-day alignment and the 24h minimum width.
func windowOK(from, to time.Time) error {
	if !from.Before(to) {
		return errors.New("metrics: window must be from < to")
	}
	if to.Sub(from) < MinWindow {
		return fmt.Errorf("metrics: window %s shorter than frozen minimum %s", to.Sub(from), MinWindow)
	}
	ud := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()) }
	if !ud(from).Equal(from) || !ud(to).Equal(to) {
		return errors.New("metrics: window must align to natural day boundaries")
	}
	return nil
}

// groupKeyOK refuses forbidden drill-down dimensions.
func groupKeyOK(key string) error {
	lower := strings.ToLower(key)
	for _, p := range forbiddenPrefixes {
		for _, seg := range strings.FieldsFunc(lower, func(r rune) bool { return r == '/' || r == ',' }) {
			if strings.HasPrefix(strings.TrimSpace(seg), p) {
				return fmt.Errorf("%w: group key %q drills down by %s — personal-trajectory filtering is forbidden", ErrPrivacyThreshold, key, p)
			}
		}
	}
	return nil
}

// Aggregate computes one org's window aggregates. Rules enforced, in order:
// progressive rollout (org must be opted in), window validity (24h minimum,
// day-aligned), forbidden drill-down refusal (M9-030), per-group distinct
// subject count with suppression below k (M9-030 — no count/sum/mean leaks),
// and cross-window non-overlap for repeated (org, group) queries. Every call
// appends an access-review entry.
func (e *Engine) Aggregate(orgID, viewer string, from, to time.Time, samples []Sample, now time.Time) ([]Aggregate, error) {
	if orgID == "" {
		return nil, errors.New("metrics: org id required")
	}
	if !e.Rollout(orgID) {
		return nil, fmt.Errorf("metrics: privacy aggregation is not enabled for org %s (progressive rollout off)", orgID)
	}
	if err := windowOK(from, to); err != nil {
		return nil, err
	}
	groups := make(map[string][]Sample)
	for _, s := range samples {
		if err := groupKeyOK(s.GroupKey); err != nil {
			return nil, err
		}
		groups[s.GroupKey] = append(groups[s.GroupKey], s)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Cross-window differential control: overlapping windows for the same
	// (org, group) let a curious admin diff two near-identical aggregates
	// and isolate one subject — refused (ADR-016 §5).
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w, seen := e.windows[orgID+"|"+k]
		if seen && from.Before(w.to) && to.After(w.from) {
			return nil, fmt.Errorf("%w: overlapping window for group %q refused (cross-window differential control)", ErrPrivacyThreshold, k)
		}
	}

	out := make([]Aggregate, 0, len(keys))
	suppressed := 0
	for _, k := range keys {
		distinct := make(map[string]float64)
		sum := 0.0
		for _, s := range groups[k] {
			if _, dup := distinct[s.Subject]; !dup {
				distinct[s.Subject] = 0
			}
			distinct[s.Subject] += s.Value
			sum += s.Value
		}
		agg := Aggregate{GroupKey: k, From: from, To: to}
		if len(distinct) < e.k {
			// Suppressed: no count, no sum, no mean — a fixed notice only.
			agg.Suppressed = true
			suppressed++
		} else {
			agg.Subjects = len(distinct)
			agg.Sum = sum
			agg.Mean = sum / float64(len(groups[k]))
		}
		out = append(out, agg)
		e.windows[orgID+"|"+k] = reviewWindow{from: from, to: to}
	}
	e.reviews = append(e.reviews, ReviewLog{
		At: now, Viewer: viewer, OrgID: orgID, From: from, To: to,
		Groups: len(out), Suppressed: suppressed,
	})
	return out, nil
}

// Reviews returns the access-review trail (访问复核).
func (e *Engine) Reviews() []ReviewLog {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]ReviewLog, len(e.reviews))
	copy(out, e.reviews)
	return out
}
