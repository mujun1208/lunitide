//go:build windows

package agenthub

import (
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

func hideVersionCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func vendorInstallDirs() []string {
	var dirs []string
	type src struct {
		root registry.Key
		path string
	}
	for _, item := range []src{
		{registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `Software\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
	} {
		key, err := registry.OpenKey(item.root, item.path, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		names, _ := key.ReadSubKeyNames(512)
		for _, name := range names {
			sub, err := registry.OpenKey(key, name, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			display, _, _ := sub.GetStringValue("DisplayName")
			loc, _, _ := sub.GetStringValue("InstallLocation")
			sub.Close()
			low := strings.ToLower(display)
			if loc == "" || (!strings.Contains(low, "cursor") && !strings.Contains(low, "kimi") && !strings.Contains(low, "codex")) {
				continue
			}
			dirs = append(dirs, strings.TrimSpace(os.ExpandEnv(loc)))
		}
		key.Close()
	}
	return dirs
}

func extraPathDirs() []string {
	var dirs []string
	type src struct {
		root registry.Key
		path string
	}
	for _, item := range []src{
		{registry.CURRENT_USER, `Environment`},
		{registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`},
	} {
		key, err := registry.OpenKey(item.root, item.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		val, _, err := key.GetStringValue("Path")
		key.Close()
		if err != nil {
			continue
		}
		for _, part := range strings.Split(val, ";") {
			part = strings.TrimSpace(os.ExpandEnv(part))
			if part != "" {
				dirs = append(dirs, part)
			}
		}
	}
	return dirs
}
