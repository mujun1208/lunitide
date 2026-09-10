package ocrapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	cooldownAuth = time.Minute
	cooldownRate = 5 * time.Minute
	cooldownBase = time.Minute
	cooldownMax  = 5 * time.Minute
)

type healthNote struct {
	Key      string    `json:"key"`
	Class    string    `json:"class"`
	Until    time.Time `json:"until"`
	Failures int       `json:"failures"`
}

type persistedHealth struct {
	Notes []healthNote `json:"notes"`
}

func (s *Service) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func healthKey(providerID, modelID, credentialGen, op string) string {
	return strings.TrimSpace(providerID) + "\x00" + strings.TrimSpace(modelID) + "\x00" + strings.TrimSpace(credentialGen) + "\x00" + strings.TrimSpace(op)
}

func healthOperation(key string) string {
	parts := strings.Split(key, "\x00")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func (s *Service) credentialGen(r Routing) string {
	if s == nil || s.credential == nil || !r.Bound() {
		return ""
	}
	return strings.TrimSpace(s.credential(r.ProviderID))
}

func (s *FileStore) healthPath() string {
	if s == nil {
		return ""
	}
	return filepath.Join(filepath.Dir(s.path), "ocr-health.json")
}

func (s *FileStore) loadHealth() ([]healthNote, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.healthPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var file persistedHealth
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	return file.Notes, nil
}

func (s *FileStore) saveHealth(notes []healthNote) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.healthPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	body, err := json.Marshal(persistedHealth{Notes: notes})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Service) loadHealth() {
	if s == nil {
		return
	}
	s.health = map[string]healthNote{}
	if s.store == nil {
		return
	}
	notes, err := s.store.loadHealth()
	if err != nil {
		return
	}
	now := s.currentTime()
	for _, note := range notes {
		if note.Key != "" && note.Until.After(now) {
			s.health[note.Key] = note
		}
	}
}

func (s *Service) persistHealthLocked() {
	if s == nil || s.store == nil {
		return
	}
	now := s.currentTime()
	notes := make([]healthNote, 0, len(s.health))
	for _, note := range s.health {
		if note.Until.After(now) {
			notes = append(notes, note)
		}
	}
	_ = s.store.saveHealth(notes)
}

func (s *Service) clearHealth() {
	if s == nil {
		return
	}
	s.healthMu.Lock()
	s.health = map[string]healthNote{}
	s.healthMu.Unlock()
	if s.store != nil {
		_ = s.store.saveHealth(nil)
	}
}

func (s *Service) cooling(r Routing, op string) bool {
	if s == nil || !r.Bound() {
		return false
	}
	key := healthKey(r.ProviderID, r.ModelID, s.credentialGen(r), op)
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	note, ok := s.health[key]
	return ok && note.Until.After(s.currentTime())
}

func (s *Service) noteFailure(r Routing, op, class string) {
	if s == nil || !r.Bound() {
		return
	}
	key := healthKey(r.ProviderID, r.ModelID, s.credentialGen(r), op)
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	if s.health == nil {
		s.health = map[string]healthNote{}
	}
	note := s.health[key]
	note.Key = key
	note.Class = class
	note.Failures++
	note.Until = s.currentTime().Add(cooldownDuration(class, note.Failures))
	s.health[key] = note
	s.persistHealthLocked()
}

func (s *Service) noteSuccess(r Routing, op string) {
	if s == nil || !r.Bound() {
		return
	}
	key := healthKey(r.ProviderID, r.ModelID, s.credentialGen(r), op)
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	delete(s.health, key)
	s.persistHealthLocked()
}

type FailureNote struct {
	Class     string
	Until     time.Time
	Operation string
}

type LocalReady struct {
	PDF     bool
	Image   bool
	Backend string
}

type HealthSnapshot struct {
	LastFailure *FailureNote
	Local       LocalReady
	Pack        PackStatus
}

func LocalOCRReady() LocalReady {
	if runtime.GOOS == "windows" {
		return LocalReady{PDF: true, Image: true, Backend: "windows-ocr"}
	}
	return LocalReady{Backend: "unavailable"}
}

func (s *Service) HealthSnapshot() HealthSnapshot {
	snap := HealthSnapshot{Local: LocalOCRReady(), Pack: DetectPPOcrPack("")}
	if s == nil {
		return snap
	}
	now := s.currentTime()
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	var best *healthNote
	for _, note := range s.health {
		if !note.Until.After(now) {
			continue
		}
		if best == nil || note.Until.After(best.Until) {
			n := note
			best = &n
		}
	}
	if best != nil {
		snap.LastFailure = &FailureNote{Class: best.Class, Until: best.Until, Operation: healthOperation(best.Key)}
	}
	return snap
}

func cooldownDuration(class string, failures int) time.Duration {
	switch class {
	case "auth":
		return cooldownAuth
	case "rate":
		return cooldownRate
	default:
		if failures >= 2 {
			return cooldownMax
		}
		return cooldownBase
	}
}

var (
	errProviderSkipped = errors.New("OCR 供应商已跳过")
	errProviderCooling = errors.New("OCR 供应商冷却中")
	errRawPDFProvider  = errors.New("供应商 OCR 不处理原始 PDF")
)

func (s *Service) tryProvider(ctx context.Context, raw []byte, hint string) (string, error) {
	if s == nil || s.provider == nil {
		return "", errProviderSkipped
	}
	if bytes.HasPrefix(raw, []byte("%PDF-")) {
		return "", errRawPDFProvider
	}
	routing, _ := s.Routing()
	if !routing.PreferProvider || !routing.Bound() {
		return "", errProviderSkipped
	}
	if s.cooling(routing, hint) {
		return "", errProviderCooling
	}
	text, err := s.provider(ctx, raw, hint)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		s.noteFailure(routing, hint, providerErrorClass(err))
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		s.noteFailure(routing, hint, "failed")
		return "", errors.New("OCR 供应商返回空结果")
	}
	s.noteSuccess(routing, hint)
	return text, nil
}
