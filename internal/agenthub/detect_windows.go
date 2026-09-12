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
