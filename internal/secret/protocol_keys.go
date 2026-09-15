package secret

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/oklog/ulid/v2"
)

// ProtocolKeyService is the CONTRACTS §4 protocol DEK port.
// Empty keyID selects the current key. A missing named key is not created.
type ProtocolKeyService interface {
	EnsureProtocolKey(context.Context) (string, error)
	WithProtocolKey(context.Context, string, func([]byte) error) error
}

const protocolDEKBytes = 32

type memoryProtocolKeys struct {
	mu      sync.Mutex
	current string
	keys    map[string][]byte
}

// NewTestProtocolKeys injects a temporary 32-byte DEK for isolated tests.
// It does not open a live user DPAPI login.
func NewTestProtocolKeys(keyID string, dek []byte) ProtocolKeyService {
	if keyID == "" {
		keyID = "test-protocol-key"
	}
	cp := append([]byte(nil), dek...)
	return &memoryProtocolKeys{current: keyID, keys: map[string][]byte{keyID: cp}}
}

func (m *memoryProtocolKeys) EnsureProtocolKey(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != "" {
		if _, ok := m.keys[m.current]; ok {
			return m.current, nil
		}
	}
	id := ulid.Make().String()
	dek := make([]byte, protocolDEKBytes)
	if _, err := rand.Read(dek); err != nil {
		return "", err
	}
	if m.keys == nil {
		m.keys = map[string][]byte{}
	}
	m.keys[id] = dek
	m.current = id
	return id, nil
}

func (m *memoryProtocolKeys) WithProtocolKey(ctx context.Context, keyID string, fn func([]byte) error) error {
	if fn == nil {
		return errors.New("protocol key callback is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	id := keyID
	if id == "" {
		id = m.current
	}
	dek, ok := m.keys[id]
	m.mu.Unlock()
	if !ok {
		return errors.New("protocol key not found")
	}
	cp := append([]byte(nil), dek...)
	defer Zero(cp)
	return fn(cp)
}

type serviceProtocolKeys struct {
	store Service
}

// NewProtocolKeyService wraps secret.Service (production DPAPI Put/WithSecret).
// The DEK file is written before callers persist ciphertext.
func NewProtocolKeyService(store Service) ProtocolKeyService {
	return &serviceProtocolKeys{store: store}
}

func protocolKeyRef(keyID string) Ref {
	return Ref{
		CredentialRef: "protocol-dek/" + keyID,
		ProviderID:    "lunitide",
		Origin:        "https://lunitide.local",
		Protocol:      "protocol_dek",
	}
}

func protocolCurrentRef() Ref {
	return Ref{
		CredentialRef: "protocol-dek-current",
		ProviderID:    "lunitide",
		Origin:        "https://lunitide.local",
		Protocol:      "protocol_dek",
	}
}

func (s *serviceProtocolKeys) EnsureProtocolKey(ctx context.Context) (string, error) {
	if s == nil || s.store == nil {
		return "", errors.New("protocol key store is required")
	}
	var current string
	err := s.store.WithSecret(ctx, protocolCurrentRef(), func(id []byte) error {
		current = string(id)
		return nil
	})
	if err == nil && current != "" {
		return current, nil
	}
	id := ulid.Make().String()
	dek := make([]byte, protocolDEKBytes)
	if _, err = rand.Read(dek); err != nil {
		return "", err
	}
	defer Zero(dek)
	if err = s.store.Put(ctx, protocolKeyRef(id), dek); err != nil {
		return "", err
	}
	if err = s.store.Put(ctx, protocolCurrentRef(), []byte(id)); err != nil {
		return "", err
	}
	return id, nil
}

type packedProtocolKeys struct {
	Current string            `json:"current"`
	Keys    map[string][]byte `json:"keys"`
}

// PackTestProtocolKeys wraps DEKs into an opaque sidecar. Callers must keep
// this blob off the backup JSON manifest (IDs only).
func PackTestProtocolKeys(ctx context.Context, keys ProtocolKeyService, ids []string) ([]byte, error) {
	if keys == nil {
		return nil, errors.New("protocol key service is required")
	}
	pack := packedProtocolKeys{Keys: map[string][]byte{}}
	for i, id := range ids {
		if err := keys.WithProtocolKey(ctx, id, func(dek []byte) error {
			pack.Keys[id] = append([]byte(nil), dek...)
			if i == 0 || pack.Current == "" {
				pack.Current = id
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	plain, err := json.Marshal(pack)
	if err != nil {
		return nil, err
	}
	defer Zero(plain)
	wrap := make([]byte, protocolDEKBytes)
	if _, err = rand.Read(wrap); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(wrap)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := aead.Seal(nil, nonce, plain, []byte("protocol-key-metadata"))
	out := make([]byte, 0, 1+len(wrap)+len(nonce)+len(sealed))
	out = append(out, 1)
	out = append(out, wrap...)
	out = append(out, nonce...)
	out = append(out, sealed...)
	return out, nil
}

// UnpackTestProtocolKeys restores a test ProtocolKeyService from the sidecar.
func UnpackTestProtocolKeys(blob []byte) (ProtocolKeyService, error) {
	if len(blob) < 1+protocolDEKBytes+12 {
		return nil, errors.New("invalid protocol key metadata")
	}
	if blob[0] != 1 {
		return nil, fmt.Errorf("unsupported protocol key metadata version %d", blob[0])
	}
	wrap := blob[1 : 1+protocolDEKBytes]
	rest := blob[1+protocolDEKBytes:]
	block, err := aes.NewCipher(wrap)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(rest) < aead.NonceSize() {
		return nil, errors.New("invalid protocol key metadata")
	}
	nonce, sealed := rest[:aead.NonceSize()], rest[aead.NonceSize():]
	plain, err := aead.Open(nil, nonce, sealed, []byte("protocol-key-metadata"))
	if err != nil {
		return nil, err
	}
	defer Zero(plain)
	var pack packedProtocolKeys
	if err = json.Unmarshal(plain, &pack); err != nil {
		return nil, err
	}
	if pack.Keys == nil {
		pack.Keys = map[string][]byte{}
	}
	return &memoryProtocolKeys{current: pack.Current, keys: pack.Keys}, nil
}

func (s *serviceProtocolKeys) WithProtocolKey(ctx context.Context, keyID string, fn func([]byte) error) error {
	if fn == nil {
		return errors.New("protocol key callback is required")
	}
	if s == nil || s.store == nil {
		return errors.New("protocol key store is required")
	}
	if keyID == "" {
		err := s.store.WithSecret(ctx, protocolCurrentRef(), func(id []byte) error {
			keyID = string(id)
			return nil
		})
		if err != nil {
			return err
		}
	}
	return s.store.WithSecret(ctx, protocolKeyRef(keyID), fn)
}
