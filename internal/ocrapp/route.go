package ocrapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var ErrRevisionConflict = errors.New("ocr routing revision conflict")

// Routing is the independent OCR strategy record. It is not a seventh
// capability role and does not use KindOCR.
type Routing struct {
	ProviderID     string `json:"providerId,omitempty"`
	ModelID        string `json:"modelId,omitempty"`
	PreferProvider bool   `json:"preferProvider"`
	Revision       string `json:"revision"`
	UpdatedAt      string `json:"updatedAt,omitempty"`
}

func (r Routing) Bound() bool {
	return r.ProviderID != "" && r.ModelID != ""
}

func RoutingRevision(r Routing) string {
	raw, _ := json.Marshal(struct {
		ProviderID     string `json:"providerId"`
		ModelID        string `json:"modelId"`
		PreferProvider bool   `json:"preferProvider"`
		UpdatedAt      string `json:"updatedAt"`
	}{r.ProviderID, r.ModelID, r.PreferProvider, r.UpdatedAt})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

type FileStore struct {
	path string
	mu   sync.Mutex
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func (s *FileStore) Get() (Routing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked()
}

func (s *FileStore) readLocked() (Routing, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			r := Routing{PreferProvider: true}
			r.Revision = RoutingRevision(r)
			return r, nil
		}
		return Routing{}, err
	}
	var r Routing
	if err := json.Unmarshal(raw, &r); err != nil {
		return Routing{}, err
	}
	if r.Revision == "" {
		r.Revision = RoutingRevision(r)
	}
	return r, nil
}

func (s *FileStore) CompareAndSet(next Routing, expected string) (Routing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.readLocked()
	if err != nil {
		return Routing{}, err
	}
	if expected == "" || cur.Revision != expected {
		return cur, ErrRevisionConflict
	}
	if (next.ProviderID == "") != (next.ModelID == "") {
		return cur, errors.New("providerId 与 modelId 必须同时填写")
	}
	next.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	next.Revision = RoutingRevision(next)
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return cur, err
	}
	body, err := json.Marshal(next)
	if err != nil {
		return cur, err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, body, 0600); err != nil {
		return cur, err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return cur, err
	}
	return next, nil
}
