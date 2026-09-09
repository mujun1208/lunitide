//go:build !windows

package officerender

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/commandworker"
)

func systemFontInventory(ctx context.Context) (FontInventory, error) {
	out := FontInventory{Basis: "fontconfig-registered-families", Families: []string{}}
	exe, err := exec.LookPath("fc-list")
	if err != nil {
		out.Basis = "unavailable"
		return out, errors.New("本机未提供可核验的字体清单")
	}
	var output bytes.Buffer
	result, err := commandworker.Run(ctx, commandworker.Spec{Exe: exe, Args: []string{"--format=%{family}\n"}, Timeout: 5 * time.Second, MaxOutputBytes: 1 << 20, MaxMemoryBytes: 128 << 20}, nil, func(data []byte) { output.Write(data) })
	if err != nil {
		return out, err
	}
	out.Complete = result.ExitCode == 0 && !result.TimedOut && !result.Truncated
	for _, line := range strings.Split(output.String(), "\n") {
		for _, name := range strings.Split(line, ",") {
			if name = fontFamilyName(name); name != "" {
				out.Families = append(out.Families, name)
			}
		}
	}
	if len(out.Families) == 0 {
		out.Complete = false
		return out, errors.New("字体清单为空")
	}
	return out, nil
}
