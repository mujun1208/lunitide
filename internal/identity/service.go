// Package identity is the this-PC person archive: a stable ULID subject,
// Ed25519 keypair, nickname/org fields, optional start password, and a
// LAN pairing PIN. It is not org.member and never addresses another PC.
package identity

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/bcrypt"
)

const (
	DefaultNickname = "月汐用户"
	legacySubject   = "local-user"
	bcryptCost      = 10
	maxNickname     = 64
	maxAvatar       = 65536
	maxDept         = 128
	maxBio          = 2000
)

var (
	ErrLocked          = errors.New("identity locked")
	ErrPassword        = errors.New("identity password mismatch")
	ErrUnavailable     = errors.New("identity unavailable")
	ErrInvalidProfile  = errors.New("identity profile invalid")
	ErrPasswordTooLong = errors.New("identity password too long")
)

type Status string

const (
	StatusOnline    Status = "online"
	StatusAway      Status = "away"
	StatusBusy      Status = "busy"
	StatusInvisible Status = "invisible"
)

type Record struct {
	SubjectID          string
	PublicKey          string
	PrivateKey         string
	Nickname           string
	Avatar             string
	Status             Status
	Department         string
	Title              string
	OrgName            string
	Bio                string
	PasswordHash       string
	PairingCode        string
	DiscoveryEnabled   bool
	CreatedAt          string
	UpdatedAt          string
}

type Public struct {
	SubjectID        string `json:"subjectId"`
	Nickname         string `json:"nickname"`
	Avatar           string `json:"avatar"`
	Status           Status `json:"status"`
	Department       string `json:"department"`
	Title            string `json:"title"`
	OrgName          string `json:"orgName"`
	Bio              string `json:"bio"`
	PublicKey        string `json:"publicKey"`
	PairingCode      string `json:"pairingCode"`
	PasswordSet      bool   `json:"passwordSet"`
	Locked           bool   `json:"locked"`
	DiscoveryEnabled bool   `json:"discoveryEnabled"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

type ProfilePatch struct {
	Nickname   *string
	Avatar     *string
	Status     *Status
	Department *string
	Title      *string
	OrgName    *string
	Bio        *string
}

type Store interface {
	LoadIdentity(ctx context.Context) (Record, bool, error)
	InsertIdentity(ctx context.Context, rec Record) error
	UpdateIdentity(ctx context.Context, rec Record) error
	RebindLegacySubject(ctx context.Context, from, to string) error
	UpsertSelfContact(ctx context.Context, rec Record) error
}

type Service struct {
	store    Store
	mu       sync.Mutex
	rec      Record
	unlocked bool
}

func New(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Ensure(ctx context.Context) error {
	if s == nil || s.store == nil {
		return ErrUnavailable
	}
	rec, ok, err := s.store.LoadIdentity(ctx)
	if err != nil {
		return err
	}
	if !ok {
		rec, err = newRecord()
		if err != nil {
			return err
		}
		if err := s.store.InsertIdentity(ctx, rec); err != nil {
			return err
		}
		if err := s.store.RebindLegacySubject(ctx, legacySubject, rec.SubjectID); err != nil {
			return err
		}
	}
	if err := s.store.UpsertSelfContact(ctx, rec); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rec = rec
	s.unlocked = rec.PasswordHash == ""
	return nil
}

func (s *Service) SubjectID() string {
	if s == nil {
		return legacySubject
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec.SubjectID == "" {
		return legacySubject
	}
	return s.rec.SubjectID
}

func (s *Service) Locked() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.unlocked
}

func (s *Service) Public() Public {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.publicLocked()
}

func (s *Service) publicLocked() Public {
	return Public{
		SubjectID:        s.rec.SubjectID,
		Nickname:         s.rec.Nickname,
		Avatar:           s.rec.Avatar,
		Status:           s.rec.Status,
		Department:       s.rec.Department,
		Title:            s.rec.Title,
		OrgName:          s.rec.OrgName,
		Bio:              s.rec.Bio,
		PublicKey:        s.rec.PublicKey,
		PairingCode:      s.rec.PairingCode,
		PasswordSet:      s.rec.PasswordHash != "",
		Locked:           !s.unlocked,
		DiscoveryEnabled: s.rec.DiscoveryEnabled,
		CreatedAt:        s.rec.CreatedAt,
		UpdatedAt:        s.rec.UpdatedAt,
	}
}

func (s *Service) Snapshot() Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rec
}

func (s *Service) Sign(message []byte) ([]byte, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := hex.DecodeString(s.rec.PrivateKey)
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, ErrUnavailable
	}
	return ed25519.Sign(ed25519.PrivateKey(raw), message), nil
}

func Verify(publicKeyHex string, message, sig []byte) bool {
	raw, err := hex.DecodeString(strings.ToLower(strings.TrimSpace(publicKeyHex)))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(raw), message, sig)
}

func (s *Service) PairingHash() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return PairingHash(s.rec.PairingCode, s.rec.SubjectID)
}

func (s *Service) Update(ctx context.Context, patch ProfilePatch) (Public, error) {
	if s == nil || s.store == nil {
		return Public{}, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.unlocked {
		return Public{}, ErrLocked
	}
	next := s.rec
	next.UpdatedAt = nowRFC3339()
	if patch.Nickname != nil {
		name := strings.TrimSpace(*patch.Nickname)
		if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > maxNickname {
			return Public{}, ErrInvalidProfile
		}
		next.Nickname = name
	}
	if patch.Avatar != nil {
		if len(*patch.Avatar) > maxAvatar {
			return Public{}, ErrInvalidProfile
		}
		next.Avatar = *patch.Avatar
	}
	if patch.Status != nil {
		if !validStatus(*patch.Status) {
			return Public{}, ErrInvalidProfile
		}
		next.Status = *patch.Status
	}
	if patch.Department != nil {
		if utf8.RuneCountInString(*patch.Department) > maxDept {
			return Public{}, ErrInvalidProfile
		}
		next.Department = *patch.Department
	}
	if patch.Title != nil {
		if utf8.RuneCountInString(*patch.Title) > maxDept {
			return Public{}, ErrInvalidProfile
		}
		next.Title = *patch.Title
	}
	if patch.OrgName != nil {
		if utf8.RuneCountInString(*patch.OrgName) > maxDept {
			return Public{}, ErrInvalidProfile
		}
		next.OrgName = *patch.OrgName
	}
	if patch.Bio != nil {
		if utf8.RuneCountInString(*patch.Bio) > maxBio {
			return Public{}, ErrInvalidProfile
		}
		next.Bio = *patch.Bio
	}
	if err := s.store.UpdateIdentity(ctx, next); err != nil {
		return Public{}, err
	}
	if err := s.store.UpsertSelfContact(ctx, next); err != nil {
		return Public{}, err
	}
	s.rec = next
	return s.publicLocked(), nil
}

func (s *Service) SetPassword(ctx context.Context, password, current string) (Public, error) {
	if s == nil || s.store == nil {
		return Public{}, ErrUnavailable
	}
	if utf8.RuneCountInString(password) > 128 {
		return Public{}, ErrPasswordTooLong
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.unlocked {
		return Public{}, ErrLocked
	}
	if s.rec.PasswordHash != "" {
		if bcrypt.CompareHashAndPassword([]byte(s.rec.PasswordHash), []byte(current)) != nil {
			return Public{}, ErrPassword
		}
	}
	next := s.rec
	next.UpdatedAt = nowRFC3339()
	if password == "" {
		next.PasswordHash = ""
	} else {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
		if err != nil {
			return Public{}, err
		}
		next.PasswordHash = string(hash)
	}
	if err := s.store.UpdateIdentity(ctx, next); err != nil {
		return Public{}, err
	}
	s.rec = next
	s.unlocked = true
	return s.publicLocked(), nil
}

func (s *Service) Unlock(password string) (Public, error) {
	if s == nil {
		return Public{}, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec.PasswordHash == "" {
		s.unlocked = true
		return s.publicLocked(), nil
	}
	if bcrypt.CompareHashAndPassword([]byte(s.rec.PasswordHash), []byte(password)) != nil {
		return Public{}, ErrPassword
	}
	s.unlocked = true
	return s.publicLocked(), nil
}

func (s *Service) SetDiscovery(ctx context.Context, enabled bool) (Public, error) {
	if s == nil || s.store == nil {
		return Public{}, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.unlocked {
		return Public{}, ErrLocked
	}
	next := s.rec
	next.DiscoveryEnabled = enabled
	next.UpdatedAt = nowRFC3339()
	if err := s.store.UpdateIdentity(ctx, next); err != nil {
		return Public{}, err
	}
	s.rec = next
	return s.publicLocked(), nil
}

func (s *Service) RotatePairingCode(ctx context.Context) (Public, error) {
	if s == nil || s.store == nil {
		return Public{}, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.unlocked {
		return Public{}, ErrLocked
	}
	code, err := randomDigits(6)
	if err != nil {
		return Public{}, err
	}
	next := s.rec
	next.PairingCode = code
	next.UpdatedAt = nowRFC3339()
	if err := s.store.UpdateIdentity(ctx, next); err != nil {
		return Public{}, err
	}
	if err := s.store.UpsertSelfContact(ctx, next); err != nil {
		return Public{}, err
	}
	s.rec = next
	return s.publicLocked(), nil
}

func PairingHash(code, subjectID string) string {
	sum := sha256.Sum256([]byte(code + ":" + subjectID))
	return hex.EncodeToString(sum[:])
}

func validStatus(status Status) bool {
	switch status {
	case StatusOnline, StatusAway, StatusBusy, StatusInvisible:
		return true
	default:
		return false
	}
}

func newRecord() (Record, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Record{}, fmt.Errorf("generate identity key: %w", err)
	}
	code, err := randomDigits(6)
	if err != nil {
		return Record{}, err
	}
	now := nowRFC3339()
	return Record{
		SubjectID:        ulid.Make().String(),
		PublicKey:        hex.EncodeToString(pub),
		PrivateKey:       hex.EncodeToString(priv),
		Nickname:         DefaultNickname,
		Status:           StatusOnline,
		PairingCode:      code,
		DiscoveryEnabled: false,
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func randomDigits(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i, b := range buf {
		out[i] = '0' + (b % 10)
	}
	return string(out), nil
}
