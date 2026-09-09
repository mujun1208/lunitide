//go:build windows

package winexec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// QuitProcessImages never shells out, matches exact executable names,
// and stays in our session. Callers require an explicit full-exit request.
func QuitProcessImages(ctx context.Context, names []string) (int, error) {
	want := map[string]bool{}
	for _, n := range names {
		want[strings.ToLower(n)] = true
	}
	var ownSession uint32
	if err := windows.ProcessIdToSessionId(uint32(os.Getpid()), &ownSession); err != nil {
		return 0, err
	}
	count := 0
	for pass := 0; pass < 3; pass++ {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
		if err != nil {
			return count, err
		}
		var entry windows.ProcessEntry32
		entry.Size = uint32(unsafe.Sizeof(entry))
		err = windows.Process32First(snapshot, &entry)
		found := false
		for err == nil {
			if cancelErr := ctx.Err(); cancelErr != nil {
				windows.CloseHandle(snapshot)
				return count, cancelErr
			}
			name := strings.ToLower(windows.UTF16ToString(entry.ExeFile[:]))
			var session uint32
			if want[name] && entry.ProcessID != uint32(os.Getpid()) && windows.ProcessIdToSessionId(entry.ProcessID, &session) == nil && session == ownSession {
				found = true
				h, openErr := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, entry.ProcessID)
				if errors.Is(openErr, windows.ERROR_INVALID_PARAMETER) {
					// A child may exit with its parent between enumeration and OpenProcess.
					err = windows.Process32Next(snapshot, &entry)
					continue
				}
				if openErr != nil {
					windows.CloseHandle(snapshot)
					return count, openErr
				}
				// Revalidate the held process handle, not a possibly reused PID.
				var buf [32768]uint16
				size := uint32(len(buf))
				openErr = windows.QueryFullProcessImageName(h, 0, &buf[0], &size)
				image := strings.ToLower(windows.UTF16ToString(buf[:size]))
				if openErr == nil && !strings.HasSuffix(image, `\`+name) {
					openErr = errors.New("process identity changed")
				}
				if openErr == nil {
					openErr = windows.TerminateProcess(h, 0)
				}
				if openErr == nil {
					var status uint32
					status, openErr = windows.WaitForSingleObject(h, 2000)
					if openErr == nil && status != windows.WAIT_OBJECT_0 {
						openErr = errors.New("process has not exited")
					}
				}
				if openErr != nil {
					if status, waitErr := windows.WaitForSingleObject(h, 0); waitErr == nil && status == windows.WAIT_OBJECT_0 {
						openErr = nil
					}
				}
				windows.CloseHandle(h)
				if openErr != nil {
					windows.CloseHandle(snapshot)
					return count, openErr
				}
				count++
			}
			err = windows.Process32Next(snapshot, &entry)
		}
		windows.CloseHandle(snapshot)
		if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return count, err
		}
		if !found {
			return count, nil
		}
	}
	return count, fmt.Errorf("application restarted or processes remain")
}
