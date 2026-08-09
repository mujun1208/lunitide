//go:build windows

// Package datadir owns resolution and protection of Lunitide's production data root.
package datadir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

const directoryName = "Lunitide"

// SecureRoot pins the verified data directory against rename for its lifetime.
type SecureRoot struct {
	noCopy noCopy
	state  *secureRootState
}

type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

type secureRootState struct {
	path   string
	handle atomic.Uintptr
	user   *windows.SID
	mu     sync.RWMutex
}

func (r *SecureRoot) liveState() (*secureRootState, error) {
	if r == nil || r.state == nil || windows.Handle(r.state.handle.Load()) == windows.InvalidHandle {
		return nil, fmt.Errorf("secure data root is closed")
	}
	return r.state, nil
}
func (r *SecureRoot) Path() string {
	s, err := r.liveState()
	if err != nil {
		return ""
	}
	return s.path
}
func (r *SecureRoot) Close() error {
	if r == nil || r.state == nil {
		return nil
	}
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	h := windows.Handle(r.state.handle.Swap(uintptr(windows.InvalidHandle)))
	if h == windows.InvalidHandle {
		return nil
	}
	return windows.CloseHandle(h)
}

// FilePath accepts a single ordinary filename, never a path supplied to SQLite.
func (r *SecureRoot) FilePath(name string) (string, error) {
	s, err := r.liveState()
	if err != nil {
		return "", err
	}
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\:`) {
		return "", fmt.Errorf("unsafe data filename %q", name)
	}
	return filepath.Join(s.path, name), nil
}

// PrepareSubdirectory creates, protects, verifies, and independently pins one
// ordinary directory directly below this secure root. The caller must Close the
// returned root after the consumer of the directory has stopped.
func (r *SecureRoot) PrepareSubdirectory(name string) (*SecureRoot, error) {
	if r == nil || r.state == nil {
		return nil, fmt.Errorf("secure data root is closed")
	}
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()
	s, err := r.liveState()
	if err != nil {
		return nil, err
	}
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\:`) {
		return nil, fmt.Errorf("unsafe data directory name %q", name)
	}
	return prepare(s.path, filepath.Join(s.path, name))
}

// ProtectRegularFile verifies a database or sidecar through a no-reparse handle,
// then applies and verifies its owner and protected DACL. Missing files are OK.
func (r *SecureRoot) ProtectRegularFile(name string) error {
	if r == nil || r.state == nil {
		return fmt.Errorf("secure data root is closed")
	}
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()
	s, err := r.liveState()
	if err != nil {
		return err
	}
	path, err := r.FilePath(name)
	if err != nil {
		return err
	}
	h, err := openNoReparse(path, false, windows.READ_CONTROL|windows.WRITE_DAC|windows.WRITE_OWNER)
	if err == windows.ERROR_FILE_NOT_FOUND {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open protected file %s: %w", name, err)
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return err
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return fmt.Errorf("%s is not a regular non-reparse file", name)
	}
	if info.NumberOfLinks != 1 {
		return fmt.Errorf("%s has %d hard links; exactly one required", name, info.NumberOfLinks)
	}
	if err := setAndVerifySecurity(h, s.user, false); err != nil {
		return fmt.Errorf("secure %s: %w", name, err)
	}
	var after windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &after); err != nil {
		return err
	}
	if after.VolumeSerialNumber != info.VolumeSerialNumber || after.FileIndexHigh != info.FileIndexHigh || after.FileIndexLow != info.FileIndexLow || after.NumberOfLinks != 1 || after.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return fmt.Errorf("%s identity, attributes, or link count changed while securing", name)
	}
	if _, err := r.liveState(); err != nil {
		return err
	}
	return nil
}

// PrepareProduction resolves LocalAppData via Known Folder and pins the result.
func PrepareProduction() (*SecureRoot, error) {
	known, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		return nil, fmt.Errorf("resolve LocalAppData known folder: %w", err)
	}
	if known == "" || !filepath.IsAbs(known) {
		return nil, fmt.Errorf("invalid LocalAppData path %q", known)
	}
	return prepare(known, filepath.Join(known, directoryName))
}

func prepare(known, path string) (*SecureRoot, error) {
	if err := verifySegmentsNoReparse(known, path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	if err := verifySegmentsNoReparse(known, path); err != nil {
		return nil, err
	}
	userToken, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("current user SID: %w", err)
	}
	h, err := openNoReparse(path, true, windows.READ_CONTROL|windows.WRITE_DAC|windows.WRITE_OWNER)
	if err != nil {
		return nil, fmt.Errorf("pin data root: %w", err)
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("inspect data root: %w", err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("data root is not an ordinary directory")
	}
	if err := setAndVerifySecurity(h, userToken.User.Sid, true); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	var after windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &after); err != nil {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("reinspect data root: %w", err)
	}
	if after.VolumeSerialNumber != info.VolumeSerialNumber || after.FileIndexHigh != info.FileIndexHigh || after.FileIndexLow != info.FileIndexLow || after.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || after.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("data root identity or attributes changed while securing")
	}
	final, err := finalPath(h)
	if err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	knownFinal, err := finalPathForDirectory(known)
	if err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	if !within(knownFinal, final) {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("data root escaped LocalAppData: %q", final)
	}
	state := &secureRootState{path: path, user: userToken.User.Sid}
	state.handle.Store(uintptr(h))
	return &SecureRoot{state: state}, nil
}

func verifySegmentsNoReparse(base, target string) error {
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, `..\`) {
		return fmt.Errorf("data root outside known folder")
	}
	current := filepath.Clean(base)
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		h, err := openNoReparse(current, true, windows.FILE_READ_ATTRIBUTES)
		if err != nil {
			return err
		}
		var info windows.ByHandleFileInformation
		err = windows.GetFileInformationByHandle(h, &info)
		windows.CloseHandle(h)
		if err != nil {
			return err
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fmt.Errorf("reparse point rejected: %s", current)
		}
	}
	return nil
}

func openNoReparse(path string, directory bool, access uint32) (windows.Handle, error) {
	flags := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if directory {
		flags |= windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	// Deliberately omit FILE_SHARE_DELETE: open objects cannot be renamed away.
	return windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, flags, 0)
}

func setAndVerifySecurity(h windows.Handle, user *windows.SID, directory bool) error {
	flags := ""
	if directory {
		flags = "OICI"
	}
	sddl := "O:" + user.String() + "D:P(A;" + flags + ";FA;;;" + user.String() + ")(A;" + flags + ";FA;;;SY)"
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if err := windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, user, nil, dacl, nil); err != nil {
		return err
	}
	got, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := got.Owner()
	if err != nil || !windows.EqualSid(owner, user) {
		return fmt.Errorf("unexpected owner")
	}
	control, _, err := got.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("DACL is not protected")
	}
	acl, _, err := got.DACL()
	if err != nil || acl == nil || acl.AceCount != 2 {
		return fmt.Errorf("DACL must contain exactly user and SYSTEM ACEs")
	}
	system, _ := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	wantFlags := uint8(0)
	if directory {
		wantFlags = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	}
	seenUser, seenSystem := false, false
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			return err
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		// SDDL's FA is expanded to FILE_ALL_ACCESS rather than left as GENERIC_ALL.
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags != wantFlags || ace.Mask != windows.ACCESS_MASK(0x001f01ff) {
			return fmt.Errorf("unexpected ACE permissions or inheritance")
		}
		if windows.EqualSid(sid, user) {
			seenUser = true
		} else if windows.EqualSid(sid, system) {
			seenSystem = true
		} else {
			return fmt.Errorf("unexpected ACE principal")
		}
	}
	if !seenUser || !seenSystem {
		return fmt.Errorf("missing user or SYSTEM ACE")
	}
	return nil
}

func finalPathForDirectory(path string) (string, error) {
	h, err := openNoReparse(path, true, windows.FILE_READ_ATTRIBUTES)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	return finalPath(h)
}
func finalPath(h windows.Handle) (string, error) {
	b := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &b[0], uint32(len(b)), 0)
		if err != nil {
			return "", err
		}
		if n < uint32(len(b)) {
			return filepath.Clean(windows.UTF16ToString(b[:n])), nil
		}
		b = make([]uint16, n+1)
	}
}
func within(base, child string) bool {
	b := strings.TrimPrefix(filepath.Clean(base), `\\?\`)
	c := strings.TrimPrefix(filepath.Clean(child), `\\?\`)
	rel, err := filepath.Rel(b, c)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, `..\`)
}

// PrepareForTest pins an explicit root while still verifying every segment.
func PrepareForTest(path string) (*SecureRoot, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("test data directory must be absolute")
	}
	parent := filepath.Dir(filepath.Clean(path))
	return prepare(parent, filepath.Clean(path))
}
