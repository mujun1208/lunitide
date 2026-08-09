//go:build windows

package sqlite

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func discoverElectronProviderMetadata() ([]inspectedElectronFile, error) {
	roaming, err := windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, 0)
	if err != nil {
		return nil, fmt.Errorf("resolve RoamingAppData known folder: %w", err)
	}
	if roaming == "" || !filepath.IsAbs(roaming) {
		return nil, fmt.Errorf("invalid RoamingAppData known folder")
	}
	return discoverElectronProviderMetadataAt(roaming)
}

// discoverElectronProviderMetadataAt is deliberately unexported and exists so
// Windows tests can exercise handle and reparse behavior without changing the
// production Known Folder source.
func discoverElectronProviderMetadataAt(roaming string) ([]inspectedElectronFile, error) {
	var found []inspectedElectronFile
	var result error
	for _, name := range electronRoamingDirectoryNames {
		in, err := openElectronCandidate(filepath.Join(roaming, name))
		if isMissingElectronSource(err) {
			continue
		}
		if err != nil {
			result = errors.Join(result, fmt.Errorf("Electron metadata candidate %q: %w", name, err))
			continue
		}
		found = append(found, in)
	}
	return found, result
}

func openElectronCandidate(candidate string) (inspectedElectronFile, error) {
	var out inspectedElectronFile
	dir, err := openElectronPath(candidate, true)
	if err != nil {
		return out, err
	}
	defer windows.CloseHandle(dir)
	dirInfo, err := electronHandleInfo(dir)
	if err != nil {
		return out, err
	}
	if dirInfo.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return out, errors.New("candidate directory reparse point rejected")
	}
	dirFinal, err := electronFinalPath(dir)
	if err != nil {
		return out, err
	}

	fileHandle, err := openElectronPath(filepath.Join(candidate, "providers.json"), false)
	if err != nil {
		return out, err
	}
	f := os.NewFile(uintptr(fileHandle), "providers.json")
	if f == nil {
		windows.CloseHandle(fileHandle)
		return out, errors.New("wrap Electron provider handle")
	}
	defer f.Close()
	fileInfo, err := electronHandleInfo(fileHandle)
	if err != nil {
		return out, err
	}
	if fileInfo.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return out, errors.New("provider file reparse point rejected")
	}
	if fileInfo.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return out, errors.New("provider source is a directory")
	}
	if fileInfo.NumberOfLinks != 1 {
		return out, errors.New("provider source hard link rejected")
	}
	fileFinal, err := electronFinalPath(fileHandle)
	if err != nil {
		return out, err
	}
	if !electronPathWithin(dirFinal, fileFinal) {
		return out, errors.New("provider file escaped allowlisted candidate directory")
	}
	stat, err := f.Stat()
	if err != nil {
		return out, err
	}
	if !stat.Mode().IsRegular() || stat.Size() < 0 || stat.Size() > maxElectronProviderFile {
		return out, errors.New("Electron provider file is not a bounded regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxElectronProviderFile+1))
	if err != nil {
		return out, err
	}
	if len(raw) > maxElectronProviderFile {
		return out, errors.New("Electron provider file exceeds size limit")
	}
	out, err = inspectElectronBytes(raw, fileFinal)
	if err != nil {
		return out, err
	}
	out.osCryptEncryptedKey, err = readElectronOSCryptKey(candidate, dirFinal)
	if err != nil && !isMissingElectronSource(err) {
		return out, err
	}
	return out, nil
}

func readElectronOSCryptKey(candidate, dirFinal string) (string, error) {
	h, err := openElectronPath(filepath.Join(candidate, "Local State"), false)
	if err != nil {
		return "", err
	}
	f := os.NewFile(uintptr(h), "Local State")
	if f == nil {
		windows.CloseHandle(h)
		return "", errors.New("wrap Electron Local State handle")
	}
	defer f.Close()
	info, err := electronHandleInfo(h)
	if err != nil || info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		return "", errors.New("invalid Electron Local State file")
	}
	if info.NumberOfLinks != 1 {
		return "", errors.New("Electron Local State hard link rejected")
	}
	final, err := electronFinalPath(h)
	if err != nil || !electronPathWithin(dirFinal, final) {
		return "", errors.New("Electron Local State escaped candidate directory")
	}
	const maxLocalState = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(f, maxLocalState+1))
	if err != nil || len(raw) > maxLocalState {
		return "", errors.New("invalid Electron Local State size")
	}
	var state struct {
		OSCrypt struct {
			EncryptedKey string `json:"encrypted_key"`
		} `json:"os_crypt"`
	}
	if json.Unmarshal(raw, &state) != nil || len(state.OSCrypt.EncryptedKey) > 4096 {
		return "", errors.New("invalid Electron Local State")
	}
	return state.OSCrypt.EncryptedKey, nil
}

func openElectronPath(path string, directory bool) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	flags := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT)
	access := uint32(windows.GENERIC_READ)
	if directory {
		flags |= windows.FILE_FLAG_BACKUP_SEMANTICS
		access = windows.FILE_READ_ATTRIBUTES
	}
	// No FILE_SHARE_DELETE: the pinned source objects cannot be renamed away.
	// Inputs are small and read once. Deny concurrent writes for this bounded
	// snapshot so ciphertext and Local State cannot mutate under pinned handles.
	return windows.CreateFile(p, access, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, flags, 0)
}

func electronHandleInfo(h windows.Handle) (windows.ByHandleFileInformation, error) {
	var info windows.ByHandleFileInformation
	err := windows.GetFileInformationByHandle(h, &info)
	return info, err
}

func electronFinalPath(h windows.Handle) (string, error) {
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

func electronPathWithin(base, child string) bool {
	b := strings.TrimPrefix(filepath.Clean(base), `\\?\`)
	c := strings.TrimPrefix(filepath.Clean(child), `\\?\`)
	rel, err := filepath.Rel(b, c)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, `..\`)
}
