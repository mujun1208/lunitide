package ocrapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrRevisionConflict      = errors.New("ocr routing revision conflict")
	ErrLegacyEngineUnwired   = errors.New("PP-OCR 仅登记未接线，不能作为可执行引擎")
	ErrDocumentEngineUnready = errors.New("尚无经验证的 Windows 运行包，不能保存文档引擎偏好")
)

// Routing is the independent OCR strategy record. It is not a seventh
// capability role and does not use KindOCR.
type Routing struct {
	ProviderID      string                  `json:"providerId,omitempty"`
	ModelID         string                  `json:"modelId,omitempty"`
	PreferProvider  bool                    `json:"preferProvider"`
	LocalEngine     string                  `json:"localEngine,omitempty"`
	PackRoot        string                  `json:"packRoot,omitempty"`
	Policy          Policy                  `json:"policy,omitempty"`
	ProjectPolicies map[string]PolicyRecord `json:"projectPolicies,omitempty"`
	ProjectLegacy   map[string]string       `json:"projectLegacy,omitempty"`
	Revision        string                  `json:"revision"`
	UpdatedAt       string                  `json:"updatedAt,omitempty"`
}

type Policy struct {
	Mode                  string   `json:"mode"`
	ComplexDocumentEngine string   `json:"complexDocumentEngine"`
	FallbackOrder         []string `json:"fallbackOrder"`
	SendToCloud           string   `json:"sendToCloud"`
	ProviderID            string   `json:"providerId,omitempty"`
	ModelID               string   `json:"modelId,omitempty"`
}

type PolicyRecord struct {
	Policy    Policy `json:"policy"`
	Revision  string `json:"revision"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type ScopedRouting struct {
	Policy     Policy
	Revision   string
	Inherited  bool
	SourceKind string
	SourceID   string
	PackRoot   string
}

func (r Routing) Bound() bool {
	return r.ProviderID != "" && r.ModelID != ""
}

func DefaultPolicy() Policy {
	return Policy{
		Mode:                  "auto",
		ComplexDocumentEngine: "none",
		FallbackOrder:         []string{"ppocr", "windows-ocr"},
		SendToCloud:           "never",
	}
}

func PolicyRevision(p Policy) string {
	raw, _ := json.Marshal(struct {
		Mode                  string   `json:"mode"`
		ComplexDocumentEngine string   `json:"complexDocumentEngine"`
		FallbackOrder         []string `json:"fallbackOrder"`
		SendToCloud           string   `json:"sendToCloud"`
		ProviderID            string   `json:"providerId"`
		ModelID               string   `json:"modelId"`
	}{p.Mode, p.ComplexDocumentEngine, p.FallbackOrder, p.SendToCloud, p.ProviderID, p.ModelID})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func InheritedRevision(requestedKind, requestedID, sourceKind, sourceID, sourceRevision string) string {
	raw, _ := json.Marshal(struct {
		RequestedKind string `json:"requestedScopeKind"`
		RequestedID   string `json:"requestedScopeId"`
		SourceKind    string `json:"sourceScopeKind"`
		SourceID      string `json:"sourceScopeId"`
		SourceRev     string `json:"sourcePolicyRevision"`
	}{requestedKind, requestedID, sourceKind, sourceID, sourceRevision})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func EffectivePolicy(r Routing) Policy {
	if strings.TrimSpace(r.Policy.Mode) != "" {
		p := r.Policy
		if p.FallbackOrder == nil {
			p.FallbackOrder = defaultFallbackOrder(r.Bound())
		}
		return p
	}
	if r.Bound() {
		return Policy{
			Mode:                  "provider_first",
			ComplexDocumentEngine: "none",
			FallbackOrder:         defaultFallbackOrder(true),
			SendToCloud:           "configured_only",
			ProviderID:            r.ProviderID,
			ModelID:               r.ModelID,
		}
	}
	return DefaultPolicy()
}

func defaultFallbackOrder(bound bool) []string {
	if bound {
		return []string{"provider", "ppocr", "windows-ocr"}
	}
	return []string{"ppocr", "windows-ocr"}
}

func ValidatePolicy(p Policy) error {
	switch p.Mode {
	case "auto", "local_fast", "local_document", "provider_first":
	default:
		return errors.New("OCR 策略无效")
	}
	if p.Mode == "local_document" {
		return ErrDocumentEngineUnready
	}
	if p.ComplexDocumentEngine != "none" && p.ComplexDocumentEngine != "paddleocr-vl-1.6" {
		return errors.New("OCR 策略无效")
	}
	if p.SendToCloud != "never" && p.SendToCloud != "configured_only" {
		return errors.New("OCR 策略无效")
	}
	if len(p.FallbackOrder) == 0 || len(p.FallbackOrder) > 3 {
		return errors.New("OCR 策略无效")
	}
	seen := map[string]bool{}
	needsProvider := p.Mode == "provider_first"
	for _, item := range p.FallbackOrder {
		if seen[item] {
			return errors.New("OCR 策略无效")
		}
		seen[item] = true
		switch item {
		case "windows-ocr", "paddleocr-vl-1.6", "provider", "ppocr":
		default:
			return errors.New("OCR 策略无效")
		}
		if item == "provider" {
			needsProvider = true
		}
		if item == "paddleocr-vl-1.6" && p.ComplexDocumentEngine != "paddleocr-vl-1.6" {
			return errors.New("OCR 策略无效")
		}
	}
	if (p.ProviderID == "") != (p.ModelID == "") {
		return errors.New("providerId 与 modelId 必须同时填写")
	}
	if needsProvider {
		if p.ProviderID == "" || p.ModelID == "" {
			return errors.New("providerId 与 modelId 必须同时填写")
		}
		if p.SendToCloud != "configured_only" {
			return errors.New("OCR 策略无效")
		}
	}
	return nil
}

func RoutingRevision(r Routing) string {
	return PolicyRevision(EffectivePolicy(r))
}

func defaultRouting() Routing {
	p := DefaultPolicy()
	return Routing{Policy: p, Revision: PolicyRevision(p)}
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

func (s *FileStore) GetScoped(scopeKind, scopeID string) (ScopedRouting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.readLocked()
	if err != nil {
		return ScopedRouting{}, err
	}
	return scopedFrom(cur, scopeKind, scopeID), nil
}

func scopedFrom(cur Routing, scopeKind, scopeID string) ScopedRouting {
	user := EffectivePolicy(cur)
	userRev := cur.Revision
	if userRev == "" {
		userRev = PolicyRevision(user)
	}
	if scopeKind != "project" {
		return ScopedRouting{
			Policy: user, Revision: userRev, Inherited: false,
			SourceKind: "user", PackRoot: cur.PackRoot,
		}
	}
	if rec, ok := cur.ProjectPolicies[scopeID]; ok && strings.TrimSpace(rec.Policy.Mode) != "" {
		rev := rec.Revision
		if rev == "" {
			rev = PolicyRevision(rec.Policy)
		}
		return ScopedRouting{
			Policy: rec.Policy, Revision: rev, Inherited: false,
			SourceKind: "project", SourceID: scopeID, PackRoot: cur.ProjectLegacy[scopeID],
		}
	}
	return ScopedRouting{
		Policy: user, Revision: InheritedRevision("project", scopeID, "user", "", userRev),
		Inherited: true, SourceKind: "user", PackRoot: cur.ProjectLegacy[scopeID],
	}
}

func (s *FileStore) readLocked() (Routing, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultRouting(), nil
		}
		return Routing{}, err
	}
	var r Routing
	if err := json.Unmarshal(raw, &r); err != nil {
		return Routing{}, err
	}
	if strings.TrimSpace(r.Policy.Mode) == "" {
		r.Policy = EffectivePolicy(r)
	}
	if r.Revision == "" {
		r.Revision = PolicyRevision(r.Policy)
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
	if next.LocalEngine == "ppocr" {
		return cur, ErrLegacyEngineUnwired
	}
	if err := prepareUserWrite(&next, cur); err != nil {
		return cur, err
	}
	if err := s.writeLocked(next); err != nil {
		return cur, err
	}
	return next, nil
}

func (s *FileStore) CompareAndSetScoped(scopeKind, scopeID string, policy Policy, packRoot, localEngine, expected string) (ScopedRouting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.readLocked()
	if err != nil {
		return ScopedRouting{}, err
	}
	if localEngine == "ppocr" {
		return scopedFrom(cur, scopeKind, scopeID), ErrLegacyEngineUnwired
	}
	if err := ValidatePolicy(policy); err != nil {
		return scopedFrom(cur, scopeKind, scopeID), err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if scopeKind != "project" {
		if expected == "" || cur.Revision != expected {
			return scopedFrom(cur, scopeKind, scopeID), ErrRevisionConflict
		}
		cur.Policy = policy
		cur.ProviderID = policy.ProviderID
		cur.ModelID = policy.ModelID
		cur.PreferProvider = policy.Mode == "provider_first" || policy.SendToCloud == "configured_only"
		if localEngine != "" {
			cur.LocalEngine = localEngine
		}
		if packRoot != "" {
			cur.PackRoot = packRoot
		}
		cur.UpdatedAt = now
		cur.Revision = PolicyRevision(policy)
		if err := s.writeLocked(cur); err != nil {
			return ScopedRouting{}, err
		}
		return scopedFrom(cur, scopeKind, scopeID), nil
	}
	current := scopedFrom(cur, scopeKind, scopeID)
	if expected == "" || current.Revision != expected {
		return current, ErrRevisionConflict
	}
	if current.Inherited {
		// Re-check that the project row is still absent in the same write.
		if _, exists := cur.ProjectPolicies[scopeID]; exists {
			return scopedFrom(cur, scopeKind, scopeID), ErrRevisionConflict
		}
	}
	if cur.ProjectPolicies == nil {
		cur.ProjectPolicies = map[string]PolicyRecord{}
	}
	cur.ProjectPolicies[scopeID] = PolicyRecord{Policy: policy, Revision: PolicyRevision(policy), UpdatedAt: now}
	if packRoot != "" {
		if cur.ProjectLegacy == nil {
			cur.ProjectLegacy = map[string]string{}
		}
		cur.ProjectLegacy[scopeID] = packRoot
	}
	if err := s.writeLocked(cur); err != nil {
		return ScopedRouting{}, err
	}
	return scopedFrom(cur, scopeKind, scopeID), nil
}

func prepareUserWrite(next *Routing, cur Routing) error {
	if (next.ProviderID == "") != (next.ModelID == "") {
		return errors.New("providerId 与 modelId 必须同时填写")
	}
	if next.LocalEngine == "" {
		next.LocalEngine = cur.LocalEngine
	}
	if next.LocalEngine != "" && next.LocalEngine != "windows-ocr" && next.LocalEngine != "auto" {
		return errors.New("本机 OCR 引擎无效")
	}
	if strings.TrimSpace(next.Policy.Mode) == "" {
		next.Policy = EffectivePolicy(*next)
	} else {
		next.ProviderID = firstNonEmpty(next.Policy.ProviderID, next.ProviderID)
		next.ModelID = firstNonEmpty(next.Policy.ModelID, next.ModelID)
		next.Policy.ProviderID = next.ProviderID
		next.Policy.ModelID = next.ModelID
	}
	if err := ValidatePolicy(next.Policy); err != nil {
		return err
	}
	next.PreferProvider = next.Policy.Mode == "provider_first" || next.Policy.SendToCloud == "configured_only"
	next.ProjectPolicies = cur.ProjectPolicies
	if next.ProjectLegacy == nil {
		next.ProjectLegacy = cur.ProjectLegacy
	}
	if next.PackRoot == "" {
		next.PackRoot = cur.PackRoot
	}
	next.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	next.Revision = PolicyRevision(next.Policy)
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (s *FileStore) writeLocked(next Routing) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	body, err := json.Marshal(next)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, body, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
