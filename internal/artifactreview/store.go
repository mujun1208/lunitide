// Package artifactreview persists the P2-2 artifact acceptance loop
// (comment → revise → accept) as an append-only review log. Storage is a
// single JSON document next to the tool workspaces (same file-based
// settings-plane precedent as command-policy.json): single-user desktop
// product, low write volume, atomic replace, bounded history.
package artifactreview

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// Review actions (frozen wire enum).
const (
	ActionComment = "comment"
	ActionRevise  = "revise"
	ActionAccept  = "accept"
)

// MaxReviews bounds the persisted log (oldest entries drop first).
const MaxReviews = 10000

var ErrInvalid = errors.New("artifactreview: invalid review")

// Review is one append-only review record.
type Review struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionId"`
	CallID    string    `json:"callId"`
	ToolName  string    `json:"toolName,omitempty"`
	Kind      string    `json:"kind"`
	Path      string    `json:"path"`
	Action    string    `json:"action"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// KindValid accepts the artifact kinds the chat pipeline can emit.
func KindValid(kind string) bool {
	switch kind {
	case "html", "xlsx", "docx", "pptx", "pdf":
		return true
	}
	return false
}

// ActionValid accepts the frozen action enum.
func ActionValid(action string) bool {
	return action == ActionComment || action == ActionRevise || action == ActionAccept
}

// Store is the file-backed review log.
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore opens (or lazily creates) the review log at
// <root>/artifact-reviews.json.
func NewStore(root string) (*Store, error) {
	if !filepath.IsAbs(root) {
		return nil, ErrInvalid
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(root, "artifact-reviews.json")}, nil
}

// Append validates and appends one review, then persists atomically.
func (s *Store) Append(sessionID, callID, toolName, kind, path, action, note string) (Review, error) {
	if len(sessionID) != 26 || callID == "" || len(callID) > 128 ||
		!KindValid(kind) || path == "" || len(path) > 512 ||
		!ActionValid(action) || len(note) > 4000 || strings.ContainsRune(note, 0) {
		return Review{}, ErrInvalid
	}
	r := Review{
		ID: ulid.Make().String(), SessionID: sessionID, CallID: callID,
		ToolName: toolName, Kind: kind, Path: path, Action: action,
		Note: note, CreatedAt: time.Now().UTC(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return Review{}, err
	}
	list = append(list, r)
	if len(list) > MaxReviews {
		list = list[len(list)-MaxReviews:]
	}
	if err := s.save(list); err != nil {
		return Review{}, err
	}
	return r, nil
}

// ListBySession answers the session's reviews plus the accepted-path set
// (a path is accepted when its newest review is an accept).
func (s *Store) ListBySession(sessionID string) ([]Review, map[string]bool, error) {
	if len(sessionID) != 26 {
		return nil, nil, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return nil, nil, err
	}
	out := make([]Review, 0, 8)
	latest := map[string]Review{}
	for _, r := range list {
		if r.SessionID != sessionID {
			continue
		}
		out = append(out, r)
		// Append-ordered log: later rows win ties, so >= keeps the newest.
		if prior, ok := latest[r.Path]; !ok || !r.CreatedAt.Before(prior.CreatedAt) {
			latest[r.Path] = r
		}
	}
	accepted := map[string]bool{}
	for path, r := range latest {
		if r.Action == ActionAccept {
			accepted[path] = true
		}
	}
	return out, accepted, nil
}

// load reads the persisted list; a missing file answers an empty list, a
// corrupt file is surfaced (fail-closed, never silently reset).
func (s *Store) load() ([]Review, error) {
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Review
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// save persists atomically (temp + explicit remove + rename for Windows).
func (s *Store) save(list []Review) error {
	b, err := json.MarshalIndent(list, "", " ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	_ = os.Remove(s.path)
	return os.Rename(tmp, s.path)
}
