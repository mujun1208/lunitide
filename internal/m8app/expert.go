// M8 FR-19 application service (T-8.10.x): expert.create / list / detail /
// update / toggle / archive / mount / mounting.get.
//
// The expert center is not a second executor: dispatch reuses the M7
// subagent.spawn read-only path, so this service only owns the catalog,
// the append-only version chain, the per-phase mountings and the nine-
// phase default suggestions. Create and update run the six-section
// validation chain (frontmatter schema -> bounds -> injection scan ->
// digest); any failure is M8-042 with zero persistence. Update takes the
// expectedVersionId optimistic lock (M8-043); active mountings keep the
// pinned old version until an explicit updateVersion. Toggle answers
// affected mountings without deleting them (dispatch skips disabled
// experts with an M8-047 hint). Archive demands the expertId-bound
// confirmation token, refuses builtin experts and refuses while mounted
// mountings exist (M8-048). Mount enforces enabled-and-not-archived
// (M8-045) and the per-phase <=4 cap (M8-044, trigger-backed).
package m8app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// M8 FR-19 error family (04 错误矩阵 M8-042~048).
var (
	// ErrExpertSixSectionInvalid: the six-section validation chain failed
	// (M8-042, 422, zero persistence).
	ErrExpertSixSectionInvalid = errors.New("m8app: expert six-section invalid")
	// ErrExpertVersionConflict: the expectedVersionId optimistic lock
	// missed (M8-043, 409).
	ErrExpertVersionConflict = errors.New("m8app: expert version conflict")
	// ErrMountLimitExceeded: the (project, phase) already mounts 4 experts
	// (M8-044, 422, trigger-backed).
	ErrMountLimitExceeded = errors.New("m8app: expert mount limit exceeded")
	// ErrExpertNotMountable: the mount targets a disabled or archived
	// expert (M8-045, 409).
	ErrExpertNotMountable = errors.New("m8app: expert not mountable")
	// ErrExpertArchiveMounted: the archive found mounted mountings
	// (M8-048, 409) - unmount everything first.
	ErrExpertArchiveMounted = errors.New("m8app: expert archive blocked by mounted mountings")
	// ErrExpertNotFound: the expert (or version/mounting) row is missing.
	ErrExpertNotFound = errors.New("m8app: expert not found")
	// ErrExpertStateInvalid: the transition is off the allowed path
	// (e.g. toggling an archived expert).
	ErrExpertStateInvalid = errors.New("m8app: expert state transition invalid")
	// ErrExpertBuiltinProtected: builtin experts refuse archive (disable
	// is allowed).
	ErrExpertBuiltinProtected = errors.New("m8app: builtin expert archive forbidden")
	// ErrExpertDuplicate: the UNIQUE(subject_id, name) row already exists.
	ErrExpertDuplicate = errors.New("m8app: expert name already exists")
)

// ExpertTx is the FR-19 single-writer transaction: expert tables plus the
// shared audit ledger only.
type ExpertTx interface {
	GetExpert(expertID string) (m8core.ExpertCatalog, error)
	GetExpertByName(subjectID, name string) (m8core.ExpertCatalog, bool, error)
	PutExpert(m8core.ExpertCatalog) error
	GetVersion(versionID string) (m8core.ExpertVersion, error)
	GetVersionBySemver(expertID, semver string) (m8core.ExpertVersion, bool, error)
	PutVersion(m8core.ExpertVersion) error
	ListVersions(expertID string) ([]m8core.ExpertVersion, error)
	ListExperts(filter ExpertFilter) ([]ExpertListItem, error)
	GetMounting(projectID, phaseKey, expertID string) (m8core.ExpertMounting, bool, error)
	PutMounting(m8core.ExpertMounting) error
	ListMountingsByExpert(expertID string) ([]m8core.ExpertMounting, error)
	ListMountingsByProjectPhase(projectID, phaseKey string) ([]ExpertMountingView, error)
	CountMountedInPhase(projectID, phaseKey string) (int, error)
	AppendAuditEvent(audit.Event) (audit.Event, error)
}

// ExpertUnitOfWork is the FR-19 single-writer boundary.
type ExpertUnitOfWork interface {
	TransactExpert(ctx context.Context, fn func(ExpertTx) error) error
}

// ExpertService implements the FR-19 use cases.
// ExpertSkillStore persists per-expert skill bindings (skills hang on the
// expert, not the conversation composer).
type ExpertSkillStore interface {
	ListExpertSkillKeys(ctx context.Context, expertID string) ([]string, error)
	ReplaceExpertSkillKeys(ctx context.Context, expertID string, keys []string) error
	SeedExpertSkillsIfEmpty(ctx context.Context, expertID string, keys []string) error
	MergeExpertSkillKeys(ctx context.Context, expertID string, keys []string) error
}

type ExpertService struct {
	uow     ExpertUnitOfWork
	clock   Clock
	subject string
	persona PersonaBodyStore
	skills  ExpertSkillStore
}

// NewExpertService wires the FR-19 service. A nil persona store keeps
// create/update working but detail answers the digest projection only.
func NewExpertService(uow ExpertUnitOfWork, localSubject string, persona PersonaBodyStore) *ExpertService {
	return &ExpertService{uow: uow, clock: systemClock{}, subject: localSubject, persona: persona}
}

// SetClock substitutes the clock (tests).
func (s *ExpertService) SetClock(c Clock) { s.clock = c }

// SetPersonaStore substitutes the persona body store (tests / on-disk
// wiring).
func (s *ExpertService) SetPersonaStore(p PersonaBodyStore) { s.persona = p }

// SetSkillStore wires the optional expert skill binder.
func (s *ExpertService) SetSkillStore(store ExpertSkillStore) { s.skills = store }

func (s *ExpertService) storeBody(ref string, body []byte) {
	if s.persona != nil {
		_ = s.persona.Put(ref, body)
	}
}

func (s *ExpertService) loadBody(ref string) ([]byte, bool) {
	if s.persona == nil {
		return nil, false
	}
	b, ok, err := s.persona.Get(ref)
	if err != nil {
		return nil, false
	}
	return b, ok
}

// CreateInput is the expert.create command.
type CreateInput struct {
	Source        string
	Frontmatter   m8core.Frontmatter
	SixSection    m8core.SixSection
	RequestID     string
	Actor         string
	SkillKeys     []string
	CatalogItemID string
}

// CreateResult is the expert.create outcome.
type CreateResult struct {
	ExpertID         string `json:"expertId"`
	VersionID        string `json:"versionId"`
	State            string `json:"state"`
	SixSectionDigest string `json:"sixSectionDigest"`
}

// Create enacts the local six-section creation chain: frontmatter schema,
// section bounds, injection scan, digest - any failure is M8-042 with
// zero persistence. The persona body lands in the read-only store keyed
// by persona_ref; the version row is append-only from birth.
func (s *ExpertService) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	if s == nil || s.uow == nil {
		return CreateResult{}, ErrServiceUnavailable
	}
	if in.Source != m8core.ExpertSourceLocal {
		return CreateResult{}, ErrExpertSixSectionInvalid
	}
	if err := in.Frontmatter.Validate(); err != nil {
		return CreateResult{}, fmt.Errorf("%w: %v", ErrExpertSixSectionInvalid, err)
	}
	if err := m8core.ValidateSixSection(in.SixSection); err != nil {
		return CreateResult{}, fmt.Errorf("%w: %v", ErrExpertSixSectionInvalid, err)
	}
	if len(in.RequestID) < 1 || len(in.RequestID) > 128 {
		return CreateResult{}, ErrPayloadInvalid
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	expertID := ulid.Make().String()
	versionID := ulid.Make().String()
	personaRef := in.Frontmatter.PersonaRef(in.SixSection)
	digest := in.SixSection.SixSectionDigest()
	var out CreateResult
	err := s.uow.TransactExpert(ctx, func(tx ExpertTx) error {
		if _, has, err := tx.GetExpertByName(s.subject, in.Frontmatter.Name); err != nil {
			return err
		} else if has {
			return ErrExpertDuplicate
		}
		e := m8core.ExpertCatalog{
			ExpertID: expertID, SubjectID: s.subject, Name: in.Frontmatter.Name,
			Division: in.Frontmatter.Division, Source: in.Source,
			CatalogItemID:    strings.TrimSpace(in.CatalogItemID),
			CurrentVersionID: versionID, State: m8core.ExpertEnabled,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.PutExpert(e); err != nil {
			return err
		}
		v := m8core.ExpertVersion{
			VersionID: versionID, ExpertID: expertID, Semver: in.Frontmatter.Semver,
			PersonaRef: personaRef, SixSectionDigest: digest,
			ChangeNote: "initial version", CreatedAt: now,
		}
		if err := tx.PutVersion(v); err != nil {
			return err
		}
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "expert.create",
			ResourceType: "expert", ResourceID: expertID,
			Actor: actorOr(in.Actor), CorrelationID: in.RequestID,
			AfterDigest: digest, CreatedAt: now,
		}); err != nil {
			return err
		}
		out = CreateResult{ExpertID: expertID, VersionID: versionID, State: m8core.ExpertEnabled, SixSectionDigest: digest}
		return nil
	})
	if err != nil {
		return out, err
	}
	s.storeBody(personaRef, []byte(in.SixSection.CanonicalJSON()))
	if len(in.SkillKeys) > 0 {
		if s.skills == nil {
			return out, ErrServiceUnavailable
		}
		if err := s.skills.ReplaceExpertSkillKeys(ctx, out.ExpertID, in.SkillKeys); err != nil {
			return out, mapSkillBindError(err)
		}
	}
	return out, nil
}

// ExpertFilter carries the expert.list filters.
type ExpertFilter struct {
	Division  string
	Source    string
	State     string
	ProjectID string
}

// ExpertListItem is one expert.list projection row.
type ExpertListItem struct {
	ExpertID          string `json:"expertId"`
	Name              string `json:"name"`
	Division          string `json:"division"`
	Source            string `json:"source"`
	Semver            string `json:"semver"`
	State             string `json:"state"`
	VersionCount      int    `json:"versionCount"`
	MountedPhaseCount int    `json:"mountedPhaseCount"`
	OriginBundleID    string `json:"originBundleId,omitempty"`
	CatalogItemID     string `json:"catalogItemId,omitempty"`
	Kind              string `json:"kind,omitempty"`
}

// ExpertListResult is the expert.list outcome.
type ExpertListResult struct {
	Experts []ExpertListItem `json:"experts"`
}

// List answers the catalog projection with optional division/source/state
// filters; projectId narrows to experts with any mounting row in that
// project.
func (s *ExpertService) List(ctx context.Context, filter ExpertFilter) (ExpertListResult, error) {
	if s == nil || s.uow == nil {
		return ExpertListResult{}, ErrServiceUnavailable
	}
	if filter.Division != "" && !m8core.ValidDivision(filter.Division) {
		return ExpertListResult{}, ErrPayloadInvalid
	}
	if filter.Source != "" && filter.Source != m8core.ExpertSourcePack &&
		filter.Source != m8core.ExpertSourceLocal && filter.Source != m8core.ExpertSourceBuiltin {
		return ExpertListResult{}, ErrPayloadInvalid
	}
	if filter.State != "" && filter.State != m8core.ExpertEnabled &&
		filter.State != m8core.ExpertDisabled && filter.State != m8core.ExpertArchived {
		return ExpertListResult{}, ErrPayloadInvalid
	}
	var out ExpertListResult
	err := s.uow.TransactExpert(ctx, func(tx ExpertTx) error {
		items, err := tx.ListExperts(filter)
		if err != nil {
			return err
		}
		for i := range items {
			items[i].Kind = ExpertKindForExpert(items[i].Name, items[i].CatalogItemID)
		}
		out.Experts = items
		return nil
	})
	return out, err
}

// DetailInput is the expert.detail command.
type DetailInput struct {
	ExpertID  string
	VersionID string
}

// DetailResult is the expert.detail outcome.
type DetailResult struct {
	Expert     map[string]any      `json:"expert"`
	SixSection json.RawMessage     `json:"sixSection"`
	Versions   []ExpertVersionView `json:"versions"`
	Mountings  []ExpertMountingRef `json:"mountings"`
}

// ExpertVersionView is one version chain row.
type ExpertVersionView struct {
	VersionID        string `json:"versionId"`
	Semver           string `json:"semver"`
	ChangeNote       string `json:"changeNote"`
	SixSectionDigest string `json:"sixSectionDigest"`
	CreatedAt        string `json:"createdAt"`
}

// ExpertMountingRef is one mounting projection of the expert.
type ExpertMountingRef struct {
	ProjectID string `json:"projectId"`
	PhaseKey  string `json:"phaseKey"`
	State     string `json:"state"`
}

// Detail answers the expert projection: catalog row, the six-section body
// of the requested version (default current), the append-only version
// chain and every mounting.
func (s *ExpertService) Detail(ctx context.Context, in DetailInput) (DetailResult, error) {
	if s == nil || s.uow == nil {
		return DetailResult{}, ErrServiceUnavailable
	}
	if len(in.ExpertID) != 26 {
		return DetailResult{}, ErrPayloadInvalid
	}
	if in.VersionID != "" && len(in.VersionID) != 26 {
		return DetailResult{}, ErrPayloadInvalid
	}
	var out DetailResult
	err := s.uow.TransactExpert(ctx, func(tx ExpertTx) error {
		e, err := tx.GetExpert(in.ExpertID)
		if errors.Is(err, m8core.ErrNotFound) {
			return ErrExpertNotFound
		}
		if err != nil {
			return err
		}
		versionID := in.VersionID
		if versionID == "" {
			versionID = e.CurrentVersionID
		}
		v, err := tx.GetVersion(versionID)
		if errors.Is(err, m8core.ErrNotFound) {
			return ErrExpertNotFound
		}
		if err != nil {
			return err
		}
		cur, err := tx.GetVersion(e.CurrentVersionID)
		if err != nil {
			return err
		}
		versions, err := tx.ListVersions(in.ExpertID)
		if err != nil {
			return err
		}
		mountings, err := tx.ListMountingsByExpert(in.ExpertID)
		if err != nil {
			return err
		}
		out.Expert = map[string]any{
			"expertId": e.ExpertID, "name": e.Name, "division": e.Division,
			"source": e.Source, "state": e.State, "semver": cur.Semver,
			"currentVersionId": e.CurrentVersionID,
			"kind":             ExpertKindForExpert(e.Name, e.CatalogItemID),
		}
		if e.OriginBundleID != "" {
			out.Expert["originBundleId"] = e.OriginBundleID
		}
		if e.CatalogItemID != "" {
			out.Expert["catalogItemId"] = e.CatalogItemID
		}
		out.SixSection = json.RawMessage(`{}`)
		if body, ok := s.loadBody(v.PersonaRef); ok {
			out.SixSection = json.RawMessage(body)
		}
		out.Versions = make([]ExpertVersionView, 0, len(versions))
		for _, ver := range versions {
			out.Versions = append(out.Versions, ExpertVersionView{
				VersionID: ver.VersionID, Semver: ver.Semver, ChangeNote: ver.ChangeNote,
				SixSectionDigest: ver.SixSectionDigest, CreatedAt: ver.CreatedAt,
			})
		}
		out.Mountings = []ExpertMountingRef{}
		for _, m := range mountings {
			out.Mountings = append(out.Mountings, ExpertMountingRef{
				ProjectID: m.ProjectID, PhaseKey: m.PhaseKey, State: m.State,
			})
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	catalogID, _ := out.Expert["catalogItemId"].(string)
	out.Expert["boundSkills"] = s.boundOrPreferred(ctx, in.ExpertID, fmt.Sprint(out.Expert["name"]), catalogID)
	return out, nil
}

func (s *ExpertService) boundOrPreferred(ctx context.Context, expertID, name, catalogItemID string) []string {
	if s != nil && s.skills != nil && expertID != "" {
		if keys, err := s.skills.ListExpertSkillKeys(ctx, expertID); err == nil {
			return keys
		}
	}
	if item, ok := ResolveConversationExpert(name, catalogItemID); ok {
		return append([]string{}, item.PreferredSkills...)
	}
	return []string{}
}

func (s *ExpertService) applySkillFloor(ctx context.Context, expertID string, keys []string) []string {
	if s == nil || s.uow == nil {
		return keys
	}
	var name, catalogID string
	err := s.uow.TransactExpert(ctx, func(tx ExpertTx) error {
		item, err := tx.GetExpert(expertID)
		if err != nil {
			return err
		}
		name = item.Name
		catalogID = item.CatalogItemID
		return nil
	})
	item, ok := ResolveConversationExpert(name, catalogID)
	if err != nil || !ok {
		return keys
	}
	floor := BindKeysFromCatalog(item)
	if len(floor) == 0 {
		return keys
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(keys)+len(floor))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	for _, key := range floor {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}

func mapSkillBindError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "invalid") || strings.Contains(msg, "capacity") {
		return ErrPayloadInvalid
	}
	return err
}

// ListBoundSkills answers stored bindings (empty if none).
func (s *ExpertService) ListBoundSkills(ctx context.Context, expertID string) ([]string, error) {
	if s == nil || s.skills == nil {
		return nil, ErrServiceUnavailable
	}
	if len(expertID) != 26 {
		return nil, ErrPayloadInvalid
	}
	return s.skills.ListExpertSkillKeys(ctx, expertID)
}

// ReplaceBoundSkills replaces the expert's skill bindings.
func (s *ExpertService) ReplaceBoundSkills(ctx context.Context, expertID string, keys []string) ([]string, error) {
	if s == nil || s.skills == nil {
		return nil, ErrServiceUnavailable
	}
	if len(expertID) != 26 {
		return nil, ErrPayloadInvalid
	}
	if keys == nil {
		keys = []string{}
	}
	keys = s.applySkillFloor(ctx, expertID, keys)
	if err := s.skills.ReplaceExpertSkillKeys(ctx, expertID, keys); err != nil {
		return nil, mapSkillBindError(err)
	}
	return s.skills.ListExpertSkillKeys(ctx, expertID)
}

// ComposeSkillsForNames unions stored bindings when present, otherwise the
// catalog preferredSkills for named conversation specialists.
func (s *ExpertService) ComposeSkillsForNames(ctx context.Context, names []string) []string {
	catalog, _, _, _ := ComposeForExpertNames(names)
	if s == nil || s.skills == nil || s.uow == nil {
		return catalog
	}
	var listed ExpertListResult
	if err := s.uow.TransactExpert(ctx, func(tx ExpertTx) error {
		items, err := tx.ListExperts(ExpertFilter{})
		if err != nil {
			return err
		}
		listed.Experts = items
		return nil
	}); err != nil {
		return catalog
	}
	byName := map[string]string{}
	byCatalog := map[string]string{}
	for _, item := range listed.Experts {
		byName[item.Name] = item.ExpertID
		if id := strings.TrimSpace(item.CatalogItemID); id != "" {
			byCatalog[id] = item.ExpertID
		}
	}
	seen := map[string]bool{}
	var out []string
	add := func(keys []string) {
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, key)
		}
	}
	usedStore := false
	for _, name := range names {
		name = strings.TrimSpace(name)
		id := byName[name]
		if id == "" {
			id = byCatalog[name]
		}
		if id == "" {
			if item, ok := ResolveConversationExpert(name, name); ok {
				add(item.PreferredSkills)
			}
			continue
		}
		keys, err := s.skills.ListExpertSkillKeys(ctx, id)
		if err != nil {
			if item, ok := ResolveConversationExpert(name, name); ok {
				add(item.PreferredSkills)
			}
			continue
		}
		usedStore = true
		add(keys)
	}
	if !usedStore && len(out) == 0 {
		return catalog
	}
	return out
}

// UpdateInput is the expert.update command.
type UpdateInput struct {
	ExpertID          string
	ExpectedVersionID string
	SixSection        map[string]string
	ChangeNote        string
	Actor             string
}

// UpdateResult is the expert.update outcome.
type UpdateResult struct {
	ExpertID  string `json:"expertId"`
	VersionID string `json:"versionId"`
	Semver    string `json:"semver"`
}

// decodeSixSection decodes the caller map onto the domain struct; unknown
// keys are ignored, all six keys must land non-empty (validated later).
func decodeSixSection(m map[string]string) (m8core.SixSection, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return m8core.SixSection{}, ErrPayloadInvalid
	}
	var out m8core.SixSection
	if err := json.Unmarshal(raw, &out); err != nil {
		return m8core.SixSection{}, ErrPayloadInvalid
	}
	return out, nil
}

// Update enacts the versioned revision: the expectedVersionId optimistic
// lock (M8-043 on drift), the six-section chain (M8-042), then one
// append-only version row with a patch-bumped semver and the atomic
// current_version_id switch. Active mountings keep the pinned old version
// until an explicit updateVersion.
func (s *ExpertService) Update(ctx context.Context, in UpdateInput) (UpdateResult, error) {
	if s == nil || s.uow == nil {
		return UpdateResult{}, ErrServiceUnavailable
	}
	if len(in.ExpertID) != 26 || len(in.ExpectedVersionID) != 26 ||
		len(in.SixSection) < 1 || len(in.ChangeNote) < 1 || len(in.ChangeNote) > 2000 {
		return UpdateResult{}, ErrPayloadInvalid
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	var out UpdateResult
	var personaRef, body string
	err := s.uow.TransactExpert(ctx, func(tx ExpertTx) error {
		e, err := tx.GetExpert(in.ExpertID)
		if errors.Is(err, m8core.ErrNotFound) {
			return ErrExpertNotFound
		}
		if err != nil {
			return err
		}
		if e.State == m8core.ExpertArchived {
			return ErrExpertStateInvalid
		}
		// M8-043: the optimistic lock check precedes content validation - a
		// stale writer learns the conflict before anything else.
		if e.CurrentVersionID != in.ExpectedVersionID {
			return ErrExpertVersionConflict
		}
		six, derr := decodeSixSection(in.SixSection)
		if derr != nil {
			return derr
		}
		if verr := m8core.ValidateSixSection(six); verr != nil {
			return fmt.Errorf("%w: %v", ErrExpertSixSectionInvalid, verr)
		}
		personaRef = m8core.Frontmatter{}.PersonaRef(six)
		digest := six.SixSectionDigest()
		body = six.CanonicalJSON()
		cur, err := tx.GetVersion(e.CurrentVersionID)
		if err != nil {
			return err
		}
		// Append-only chain: patch-bump until the semver is free.
		next := m8core.BumpPatch(cur.Semver)
		for i := 0; i < 1000; i++ {
			if _, has, err := tx.GetVersionBySemver(e.ExpertID, next); err != nil {
				return err
			} else if !has {
				break
			}
			next = m8core.BumpPatch(next)
		}
		versionID := ulid.Make().String()
		v := m8core.ExpertVersion{
			VersionID: versionID, ExpertID: e.ExpertID, Semver: next,
			PersonaRef: personaRef, SixSectionDigest: digest,
			ChangeNote: in.ChangeNote, CreatedAt: now,
		}
		if err := tx.PutVersion(v); err != nil {
			return err
		}
		e.CurrentVersionID, e.UpdatedAt = versionID, now
		if err := tx.PutExpert(e); err != nil {
			return err
		}
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "expert.update",
			ResourceType: "expert", ResourceID: e.ExpertID,
			Actor: actorOr(in.Actor), AfterDigest: digest, CreatedAt: now,
		}); err != nil {
			return err
		}
		out = UpdateResult{ExpertID: e.ExpertID, VersionID: versionID, Semver: next}
		return nil
	})
	if err != nil {
		return out, err
	}
	s.storeBody(personaRef, []byte(body))
	return out, nil
}

// ExpertToggleInput is the expert.toggle command.
type ExpertToggleInput struct {
	ExpertID string
	Enabled  bool
	Actor    string
}

// ExpertToggleResult is the expert.toggle outcome.
type ExpertToggleResult struct {
	ExpertID          string `json:"expertId"`
	State             string `json:"state"`
	AffectedMountings int    `json:"affectedMountings"`
}

// Toggle enacts enabled<->disabled. Disabling keeps every active mounting
// (they display stopped and dispatch skips them with an M8-047 hint); the
// answer counts the affected mounted phases. Archived refuses; builtin
// allows disable.
func (s *ExpertService) Toggle(ctx context.Context, in ExpertToggleInput) (ExpertToggleResult, error) {
	if s == nil || s.uow == nil {
		return ExpertToggleResult{}, ErrServiceUnavailable
	}
	if len(in.ExpertID) != 26 {
		return ExpertToggleResult{}, ErrPayloadInvalid
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	target := m8core.ExpertDisabled
	if in.Enabled {
		target = m8core.ExpertEnabled
	}
	var out ExpertToggleResult
	err := s.uow.TransactExpert(ctx, func(tx ExpertTx) error {
		e, err := tx.GetExpert(in.ExpertID)
		if errors.Is(err, m8core.ErrNotFound) {
			return ErrExpertNotFound
		}
		if err != nil {
			return err
		}
		if err := m8core.ExpertTransition(e.State, target); err != nil {
			return ErrExpertStateInvalid
		}
		mountings, err := tx.ListMountingsByExpert(e.ExpertID)
		if err != nil {
			return err
		}
		affected := 0
		for _, m := range mountings {
			if m.State == m8core.MountingMounted {
				affected++
			}
		}
		e.State, e.UpdatedAt = target, now
		if err := tx.PutExpert(e); err != nil {
			return err
		}
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "expert.toggle",
			ResourceType: "expert", ResourceID: e.ExpertID,
			Actor: actorOr(in.Actor), CreatedAt: now,
		}); err != nil {
			return err
		}
		out = ExpertToggleResult{ExpertID: e.ExpertID, State: target, AffectedMountings: affected}
		return nil
	})
	return out, err
}

// ArchiveInput is the expert.archive command.
type ArchiveInput struct {
	ExpertID     string
	ConfirmToken string
	Actor        string
}

// ArchiveResult is the expert.archive outcome.
type ArchiveResult struct {
	ExpertID         string `json:"expertId"`
	State            string `json:"state"`
	ArchivedVersions int    `json:"archivedVersions"`
}

// Archive enacts the terminal archive: the confirmation token must be the
// expertId-bound value, builtin experts refuse (disable allowed, archive
// forbidden), any mounted mounting refuses wholesale (M8-048). The
// append-only version chain stays for audit.
func (s *ExpertService) Archive(ctx context.Context, in ArchiveInput) (ArchiveResult, error) {
	if s == nil || s.uow == nil {
		return ArchiveResult{}, ErrServiceUnavailable
	}
	if len(in.ExpertID) != 26 || !m8core.ValidHexDigest(in.ConfirmToken) {
		return ArchiveResult{}, ErrPayloadInvalid
	}
	if !strings.EqualFold(in.ConfirmToken, m8core.ArchiveConfirmToken(in.ExpertID)) {
		return ArchiveResult{}, ErrPayloadInvalid
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	var out ArchiveResult
	err := s.uow.TransactExpert(ctx, func(tx ExpertTx) error {
		e, err := tx.GetExpert(in.ExpertID)
		if errors.Is(err, m8core.ErrNotFound) {
			return ErrExpertNotFound
		}
		if err != nil {
			return err
		}
		if e.Source == m8core.ExpertSourceBuiltin {
			return ErrExpertBuiltinProtected
		}
		if err := m8core.ExpertTransition(e.State, m8core.ExpertArchived); err != nil {
			return ErrExpertStateInvalid
		}
		mountings, err := tx.ListMountingsByExpert(e.ExpertID)
		if err != nil {
			return err
		}
		for _, m := range mountings {
			if m.State == m8core.MountingMounted {
				return ErrExpertArchiveMounted
			}
		}
		versions, err := tx.ListVersions(e.ExpertID)
		if err != nil {
			return err
		}
		e.State, e.UpdatedAt = m8core.ExpertArchived, now
		if err := tx.PutExpert(e); err != nil {
			return err
		}
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "expert.archive",
			ResourceType: "expert", ResourceID: e.ExpertID,
			Actor: actorOr(in.Actor), CreatedAt: now,
		}); err != nil {
			return err
		}
		out = ArchiveResult{ExpertID: e.ExpertID, State: m8core.ExpertArchived, ArchivedVersions: len(versions)}
		return nil
	})
	return out, err
}
