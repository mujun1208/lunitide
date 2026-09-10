package connectorapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Recipe struct {
	ID              string `json:"id"`
	Scope           string `json:"scope"`
	RateLimit       string `json:"rateLimit,omitempty"`
	Revision        int    `json:"revision"`
	CredentialRef   string `json:"credentialRef,omitempty"`
	Health          string `json:"health,omitempty"`
	Status          string `json:"status"`
	PendingExternal bool   `json:"pendingExternal,omitempty"`
	Paused          bool   `json:"paused,omitempty"`
}

type FileStore struct {
	path     string
	onRevoke func(id string)
	mu       sync.Mutex
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func DefaultCatalog() []Recipe {
	return []Recipe{
		{ID: "ifind", Scope: "quotes", Status: "missing_credential", Paused: true},
		{ID: "tianyancha", Scope: "legal", Status: "missing_credential", Paused: true},
		{ID: "sec", Scope: "legal", PendingExternal: true, Status: "pending_external", Paused: true},
		{ID: "cloud-drive", Scope: "files", Status: "missing_credential", Paused: true},
		{ID: "git-host", Scope: "code", Status: "missing_credential", Paused: true},
		{ID: "logistics", Scope: "logistics", Status: "missing_credential", Paused: true},
		{ID: "social", Scope: "social", PendingExternal: true, Status: "pending_external", Paused: true},
	}
}

func (s *FileStore) ListOrCatalog() []Recipe {
	catalog := DefaultCatalog()
	if s == nil {
		return catalog
	}
	stored := s.List()
	byID := make(map[string]Recipe, len(catalog)+len(stored))
	for _, r := range catalog {
		byID[r.ID] = r
	}
	for _, r := range stored {
		byID[r.ID] = r
	}
	out := make([]Recipe, 0, len(byID))
	seen := make(map[string]bool, len(byID))
	for _, r := range catalog {
		if got, ok := byID[r.ID]; ok {
			out = append(out, got)
			seen[r.ID] = true
		}
	}
	for _, r := range stored {
		if !seen[r.ID] {
			out = append(out, r)
			seen[r.ID] = true
		}
	}
	return out
}

func catalogClosedStatus(id string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "ifind", "tianyancha", "cloud-drive", "git-host", "logistics":
		return "missing_credential", true
	case "sec", "sec-edgar", "social":
		return "pending_external", true
	default:
		return "", false
	}
}

func ForbiddenLookup(id string) bool {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "ifind", "tianyancha", "sec", "sec-edgar":
		return true
	default:
		return false
	}
}

func ImpersonateViaWebSearch(id string) bool {
	return false
}

func AttachmentReceipt(r Recipe) string {
	status := DeriveStatus(r)
	if status == "pending_external" || status == "missing_credential" {
		return status
	}
	return status
}

func DeriveStatus(r Recipe) string {
	if status, ok := catalogClosedStatus(r.ID); ok {
		return status
	}
	if r.PendingExternal {
		return "pending_external"
	}
	if strings.TrimSpace(r.CredentialRef) == "" {
		return "missing_credential"
	}
	return "ready"
}

func (s *FileStore) load() map[string]Recipe {
	out := map[string]Recipe{}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]Recipe{}
	}
	return out
}

func (s *FileStore) save(items map[string]Recipe) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0600)
}

func (s *FileStore) Put(r Recipe) (Recipe, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.load()
	if cur, ok := items[r.ID]; ok {
		r.Revision = cur.Revision + 1
	} else {
		r.Revision = 1
	}
	r.Status = DeriveStatus(r)
	if r.Status != "ready" {
		r.Paused = true
	}
	items[r.ID] = r
	return r, s.save(items)
}

func (s *FileStore) List() []Recipe {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.load()
	out := make([]Recipe, 0, len(items))
	for _, r := range items {
		r.Status = DeriveStatus(r)
		out = append(out, r)
	}
	return out
}

func (s *FileStore) Get(id string) (Recipe, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.load()[id]
	if ok {
		r.Status = DeriveStatus(r)
	}
	return r, ok
}

func (s *FileStore) SetRevokeHook(fn func(id string)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.onRevoke = fn
	s.mu.Unlock()
}

func (s *FileStore) RevokeCredential(id string) (Recipe, error) {
	s.mu.Lock()
	items := s.load()
	r := items[id]
	r.ID = id
	r.CredentialRef = ""
	r.Paused = true
	r.Revision++
	r.Status = DeriveStatus(r)
	items[id] = r
	hook := s.onRevoke
	err := s.save(items)
	s.mu.Unlock()
	if err == nil && hook != nil {
		hook(id)
	}
	return r, err
}

func (s *FileStore) BackgroundAllowed(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.load()[id]
	return ok && !r.Paused && DeriveStatus(r) == "ready"
}
