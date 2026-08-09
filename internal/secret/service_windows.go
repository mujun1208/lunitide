//go:build windows

package secret

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/lunitide/lunitide/internal/datadir"
	"golang.org/x/sys/windows"
)

const maxSecretSize = 60 << 10

type DPAPIService struct{ root *datadir.SecureRoot }

func NewDPAPIService(root *datadir.SecureRoot) (*DPAPIService, error) {
	if root == nil || root.Path() == "" {
		return nil, errors.New("secure root is required")
	}
	return &DPAPIService{root: root}, nil
}

func (s *DPAPIService) Put(ctx context.Context, ref Ref, plaintext []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ref, err := ref.Validate()
	if err != nil {
		return err
	}
	if len(plaintext) == 0 || len(plaintext) > maxSecretSize {
		return errors.New("invalid secret size")
	}
	entropy := entropyFor(ref)
	defer Zero(entropy)
	ciphertext, err := protect(plaintext, entropy)
	if err != nil {
		return errors.New("credential protection failed")
	}
	defer Zero(ciphertext)
	name := assetName(ref)
	path, err := s.root.FilePath(name)
	if err != nil {
		return err
	}
	// Never replace an object that fails the regular-file/link/security checks.
	if err := s.root.ProtectRegularFile(name); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.root.Path(), ".secret-*")
	if err != nil {
		return errors.New("credential write failed")
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(ciphertext)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return errors.New("credential write failed")
	}
	tmpBase := filepath.Base(tmpName)
	if err := s.root.ProtectRegularFile(tmpBase); err != nil {
		return err
	}
	from, _ := windows.UTF16PtrFromString(tmpName)
	to, _ := windows.UTF16PtrFromString(path)
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return errors.New("credential commit failed")
	}
	if err := s.root.ProtectRegularFile(name); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func (s *DPAPIService) WithSecret(ctx context.Context, ref Ref, callback func([]byte) error) error {
	if callback == nil {
		return errors.New("secret callback is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ref, err := ref.Validate()
	if err != nil {
		return err
	}
	name := assetName(ref)
	if err := s.root.ProtectRegularFile(name); err != nil {
		return err
	}
	path, err := s.root.FilePath(name)
	if err != nil {
		return err
	}
	ciphertext, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return os.ErrNotExist
		}
		return errors.New("credential read failed")
	}
	defer Zero(ciphertext)
	if len(ciphertext) == 0 || len(ciphertext) > maxSecretSize+4096 {
		return errors.New("invalid credential asset")
	}
	entropy := entropyFor(ref)
	defer Zero(entropy)
	plaintext, err := unprotect(ciphertext, entropy)
	if err != nil {
		return errors.New("credential unavailable")
	}
	defer Zero(plaintext)
	return callback(plaintext)
}

func (s *DPAPIService) Delete(ctx context.Context, ref Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ref, err := ref.Validate()
	if err != nil {
		return err
	}
	name := assetName(ref)
	if err := s.root.ProtectRegularFile(name); err != nil {
		return err
	}
	path, err := s.root.FilePath(name)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func assetName(ref Ref) string {
	entropy := entropyFor(ref)
	defer Zero(entropy)
	sum := sha256.Sum256(entropy)
	return fmt.Sprintf("credential-%x.dpapi", sum[:])
}
func entropyFor(ref Ref) []byte {
	fields := []string{"lunitide-secret-v1", ref.CredentialRef, ref.ProviderID, ref.Origin, ref.Protocol}
	h := sha256.New()
	var size [4]byte
	for _, field := range fields {
		binary.BigEndian.PutUint32(size[:], uint32(len(field)))
		h.Write(size[:])
		h.Write([]byte(field))
	}
	return h.Sum(nil)
}

type dataBlob struct {
	size uint32
	data *byte
}

var (
	crypt32            = windows.NewLazySystemDLL("crypt32.dll")
	cryptProtectData   = crypt32.NewProc("CryptProtectData")
	cryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	advapi32           = windows.NewLazySystemDLL("advapi32.dll")
	// RtlSecureZeroMemory is exported by advapi32 as SystemFunction040.
	rtlSecureZeroMemory = advapi32.NewProc("SystemFunction040")
	secureZeroNative    = func(data *byte, size uintptr) {
		_, _, _ = rtlSecureZeroMemory.Call(uintptr(unsafe.Pointer(data)), size)
	}
	localFreeNative = func(data *byte) { _, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(data))) }
)

func blob(value []byte) dataBlob {
	if len(value) == 0 {
		return dataBlob{}
	}
	return dataBlob{uint32(len(value)), &value[0]}
}
func protect(input, entropy []byte) ([]byte, error) {
	ciphertext, err := crypt(true, input, entropy)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, entropy)
	_, _ = mac.Write(ciphertext)
	asset := append(mac.Sum(nil), ciphertext...)
	Zero(ciphertext)
	return asset, nil
}
func unprotect(input, entropy []byte) ([]byte, error) {
	if len(input) <= sha256.Size {
		return nil, errors.New("invalid protected asset")
	}
	tag, ciphertext := input[:sha256.Size], input[sha256.Size:]
	mac := hmac.New(sha256.New, entropy)
	_, _ = mac.Write(ciphertext)
	if !hmac.Equal(tag, mac.Sum(nil)) {
		return nil, errors.New("credential binding mismatch")
	}
	return crypt(false, ciphertext, entropy)
}
func crypt(protecting bool, input, entropy []byte) ([]byte, error) {
	in, ent, out := blob(input), blob(entropy), dataBlob{}
	proc := cryptUnprotectData
	if protecting {
		proc = cryptProtectData
	}
	var r uintptr
	if protecting {
		r, _, _ = proc.Call(uintptr(unsafe.Pointer(&in)), 0, uintptr(unsafe.Pointer(&ent)), 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	} else {
		r, _, _ = proc.Call(uintptr(unsafe.Pointer(&in)), 0, uintptr(unsafe.Pointer(&ent)), 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	}
	if r == 0 {
		return nil, syscall.GetLastError()
	}
	defer releaseNativeBlob(out)
	result := make([]byte, out.size)
	copy(result, unsafe.Slice(out.data, out.size))
	return result, nil
}

func releaseNativeBlob(out dataBlob) {
	if out.data == nil {
		return
	}
	secureZeroNative(out.data, uintptr(out.size))
	localFreeNative(out.data)
}
