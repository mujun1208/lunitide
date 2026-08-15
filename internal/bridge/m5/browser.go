// T-5.4.3 browser bridge: browser.act with the four page ops (navigate |
// read | click | type) plus snapshot. The bridge never talks to a browser
// itself: the real page driver lives in the host (low-privilege) process
// and is injected through PageDriver; snapshot bytes leave through the
// artifact registry (ArtifactSink) so untrusted page content lands in the
// CAS with its sha256 journalled in m5_artifact and only the artifact id
// crosses the wire. Sensitive intents (download / upload / login / payment)
// are not ops at all: the dispatch table has no entry for them and the
// default branch answers ErrBrowserOpInvalid (BRW-001).
package m5

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"

	"github.com/lunitide/lunitide/internal/browser"
	"github.com/lunitide/lunitide/internal/domain/m5workspace"
)

var (
	// ErrBrowserOpInvalid maps to BRW-001: op must be navigate, read,
	// click, type or snapshot (sensitive intents are not ops at all).
	ErrBrowserOpInvalid = errors.New("m5: op must be navigate, read, click, type or snapshot")
	// ErrBrowserURLRequired: navigate needs a url.
	ErrBrowserURLRequired = errors.New("m5: navigate requires a url")
	// ErrBrowserSelectorRequired: click and type address elements by selector.
	ErrBrowserSelectorRequired = errors.New("m5: click and type require a selector")
	// ErrBrowserRunRequired: snapshot artifacts must be journalled under a run.
	ErrBrowserRunRequired = errors.New("m5: snapshot requires a runId")
	// ErrBrowserDriverUnavailable is a wiring bug: no page driver injected.
	ErrBrowserDriverUnavailable = errors.New("m5: browser page driver unavailable")
	// ErrBrowserSinkUnavailable is a wiring bug: no artifact sink injected.
	ErrBrowserSinkUnavailable = errors.New("m5: browser artifact sink unavailable")
)

// BrowserReadMaxBytes caps read text at 256 KiB so a hostile page cannot
// flood the transcript.
const BrowserReadMaxBytes = 256 << 10

// BrowserInput is the unified browser op request.
type BrowserInput struct {
	Op        string `json:"op"` // navigate | read | click | type | snapshot
	RunID     string `json:"runId"`
	URL       string `json:"url"`
	Selector  string `json:"selector"`
	Text      string `json:"text"`
	SessionID string `json:"sessionId"`
}

// Element is one interactive surface row surfaced to the agent.
type Element struct {
	RefID string `json:"refId"`
	Role  string `json:"role"`
	Label string `json:"label"`
}

// BrowserResult carries the page view for one op.
type BrowserResult struct {
	OK                 bool      `json:"ok"`
	URL                string    `json:"url"`
	Title              string    `json:"title"`
	Elements           []Element `json:"elements,omitempty"`
	SnapshotArtifactID string    `json:"snapshotArtifactId,omitempty"`
	TextContent        string    `json:"textContent,omitempty"`
	EventSeq           int64     `json:"eventSeq"`
}

// PageDriver is the narrow page surface the bridge depends on; the real
// driver runs in the host (low-privilege) process.
type PageDriver interface {
	Navigate(url string) (title string, err error)
	Read() (text string, err error)
	Click(refID string) error
	Type(refID, text string) error
	Snapshot() (png []byte, err error)
	CurrentURL() string
}

// ArtifactSink journals snapshot bytes; internal/artifact.Registry
// satisfies it (Register pins mime/size/sha256 into m5_artifact).
type ArtifactSink interface {
	Register(ctx context.Context, runID, mime, generator string, data []byte) (m5workspace.Artifact, error)
}

// BrowserResolver is the DNS gate for navigate; tests inject fakes,
// production uses browser.ResolveAndCheck (BRW-002).
type BrowserResolver func(host string) ([]net.IP, error)

// BrowserService implements browser.act over an injected page driver.
type BrowserService struct {
	driver  PageDriver
	sink    ArtifactSink
	resolve BrowserResolver

	mu       sync.Mutex
	eventSeq int64
}

func NewBrowserService(driver PageDriver, sink ArtifactSink) *BrowserService {
	return &BrowserService{driver: driver, sink: sink, resolve: browser.ResolveAndCheck}
}

// SetResolver substitutes the DNS gate (tests).
func (s *BrowserService) SetResolver(r BrowserResolver) { s.resolve = r }

func (s *BrowserService) nextEventSeq() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventSeq++
	return s.eventSeq
}

func (s *BrowserService) currentURL(fallback string) string {
	if u := s.driver.CurrentURL(); u != "" {
		return u
	}
	return fallback
}

// Op dispatches after op validation. Unknown ops — which by construction
// includes every sensitive intent (download/upload/login/payment,
// BRW-001) — answer ErrBrowserOpInvalid.
func (s *BrowserService) Op(ctx context.Context, in BrowserInput) (BrowserResult, error) {
	if s.driver == nil {
		return BrowserResult{}, ErrBrowserDriverUnavailable
	}
	switch in.Op {
	case "navigate":
		return s.navigate(ctx, in)
	case "read":
		return s.read(in)
	case "click":
		return s.act(in, true)
	case "type":
		return s.act(in, false)
	case "snapshot":
		return s.snapshot(ctx, in)
	default:
		return BrowserResult{}, ErrBrowserOpInvalid
	}
}

// navigate runs the full policy gate (URL allowlist + literal
// classification, then resolve-and-classify for domain hosts — BRW-001 /
// BRW-002) before the driver ever sees the URL.
func (s *BrowserService) navigate(ctx context.Context, in BrowserInput) (BrowserResult, error) {
	if strings.TrimSpace(in.URL) == "" {
		return BrowserResult{}, ErrBrowserURLRequired
	}
	if err := browser.CheckURL(in.URL); err != nil {
		return BrowserResult{}, err
	}
	u, err := url.Parse(in.URL)
	if err != nil {
		return BrowserResult{}, err
	}
	if s.resolve != nil {
		ips, err := s.resolve(u.Hostname())
		if err != nil {
			return BrowserResult{}, err
		}
		// Defense in depth: the zero-escape invariant is enforced at the
		// point of consumption, so even an injected resolver cannot smuggle
		// a non-public address past the classifier (BRW-002).
		for _, ip := range ips {
			if err := browser.ClassifyIP(ip); err != nil {
				return BrowserResult{}, fmt.Errorf("%w (host %s)", err, u.Hostname())
			}
		}
	}
	title, err := s.driver.Navigate(in.URL)
	if err != nil {
		return BrowserResult{}, fmt.Errorf("m5: navigate: %w", err)
	}
	return BrowserResult{
		OK:       true,
		URL:      s.currentURL(in.URL),
		Title:    title,
		EventSeq: s.nextEventSeq(),
	}, nil
}

func (s *BrowserService) read(in BrowserInput) (BrowserResult, error) {
	text, err := s.driver.Read()
	if err != nil {
		return BrowserResult{}, fmt.Errorf("m5: read: %w", err)
	}
	if len(text) > BrowserReadMaxBytes {
		text = text[:BrowserReadMaxBytes]
	}
	return BrowserResult{
		OK:          true,
		URL:         s.currentURL(""),
		TextContent: text,
		EventSeq:    s.nextEventSeq(),
	}, nil
}

// act covers click and type: element ops address their target by selector.
func (s *BrowserService) act(in BrowserInput, isClick bool) (BrowserResult, error) {
	if strings.TrimSpace(in.Selector) == "" {
		return BrowserResult{}, ErrBrowserSelectorRequired
	}
	if isClick {
		if err := s.driver.Click(in.Selector); err != nil {
			return BrowserResult{}, fmt.Errorf("m5: click: %w", err)
		}
	} else {
		if err := s.driver.Type(in.Selector, in.Text); err != nil {
			return BrowserResult{}, fmt.Errorf("m5: type: %w", err)
		}
	}
	return BrowserResult{OK: true, URL: s.currentURL(""), EventSeq: s.nextEventSeq()}, nil
}

// snapshot registers the PNG through the artifact pipeline: untrusted page
// bytes land in the CAS with mime/size/sha256 journalled in m5_artifact
// and only the artifact id crosses the wire.
func (s *BrowserService) snapshot(ctx context.Context, in BrowserInput) (BrowserResult, error) {
	if s.sink == nil {
		return BrowserResult{}, ErrBrowserSinkUnavailable
	}
	if in.RunID == "" {
		return BrowserResult{}, ErrBrowserRunRequired
	}
	png, err := s.driver.Snapshot()
	if err != nil {
		return BrowserResult{}, fmt.Errorf("m5: snapshot: %w", err)
	}
	a, err := s.sink.Register(ctx, in.RunID, "image/png", "browser.snapshot", png)
	if err != nil {
		return BrowserResult{}, err
	}
	return BrowserResult{
		OK:                 true,
		URL:                s.currentURL(""),
		SnapshotArtifactID: a.ID,
		EventSeq:           s.nextEventSeq(),
	}, nil
}
