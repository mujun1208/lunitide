package producthub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	AdminUsername    = "mujun"
	initialPassword  = "1234567890"
	bcryptCost       = 10
	refreshCooldown  = 30 * time.Second
)

var (
	ErrAuthDenied     = errors.New("PH_012")
	ErrBadCredentials = errors.New("PH_013")
	ErrRateLimited    = errors.New("PH_005")
	ErrNotImplemented = errors.New("PH_006")
	ErrNotFound       = errors.New("PH_018")
)

type gate struct {
	mu       sync.Mutex
	tokens   map[string]time.Time
	lastGen  time.Time
}

func newGate() *gate {
	return &gate{tokens: map[string]time.Time{}}
}

func hashPassword(plain string) (string, error) {
	raw, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (s *Service) ensureAuth(ctx context.Context) error {
	if s.persist == nil {
		return nil
	}
	hash, err := s.persist.ProductHubLoadAuth(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(hash) != "" {
		return nil
	}
	next, err := hashPassword(initialPassword)
	if err != nil {
		return err
	}
	return s.persist.ProductHubSaveAuth(ctx, next)
}

func (s *Service) Unlock(ctx context.Context, username, password string) (string, error) {
	if err := s.ensureAuth(ctx); err != nil {
		return "", err
	}
	if strings.TrimSpace(username) != AdminUsername {
		return "", ErrBadCredentials
	}
	hash, err := s.persist.ProductHubLoadAuth(ctx)
	if err != nil {
		return "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return "", ErrBadCredentials
	}
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf[:])
	s.gate.mu.Lock()
	s.gate.tokens[token] = time.Now()
	s.gate.mu.Unlock()
	return token, nil
}

func (s *Service) ChangePassword(ctx context.Context, token, current, next string) error {
	if !s.Check(token) {
		return ErrAuthDenied
	}
	if strings.TrimSpace(next) == "" || len([]rune(next)) > 128 {
		return ErrBadCredentials
	}
	hash, err := s.persist.ProductHubLoadAuth(ctx)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(current)) != nil {
		return ErrBadCredentials
	}
	fresh, err := hashPassword(next)
	if err != nil {
		return err
	}
	return s.persist.ProductHubSaveAuth(ctx, fresh)
}

func (s *Service) Check(token string) bool {
	if s == nil || strings.TrimSpace(token) == "" {
		return false
	}
	s.gate.mu.Lock()
	defer s.gate.mu.Unlock()
	_, ok := s.gate.tokens[token]
	return ok
}

func (s *Service) touchRefresh() error {
	s.gate.mu.Lock()
	defer s.gate.mu.Unlock()
	if !s.gate.lastGen.IsZero() && time.Since(s.gate.lastGen) < refreshCooldown {
		return ErrRateLimited
	}
	s.gate.lastGen = time.Now()
	return nil
}
