//go:build !windows

package omni

import "fmt"

func applyRuntimeSetup(_, _ string) error {
	return fmt.Errorf("omni: 自动安装 llama-omni-server 仅支持 Windows")
}
