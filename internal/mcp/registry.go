package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// ErrToolNotReadOnly: a declaration with ReadOnly=false is refused, so
	// a write-semantics tool can never be registered or listed.
	ErrToolNotReadOnly = errors.New("mcp: tool declaration is not read-only")
	// ErrToolNameInvalid: empty name or one containing path separators,
	// spaces or NUL.
	ErrToolNameInvalid = errors.New("mcp: tool name empty or contains separators/spaces")
	// ErrToolDuplicate: the same tool name declared twice.
	ErrToolDuplicate = errors.New("mcp: duplicate tool name")
	// ErrToolNotFound: invocation of an unregistered tool.
	ErrToolNotFound = errors.New("mcp: tool not registered")
	// ErrBreakerOpen: endpoint is inside its 60 s cooldown window.
	ErrBreakerOpen = errors.New("mcp: endpoint circuit breaker open")
	// ErrClientUnavailable: Invoke called without a usable client.
	ErrClientUnavailable = errors.New("mcp: remote client unavailable")
)

// Frozen M5 breaker parameters (design doc M5/02); changes need an ADR:
// five consecutive failures open the breaker for sixty seconds.
const (
	BreakerThreshold = 5
	BreakerCooldown  = 60 * time.Second
)

// ToolDecl is one remote tool advertisement. ReadOnly must be true; the
// registry refuses the whole declaration set otherwise, so a write tool
// can never become visible, even for a moment.
type ToolDecl struct {
	Name        string
	Endpoint    string
	ReadOnly    bool
	Description string
}

type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// breaker is the per-endpoint failure streak and its cooldown deadline.
type breaker struct {
	failures    int
	openedUntil time.Time
}

// Registry owns the read-only tool catalogue plus per-endpoint breaker
// state. Loading is all-or-nothing, so a half-valid declaration set is
// never observable.
type Registry struct {
	mu       sync.Mutex
	tools    map[string]ToolDecl
	breakers map[string]*breaker
	clock    Clock
}

// NewRegistry validates and atomically loads the declaration set:
// read-only only, well-formed names, no duplicates. One bad declaration
// rejects the whole set.
func NewRegistry(decls []ToolDecl) (*Registry, error) {
	tools := make(map[string]ToolDecl, len(decls))
	for _, d := range decls {
		if !d.ReadOnly {
			return nil, fmt.Errorf("%w: %q", ErrToolNotReadOnly, d.Name)
		}
		if d.Name == "" || strings.ContainsAny(d.Name, "/\\ \x00") {
			return nil, fmt.Errorf("%w: %q", ErrToolNameInvalid, d.Name)
		}
		if _, dup := tools[d.Name]; dup {
			return nil, fmt.Errorf("%w: %q", ErrToolDuplicate, d.Name)
		}
		tools[d.Name] = d
	}
	return &Registry{
		tools:    tools,
		breakers: make(map[string]*breaker),
		clock:    systemClock{},
	}, nil
}

// SetClock substitutes the wall clock (tests).
func (r *Registry) SetClock(c Clock) { r.clock = c }

// ListTools returns the registered read-only tools sorted by name; write
// tools never appear because they cannot be registered.
func (r *Registry) ListTools() []ToolDecl {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ToolDecl, 0, len(r.tools))
	for _, d := range r.tools {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RecordFailure counts one failed invocation; the fifth consecutive
// failure opens the breaker for the frozen 60 s cooldown.
func (r *Registry) RecordFailure(endpoint string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.breakerFor(endpoint)
	b.failures++
	if b.failures >= BreakerThreshold {
		b.openedUntil = r.clock.Now().Add(BreakerCooldown)
	}
}

// RecordSuccess clears an endpoint's failure streak.
func (r *Registry) RecordSuccess(endpoint string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.breakerFor(endpoint)
	b.failures = 0
	b.openedUntil = time.Time{}
}

// Invoke routes one call through the catalogue and the breaker: inside the
// 60 s cooldown window an endpoint fails fast with ErrBreakerOpen;
// otherwise the client runs and the outcome updates the streak.
func (r *Registry) Invoke(ctx context.Context, client *Client, tool string, args []byte) (InvokeResult, error) {
	r.mu.Lock()
	decl, ok := r.tools[tool]
	r.mu.Unlock()
	if !ok {
		return InvokeResult{}, fmt.Errorf("%w: %q", ErrToolNotFound, tool)
	}
	if err := r.breakerBlocked(decl.Endpoint); err != nil {
		return InvokeResult{}, err
	}
	if client == nil {
		return InvokeResult{}, ErrClientUnavailable
	}
	res, err := client.Invoke(ctx, InvokeInput{Tool: tool, ArgsJSON: args})
	if err != nil {
		r.RecordFailure(decl.Endpoint)
		return InvokeResult{}, err
	}
	r.RecordSuccess(decl.Endpoint)
	return res, nil
}

// breakerBlocked reports ErrBreakerOpen while now is inside the endpoint's
// cooldown window.
func (r *Registry) breakerBlocked(endpoint string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.breakers[endpoint]
	if !ok {
		return nil
	}
	if r.clock.Now().Before(b.openedUntil) {
		return fmt.Errorf("%w: %s cooling down for %s", ErrBreakerOpen, endpoint, BreakerCooldown)
	}
	return nil
}

// breakerFor lazily creates the endpoint's breaker record; caller holds
// the mutex.
func (r *Registry) breakerFor(endpoint string) *breaker {
	b, ok := r.breakers[endpoint]
	if !ok {
		b = &breaker{}
		r.breakers[endpoint] = b
	}
	return b
}
