package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
)

// Interactive HTML previews are served from their own origin, and a ticket is the
// only name a file has there. A ticket authorizes exactly one folder — the one
// holding the previewed document — for a short while, so a generated page can
// load its own stylesheet and script without the preview origin ever being able
// to address the rest of the machine.
//
// Resolution deliberately goes back through ResolveSessionArtifact rather than
// joining paths here: that function already enforces the session-root
// containment rules the rest of the product relies on, and a second, parallel
// implementation of containment is how these bugs get written.
const (
	previewTicketTTL = 30 * time.Minute
	// Room for a user who opens many previews. Eviction is by age, so a long
	// reading session on one document cannot be pushed out by churn elsewhere.
	previewTicketMax = 64
	// A preview subresource is a page asset (script, stylesheet, image, font).
	// Anything larger is not something a preview needs in order to render.
	previewAssetMaxBytes = 16 << 20
)

type previewTicket struct {
	sessionID string
	rootRel   string // slash-relative path of the document itself
	baseRel   string // its folder, "" when the document sits at the session root
	expiresAt time.Time
}

type previewTicketStore struct {
	mu      sync.Mutex
	tickets map[string]previewTicket
	now     func() time.Time
}

func newPreviewTicketStore() *previewTicketStore {
	return &previewTicketStore{tickets: map[string]previewTicket{}, now: time.Now}
}

// mint returns a fresh token addressing relPath inside sessionID. Tokens are
// never reused for the same document: a new one means a stale renderer cannot
// keep a folder reachable by holding on to an old URL.
func (s *previewTicketStore) mint(sessionID, relPath string) (string, error) {
	rel, ok := normalizePreviewRel(relPath)
	if !ok || sessionID == "" {
		return "", errors.New("preview: unusable artifact path")
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	base := path.Dir(rel)
	if base == "." || base == "/" {
		base = ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictLocked()
	s.tickets[token] = previewTicket{sessionID: sessionID, rootRel: rel, baseRel: base, expiresAt: s.now().Add(previewTicketTTL)}
	return token, nil
}

// lookup answers the session and workspace-relative path a request addresses. An
// empty asset path means the ticket's own document; anything else is resolved as
// a sibling of it.
func (s *previewTicketStore) lookup(token, assetRel string) (sessionID, relPath string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ticket, ok := s.tickets[token]
	if !ok {
		return "", "", errors.New("preview: unknown ticket")
	}
	if s.now().After(ticket.expiresAt) {
		delete(s.tickets, token)
		return "", "", errors.New("preview: ticket expired")
	}
	if assetRel == "" {
		return ticket.sessionID, ticket.rootRel, nil
	}
	asset, ok := normalizePreviewRel(assetRel)
	if !ok {
		return "", "", errors.New("preview: unusable asset path")
	}
	if ticket.baseRel == "" {
		return ticket.sessionID, asset, nil
	}
	return ticket.sessionID, ticket.baseRel + "/" + asset, nil
}

// evictLocked drops expired tickets, then the oldest if the store is still full.
func (s *previewTicketStore) evictLocked() {
	now := s.now()
	for token, ticket := range s.tickets {
		if now.After(ticket.expiresAt) {
			delete(s.tickets, token)
		}
	}
	for len(s.tickets) >= previewTicketMax {
		oldest, at := "", time.Time{}
		for token, ticket := range s.tickets {
			if at.IsZero() || ticket.expiresAt.Before(at) {
				oldest, at = token, ticket.expiresAt
			}
		}
		if oldest == "" {
			return
		}
		delete(s.tickets, oldest)
	}
}

// normalizePreviewRel is the engine's own view of a safe workspace-relative
// path. The host parser already rejected traversal on the wire; this refuses it
// again because the two live far apart and only one of them is guaranteed to
// have run.
func normalizePreviewRel(raw string) (string, bool) {
	rel := strings.ReplaceAll(strings.TrimSpace(raw), `\`, "/")
	if rel == "" || strings.ContainsAny(rel, "\x00:") || strings.HasPrefix(rel, "/") {
		return "", false
	}
	clean := path.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", false
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", false
		}
	}
	return clean, true
}

// ResolvePreviewTicket answers the host's resource broker: a token plus a
// relative asset path become an absolute file and its MIME type, or an error.
// Containment is enforced by ResolveSessionArtifact, so a ticket can only ever
// reach files the session already authorizes.
func (e *Engine) ResolvePreviewTicket(ctx context.Context, token, assetRel string) (string, int64, error) {
	if e == nil || e.previewTickets == nil || e.tools == nil {
		return "", 0, errors.New("preview: unavailable")
	}
	sessionID, relPath, err := e.previewTickets.lookup(token, assetRel)
	if err != nil {
		return "", 0, err
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	target, err := e.tools.ResolveSessionArtifact(sessionID, relPath)
	if err != nil {
		return "", 0, err
	}
	info, statErr := os.Stat(target)
	if statErr != nil || !info.Mode().IsRegular() {
		return "", 0, errors.New("preview: not a file")
	}
	if info.Size() > previewAssetMaxBytes {
		return "", 0, errors.New("preview: asset too large")
	}
	return target, info.Size(), nil
}

// previewOriginPrefix mirrors webviewhost.PreviewOrigin + PreviewPathPrefix. The
// engine does not import the host (media playback URLs are built the same way),
// so a test pins the two spellings together.
const previewOriginPrefix = "https://preview.lunitide.local/p/"

// handleInternalPreviewAssetResolve is the host's only route into the file
// system for the preview origin. It is host-private, like the media equivalent:
// the renderer must never be able to turn a token into a path.
func handleInternalPreviewAssetResolve(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Token string `json:"token"`
		Rel   string `json:"rel"`
	}
	if decodePayload(r.Payload, &p) != nil || p.Token == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "internal.preview.asset.resolve 参数无效", false)
	}
	path, size, err := e.ResolvePreviewTicket(ctx, p.Token, p.Rel)
	if err != nil {
		return r.Fail("PREVIEW_TICKET_EXPIRED", "预览已过期", false)
	}
	return r.Ok(map[string]any{"path": path, "size": size})
}

// MintPreviewTicket hands the renderer the URL to frame for an interactive
// preview.
//
// The URL carries only the document's file name, never its folder: assets resolve
// relative to it, and the ticket supplies the folder. Putting the workspace path
// in the URL as well would make every relative asset resolve one folder deeper
// than it lives.
func (e *Engine) MintPreviewTicket(sessionID, relPath string) (string, error) {
	if e == nil || e.previewTickets == nil {
		return "", errors.New("preview: unavailable")
	}
	token, err := e.previewTickets.mint(sessionID, relPath)
	if err != nil {
		return "", err
	}
	name, ok := normalizePreviewRel(relPath)
	if !ok {
		return "", errors.New("preview: unusable artifact path")
	}
	return previewOriginPrefix + token + "/" + path.Base(name), nil
}
