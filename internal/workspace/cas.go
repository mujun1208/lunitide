// M5 T-5.2.4 CAS patch store: content-addressed blobs keyed by hex sha256,
// sharded one directory deep. Patches are immutable so a hit never writes.
package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var (
	ErrCASRef   = errors.New("workspace: bad CAS ref")
	ErrCASMiss  = errors.New("workspace: CAS blob missing")
	casRefRe    = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// MaxCASBlobBytes caps one stored patch (aligned with the 4 GiB hard quota
// far above any single file an M5 change should touch).
const MaxCASBlobBytes = 1 << 30

type CASStore struct{ Root string }

func NewCASStore(root string) (*CASStore, error) {
	if root == "" {
		return nil, ErrCASRef
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &CASStore{Root: root}, nil
}

func validRef(ref string) bool { return casRefRe.MatchString(ref) }

func (c *CASStore) path(ref string) string {
	return filepath.Join(c.Root, ref[:2], ref)
}

// Put stores data and returns its sha256 hex ref. Existing blobs are reused.
func (c *CASStore) Put(data []byte) (string, error) {
	if len(data) > MaxCASBlobBytes {
		return "", ErrCASRef
	}
	sum := sha256.Sum256(data)
	ref := hex.EncodeToString(sum[:])
	p := c.path(ref)
	if _, err := os.Stat(p); err == nil {
		return ref, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".cas-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if err = os.Rename(name, p); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return ref, nil
}

// Get loads a blob by ref.
func (c *CASStore) Get(ref string) ([]byte, error) {
	if !validRef(ref) {
		return nil, fmt.Errorf("%w: %q", ErrCASRef, ref)
	}
	b, err := os.ReadFile(c.path(ref))
	if os.IsNotExist(err) {
		return nil, ErrCASMiss
	}
	return b, err
}

// Has reports whether a ref is stored.
func (c *CASStore) Has(ref string) bool {
	return validRef(ref) && func() bool {
		_, err := os.Stat(c.path(ref))
		return err == nil
	}()
}
