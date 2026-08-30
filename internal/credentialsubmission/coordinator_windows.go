//go:build windows

// Package credentialsubmission implements the Host-only, crash-safe handoff of credentials.
package credentialsubmission

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/datadir"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/oklog/ulid/v2"
	"golang.org/x/sys/windows"
)

const (
	journalName       = "credential-submissions.json"
	lockName          = "credential-submissions.lock"
	MaxCredentialSize = 60 << 10
	MaxTTL            = 5 * time.Minute
	maxJournalSize    = 1 << 20
	maxJournalEntries = 4096
)

var (
	ErrConflict = errors.New("credential submission request conflict")
	ErrExpired  = errors.New("credential submission expired")
	ErrNotFound = errors.New("credential submission not found")
	ErrState    = errors.New("credential submission state conflict")
	ErrBusy     = errors.New("credential submission busy")
	ErrPoisoned = errors.New("credential submission coordinator persistence is uncertain")
	ErrLocked   = errors.New("credential submission journal is owned by another coordinator")
)

type Scope struct{ ProviderID, DraftFingerprint string }

func Existing(id string) Scope { return Scope{ProviderID: id} }
func Draft(fp string) Scope    { return Scope{DraftFingerprint: fp} }

type SubmitInput struct {
	Scope    Scope
	Protocol provider.Protocol
	Origin   string
	// Request is the canonical Host request representation. The coordinator hashes it;
	// callers cannot provide an authorization hash.
	Request []byte
	// RequestHash is retained for internal legacy tests. New Host code must set Request.
	RequestHash string
	Credential  []byte
	TTL         time.Duration
}
type Submission struct {
	SubmissionID string    `json:"submissionId"`
	ProviderID   string    `json:"providerId"`
	ExpiresAt    time.Time `json:"expires"`
}
type Reservation struct {
	Submission
	Ref secret.Ref
}

type State string

const (
	StatePutting  State = "putting"
	StateReady    State = "ready"
	StateReserved State = "reserved"
	StateAdopted  State = "adopted"
	StateConsumed State = "consumed"
)

type journalEntry struct {
	SubmissionID  string     `json:"submissionId"`
	ProviderID    string     `json:"providerId"`
	Ref           secret.Ref `json:"ref"`
	RequestDigest string     `json:"requestDigest"`
	State         State      `json:"state"`
	ExpiresAt     time.Time  `json:"expiry"`
}
type journal struct {
	Version int            `json:"version"`
	Entries []journalEntry `json:"entries"`
}

// ProviderResolver is authoritative for Existing scope; untrusted submit fields are ignored.
type ProviderResolver interface {
	ResolveProvider(context.Context, string) (provider.Provider, error)
}

// ReferenceResolver answers from the same authoritative database that adopts refs.
type ReferenceResolver interface {
	IsCredentialReferenceAdopted(context.Context, secret.Ref) (bool, error)
}

type SaveResult uint8

const (
	NotCommitted SaveResult = iota
	Committed
	Unknown
)

type saveHook func([]byte) (SaveResult, error)

type Coordinator struct {
	mu           sync.Mutex
	root         *datadir.SecureRoot
	secrets      secret.Service
	references   ReferenceResolver
	providers    ProviderResolver
	now          func() time.Time
	entries      map[string]journalEntry
	lock         windows.Handle
	poisoned     bool
	saveOverride saveHook
}

func New(root *datadir.SecureRoot, secrets secret.Service, references ReferenceResolver, dependencies ...any) (*Coordinator, error) {
	if root == nil || root.Path() == "" || secrets == nil || references == nil {
		return nil, errors.New("secure root, secret service, and authoritative reference resolver are required")
	}
	var providers ProviderResolver
	for _, dependency := range dependencies {
		if v, ok := dependency.(ProviderResolver); ok {
			providers = v
		}
	}
	c := &Coordinator{root: root, secrets: secrets, references: references, providers: providers, now: time.Now, entries: map[string]journalEntry{}, lock: windows.InvalidHandle}
	if err := c.acquireLock(); err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = c.Close()
		}
	}()
	if err := c.loadLocked(); err != nil {
		return nil, err
	}
	if err := c.Recover(context.Background()); err != nil {
		return nil, err
	}
	ok = true
	return c, nil
}
func (c *Coordinator) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lock == windows.InvalidHandle {
		return nil
	}
	h := c.lock
	c.lock = windows.InvalidHandle
	return windows.CloseHandle(h)
}
func (c *Coordinator) acquireLock() error {
	if err := c.root.ProtectRegularFile(lockName); err != nil {
		return err
	}
	p, err := c.root.FilePath(lockName)
	if err != nil {
		return err
	}
	wp, _ := windows.UTF16PtrFromString(p)
	h, err := windows.CreateFile(wp, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrLocked, err)
	}
	c.lock = h
	if err = c.root.ProtectRegularFile(lockName); err != nil {
		windows.CloseHandle(h)
		c.lock = windows.InvalidHandle
		return err
	}
	return nil
}

func requestDigest(request []byte) (string, error) {
	if len(request) == 0 || len(request) > maxJournalSize {
		return "", errors.New("invalid canonical request")
	}
	h := sha256.Sum256(request)
	return hex.EncodeToString(h[:]), nil
}

func (c *Coordinator) Submit(ctx context.Context, in SubmitInput) (Submission, error) {
	defer secret.Zero(in.Credential)
	if err := ctx.Err(); err != nil {
		return Submission{}, err
	}
	if len(in.Credential) == 0 || len(in.Credential) > MaxCredentialSize {
		return Submission{}, errors.New("invalid credential size")
	}
	digest, err := requestDigest(in.Request)
	if len(in.Request) == 0 && validDigest(in.RequestHash) {
		digest = strings.ToLower(in.RequestHash)
		err = nil
	}
	if err != nil {
		return Submission{}, err
	}
	origin := in.Origin
	protocol := in.Protocol
	providerID := ""
	if (in.Scope.ProviderID == "") == (in.Scope.DraftFingerprint == "") {
		return Submission{}, errors.New("exactly one scope is required")
	}
	if in.Scope.ProviderID != "" {
		if c.providers == nil {
			return Submission{}, errors.New("existing scope requires provider resolver")
		}
		p, e := c.providers.ResolveProvider(ctx, in.Scope.ProviderID)
		if e != nil {
			return Submission{}, e
		}
		providerID = p.ID
		protocol, origin, e = existingUpdateTarget(in.Request, p)
		if e != nil || protocol != in.Protocol || origin != in.Origin {
			return Submission{}, errors.New("existing update target mismatch")
		}
	} else {
		if !validProtocol(protocol) {
			return Submission{}, errors.New("invalid protocol")
		}
		normalized, e := provider.NormalizeOrigin(origin)
		if e != nil || normalized != origin {
			return Submission{}, errors.New("origin is not canonical")
		}
		if len(in.Request) != 0 {
			boundProtocol, boundOrigin, e := draftCreateTarget(in.Request)
			if e != nil || boundProtocol != protocol || boundOrigin != origin {
				return Submission{}, errors.New("draft create target mismatch")
			}
		}
		fp, _ := provider.OriginFingerprint(protocol, origin)
		if !sameDigest(fp, in.Scope.DraftFingerprint) {
			return Submission{}, errors.New("draft fingerprint mismatch")
		}
		providerID, err = newULID()
		if err != nil {
			return Submission{}, err
		}
	}
	if _, err = ulid.ParseStrict(providerID); err != nil {
		return Submission{}, errors.New("invalid provider ID")
	}
	sid, err := newULID()
	if err != nil {
		return Submission{}, err
	}
	rid, err := newULID()
	if err != nil {
		return Submission{}, err
	}
	ttl := in.TTL
	if ttl <= 0 {
		ttl = MaxTTL
	}
	if ttl > MaxTTL {
		return Submission{}, errors.New("submission TTL exceeds five minutes")
	}
	e := journalEntry{SubmissionID: sid, ProviderID: providerID, Ref: secret.Ref{CredentialRef: rid, ProviderID: providerID, Origin: origin, Protocol: string(protocol)}, RequestDigest: digest, State: StatePutting, ExpiresAt: c.now().UTC().Add(ttl)}
	c.mu.Lock()
	if err = c.usableLocked(); err == nil {
		if len(c.entries) >= maxJournalEntries {
			err = ErrBusy
		} else {
			c.entries[sid] = e
			err = c.persistMutationLocked(func() { delete(c.entries, sid) })
		}
	}
	c.mu.Unlock()
	if err != nil {
		return Submission{}, err
	}
	if err = c.secrets.Put(ctx, e.Ref, in.Credential); err != nil {
		_ = c.cleanup(context.Background(), sid)
		return Submission{}, err
	}
	c.mu.Lock()
	if err = c.usableLocked(); err == nil {
		cur := c.entries[sid]
		old := cur
		cur.State = StateReady
		c.entries[sid] = cur
		err = c.persistMutationLocked(func() { c.entries[sid] = old })
		e = cur
	}
	c.mu.Unlock()
	if err != nil {
		return Submission{}, err
	}
	return public(e), nil
}

func draftCreateTarget(request []byte) (provider.Protocol, string, error) {
	var p struct {
		Protocol provider.Protocol `json:"protocol"`
		BaseURL  string            `json:"baseUrl"`
	}
	if json.Unmarshal(request, &p) != nil || !validProtocol(p.Protocol) {
		return "", "", errors.New("invalid bound create")
	}
	origin, err := provider.NormalizeOrigin(p.BaseURL)
	return p.Protocol, origin, err
}

func existingUpdateTarget(request []byte, current provider.Provider) (provider.Protocol, string, error) {
	var p struct {
		ID              string             `json:"id"`
		Protocol        *provider.Protocol `json:"protocol"`
		BaseURL         *string            `json:"baseUrl"`
		ExpectedVersion int64              `json:"expectedVersion"`
	}
	if json.Unmarshal(request, &p) != nil || p.ID != current.ID || p.ExpectedVersion < 1 {
		return "", "", errors.New("invalid bound update")
	}
	protocol := current.Protocol
	if p.Protocol != nil {
		protocol = *p.Protocol
	}
	if !validProtocol(protocol) {
		return "", "", errors.New("invalid target protocol")
	}
	baseURL := current.BaseURL
	if p.BaseURL != nil {
		baseURL = *p.BaseURL
	}
	origin, err := provider.NormalizeOrigin(baseURL)
	return protocol, origin, err
}

func (c *Coordinator) Reserve(ctx context.Context, id string, request any) (Reservation, error) {
	return c.transition(ctx, id, request, StateReady, StateReserved)
}
func (c *Coordinator) Adopt(ctx context.Context, id string, request any) (Reservation, error) {
	return c.transition(ctx, id, request, StateReserved, StateAdopted)
}
func (c *Coordinator) Consume(ctx context.Context, id string, request any) (Reservation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.usableLocked(); err != nil {
		return Reservation{}, err
	}
	e, ok := c.entries[id]
	if !ok {
		return Reservation{}, ErrNotFound
	}
	if err := matchRequest(e, request); err != nil {
		return Reservation{}, err
	}
	if !c.now().Before(e.ExpiresAt) {
		return Reservation{}, ErrExpired
	}
	if e.State == StateConsumed {
		return reservation(e), nil
	}
	if e.State != StateAdopted {
		return Reservation{}, ErrState
	}
	old := e
	e.State = StateConsumed
	c.entries[id] = e
	if err := c.persistMutationLocked(func() { c.entries[id] = old }); err != nil {
		return Reservation{}, err
	}
	return reservation(e), nil
}
func (c *Coordinator) transition(ctx context.Context, id string, request any, from, to State) (Reservation, error) {
	if err := ctx.Err(); err != nil {
		return Reservation{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.usableLocked(); err != nil {
		return Reservation{}, err
	}
	e, ok := c.entries[id]
	if !ok {
		return Reservation{}, ErrNotFound
	}
	if err := matchRequest(e, request); err != nil {
		return Reservation{}, err
	}
	if e.State == StatePutting {
		return Reservation{}, ErrBusy
	}
	if !c.now().Before(e.ExpiresAt) {
		return Reservation{}, ErrExpired
	}
	if e.State == to {
		return reservation(e), nil
	}
	if e.State != from {
		return Reservation{}, ErrState
	}
	old := e
	e.State = to
	c.entries[id] = e
	if err := c.persistMutationLocked(func() { c.entries[id] = old }); err != nil {
		return Reservation{}, err
	}
	return reservation(e), nil
}
func matchRequest(e journalEntry, r any) error {
	var d string
	var err error
	switch value := r.(type) {
	case []byte:
		d, err = requestDigest(value)
	case string:
		if validDigest(value) {
			d = strings.ToLower(value)
		} else {
			err = ErrConflict
		}
	default:
		err = ErrConflict
	}
	if err != nil || !sameDigest(e.RequestDigest, d) {
		return ErrConflict
	}
	return nil
}

func (c *Coordinator) Recover(ctx context.Context) error        { return c.reconcile(ctx, false) }
func (c *Coordinator) CleanupExpired(ctx context.Context) error { return c.reconcile(ctx, true) }
func (c *Coordinator) reconcile(ctx context.Context, expiredOnly bool) error {
	c.mu.Lock()
	if err := c.usableLocked(); err != nil {
		c.mu.Unlock()
		return err
	}
	ids := make([]string, 0, len(c.entries))
	for id, e := range c.entries {
		if !expiredOnly || !c.now().Before(e.ExpiresAt) {
			ids = append(ids, id)
		}
	}
	c.mu.Unlock()
	for _, id := range ids {
		if err := c.cleanup(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
func (c *Coordinator) cleanup(ctx context.Context, id string) error {
	c.mu.Lock()
	if err := c.usableLocked(); err != nil {
		c.mu.Unlock()
		return err
	}
	e, ok := c.entries[id]
	if ok && (e.State == StateAdopted || e.State == StateConsumed) {
		if !c.now().Before(e.ExpiresAt) {
			delete(c.entries, id)
			err := c.persistMutationLocked(func() { c.entries[id] = e })
			c.mu.Unlock()
			return err
		}
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	if !ok {
		return nil
	}
	referenced, err := c.references.IsCredentialReferenceAdopted(ctx, e.Ref)
	if err != nil {
		return fmt.Errorf("reference integrity unknown: %w", err)
	}
	if referenced {
		c.mu.Lock()
		cur, ok := c.entries[id]
		if ok && !c.now().Before(cur.ExpiresAt) {
			delete(c.entries, id)
			err = c.persistMutationLocked(func() { c.entries[id] = cur })
		} else if ok && cur.State != StateConsumed {
			old := cur
			cur.State = StateAdopted
			c.entries[id] = cur
			err = c.persistMutationLocked(func() { c.entries[id] = old })
		}
		c.mu.Unlock()
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cur, ok := c.entries[id]
	if !ok || cur.State == StateAdopted || cur.State == StateConsumed {
		return nil
	}
	if err = c.secrets.Delete(ctx, cur.Ref); err != nil {
		return err
	}
	delete(c.entries, id)
	return c.persistMutationLocked(func() { c.entries[id] = cur })
}

func (c *Coordinator) usableLocked() error {
	if c.poisoned {
		return ErrPoisoned
	}
	if c.lock == windows.InvalidHandle {
		return ErrLocked
	}
	return nil
}
func (c *Coordinator) persistMutationLocked(rollback func()) error {
	result, err := c.saveLocked()
	switch result {
	case NotCommitted:
		rollback()
		return err
	case Committed:
		return err
	default:
		c.poisoned = true
		c.entries = map[string]journalEntry{}
		loadErr := c.loadLocked()
		if loadErr != nil {
			return fmt.Errorf("%w: %v; reload: %v", ErrPoisoned, err, loadErr)
		}
		return fmt.Errorf("%w: %v", ErrPoisoned, err)
	}
}
func (c *Coordinator) saveLocked() (SaveResult, error) {
	if len(c.entries) > maxJournalEntries {
		return NotCommitted, errors.New("journal exceeds entry limit")
	}
	j := journal{Version: 1, Entries: make([]journalEntry, 0, len(c.entries))}
	for _, e := range c.entries {
		j.Entries = append(j.Entries, e)
	}
	b, err := json.Marshal(j)
	if err != nil {
		return NotCommitted, err
	}
	defer secret.Zero(b)
	if len(b) > maxJournalSize {
		return NotCommitted, errors.New("journal exceeds size limit")
	}
	if c.saveOverride != nil {
		return c.saveOverride(b)
	}
	path, err := c.root.FilePath(journalName)
	if err != nil {
		return NotCommitted, err
	}
	tmp, err := os.CreateTemp(c.root.Path(), ".submission-*")
	if err != nil {
		return NotCommitted, errors.New("journal write failed")
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(b)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if ce := tmp.Close(); err == nil {
		err = ce
	}
	if err != nil {
		return NotCommitted, errors.New("journal write failed")
	}
	if err = c.root.ProtectRegularFile(filepath.Base(name)); err != nil {
		return NotCommitted, err
	}
	from, _ := windows.UTF16PtrFromString(name)
	to, _ := windows.UTF16PtrFromString(path)
	if err = windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return Unknown, fmt.Errorf("journal commit failed: %w", err)
	}
	if err = c.root.ProtectRegularFile(journalName); err != nil {
		return Committed, err
	}
	return Committed, nil
}
func (c *Coordinator) loadLocked() error {
	if err := c.root.ProtectRegularFile(journalName); err != nil {
		return err
	}
	p, err := c.root.FilePath(journalName)
	if err != nil {
		return err
	}
	f, err := os.Open(p)
	if os.IsNotExist(err) {
		c.entries = map[string]journalEntry{}
		return nil
	}
	if err != nil {
		return errors.New("journal read failed")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxJournalSize+1))
	if err != nil || len(b) > maxJournalSize {
		return errors.New("journal size/integrity invalid")
	}
	defer secret.Zero(b)
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var j journal
	if err = dec.Decode(&j); err != nil {
		return errors.New("invalid journal")
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		return errors.New("journal has trailing data")
	}
	if j.Version != 1 || len(j.Entries) > maxJournalEntries {
		return errors.New("invalid journal version/count")
	}
	m := map[string]journalEntry{}
	refs := map[string]bool{}
	for _, e := range j.Entries {
		if err = validateEntry(e, c.now()); err != nil {
			return err
		}
		if _, ok := m[e.SubmissionID]; ok || refs[e.Ref.CredentialRef] {
			return errors.New("duplicate submission/reference")
		}
		m[e.SubmissionID] = e
		refs[e.Ref.CredentialRef] = true
	}
	c.entries = m
	return nil
}
func validateEntry(e journalEntry, now time.Time) error {
	canonical := func(s string) bool { id, err := ulid.ParseStrict(s); return err == nil && id.String() == s }
	if !canonical(e.SubmissionID) || !canonical(e.ProviderID) || !canonical(e.Ref.CredentialRef) || !canonical(e.Ref.ProviderID) {
		return errors.New("journal contains invalid ULID")
	}
	if e.Ref.ProviderID != e.ProviderID || !validProtocol(provider.Protocol(e.Ref.Protocol)) {
		return errors.New("journal reference mismatch")
	}
	o, err := provider.NormalizeOrigin(e.Ref.Origin)
	if err != nil || o != e.Ref.Origin {
		return errors.New("journal origin is not canonical")
	}
	if !validDigest(e.RequestDigest) {
		return errors.New("journal digest invalid")
	}
	switch e.State {
	case StatePutting, StateReady, StateReserved, StateAdopted, StateConsumed:
	default:
		return errors.New("journal state invalid")
	}
	if e.ExpiresAt.Location() != time.UTC || e.ExpiresAt.IsZero() || e.ExpiresAt.After(now.UTC().Add(MaxTTL+time.Minute)) {
		return errors.New("journal expiry invalid")
	}
	return nil
}
func validProtocol(p provider.Protocol) bool {
	return provider.ValidProtocol(p)
}
func validDigest(v string) bool {
	b, e := hex.DecodeString(v)
	return e == nil && len(b) == sha256.Size && v == strings.ToLower(v)
}
func sameDigest(a, b string) bool {
	aa, ea := hex.DecodeString(a)
	bb, eb := hex.DecodeString(b)
	return ea == nil && eb == nil && len(aa) == 32 && len(bb) == 32 && subtle.ConstantTimeCompare(aa, bb) == 1
}
func newULID() (string, error) {
	id, e := ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
	return id.String(), e
}
func public(e journalEntry) Submission       { return Submission{e.SubmissionID, e.ProviderID, e.ExpiresAt} }
func reservation(e journalEntry) Reservation { return Reservation{public(e), e.Ref} }
