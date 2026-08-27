//go:build !windows

package meetings

import (
	"os/exec"
	"runtime"
	"strings"
)

func pickSavePath(title, defaultName string) (string, error) {
	if runtime.GOOS == "darwin" {
		return pickSaveDarwin(title, defaultName)
	}
	return pickSaveLinux(title, defaultName)
}

func pickSaveDarwin(title, defaultName string) (string, error) {
	if _, err := exec.LookPath("osascript"); err != nil {
		return "", ErrUnsupported
	}
	script := `POSIX path of (choose file name with prompt "` + title + `" default name "` + defaultName + `")`
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	path := strings.TrimSpace(string(out))
	if err != nil {
		if strings.Contains(strings.ToLower(path+" "+err.Error()), "cancel") {
			return "", ErrCanceled
		}
		return "", ErrUnsupported
	}
	if path == "" {
		return "", ErrCanceled
	}
	return strings.TrimSuffix(path, "/"), nil
}

func pickSaveLinux(title, defaultName string) (string, error) {
	if _, err := exec.LookPath("zenity"); err == nil {
		out, err := exec.Command("zenity", "--file-selection", "--save", "--confirm-overwrite", "--title="+title, "--filename="+defaultName).CombinedOutput()
		path := strings.TrimSpace(string(out))
		if err != nil {
			if strings.Contains(strings.ToLower(path+" "+err.Error()), "cancel") {
				return "", ErrCanceled
			}
			return "", ErrUnsupported
		}
		if path == "" {
			return "", ErrCanceled
		}
		return path, nil
	}
	if _, err := exec.LookPath("kdialog"); err == nil {
		out, err := exec.Command("kdialog", "--getsavefilename", defaultName).CombinedOutput()
		path := strings.TrimSpace(string(out))
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "cancel") {
				return "", ErrCanceled
			}
			return "", ErrUnsupported
		}
		if path == "" {
			return "", ErrCanceled
		}
		return path, nil
	}
	return "", ErrUnsupported
}
