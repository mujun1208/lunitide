//go:build !windows

package people

import (
	"bytes"
	"os/exec"
	"runtime"
	"strings"
)

func pickLocalPath(folder bool) (string, error) {
	if runtime.GOOS == "darwin" {
		return pickDarwin(folder)
	}
	return pickLinux(folder)
}

func pickDarwin(folder bool) (string, error) {
	if _, err := exec.LookPath("osascript"); err != nil {
		return "", ErrUnsupported
	}
	prompt := "选择要发送的文件"
	script := `POSIX path of (choose file with prompt "` + prompt + `")`
	if folder {
		prompt = "选择要打包发送的文件夹"
		script = `POSIX path of (choose folder with prompt "` + prompt + `")`
	}
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	path := strings.TrimSpace(string(out))
	if err != nil {
		if isPickerCanceled(path, err) {
			return "", ErrCanceled
		}
		return "", ErrUnsupported
	}
	if path == "" {
		return "", ErrCanceled
	}
	return strings.TrimSuffix(path, "/"), nil
}

func pickLinux(folder bool) (string, error) {
	if path, err := pickZenity(folder); err == nil || err == ErrCanceled {
		return path, err
	}
	if path, err := pickKDialog(folder); err == nil || err == ErrCanceled {
		return path, err
	}
	return "", ErrUnsupported
}

func pickZenity(folder bool) (string, error) {
	if _, err := exec.LookPath("zenity"); err != nil {
		return "", ErrUnsupported
	}
	args := []string{"--file-selection", "--title=选择要发送的文件"}
	if folder {
		args = []string{"--file-selection", "--directory", "--title=选择要打包发送的文件夹"}
	}
	out, err := exec.Command("zenity", args...).CombinedOutput()
	path := strings.TrimSpace(string(out))
	if err != nil {
		if isPickerCanceled(path, err) {
			return "", ErrCanceled
		}
		return "", ErrUnsupported
	}
	if path == "" {
		return "", ErrCanceled
	}
	return path, nil
}

func pickKDialog(folder bool) (string, error) {
	if _, err := exec.LookPath("kdialog"); err != nil {
		return "", ErrUnsupported
	}
	args := []string{"--getopenfilename", ".", "*"}
	if folder {
		args = []string{"--getexistingdirectory", "."}
	}
	out, err := exec.Command("kdialog", args...).CombinedOutput()
	path := strings.TrimSpace(string(out))
	if err != nil {
		if isPickerCanceled(path, err) {
			return "", ErrCanceled
		}
		return "", ErrUnsupported
	}
	if path == "" {
		return "", ErrCanceled
	}
	return path, nil
}

func isPickerCanceled(output string, err error) bool {
	lower := strings.ToLower(output + " " + err.Error())
	if strings.Contains(lower, "cancel") || strings.Contains(lower, "取消") {
		return true
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 && !bytes.Contains([]byte(lower), []byte("error")) {
		return true
	}
	return false
}
