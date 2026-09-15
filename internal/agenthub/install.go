package agenthub

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type CommandRunner func(ctx context.Context, name string, args ...string) (string, error)

type InstallResult struct {
	Agents    []AgentStatus `json:"agents"`
	Installed bool          `json:"installed"`
	Connected bool          `json:"connected"`
	Hint      string        `json:"hint"`
}

const (
	installRecipeTimeout = 4 * time.Minute
	installLoginTimeout  = 2 * time.Minute
)

func npmBin() string {
	if runtime.GOOS == "windows" {
		return "npm.cmd"
	}
	return "npm"
}

func installRecipes(name string) [][]string {
	switch name {
	case "codex":
		return [][]string{{npmBin(), "i", "-g", "@openai/codex"}}
	case "cursor":
		if runtime.GOOS == "windows" {
			return [][]string{
				{"powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "irm https://cursor.com/install?win32=true | iex"},
				{npmBin(), "i", "-g", "@cursor/cli"},
			}
		}
		return [][]string{
			{"bash", "-lc", "curl https://cursor.com/install -fsS | bash"},
			{npmBin(), "i", "-g", "@cursor/cli"},
		}
	case "kimi":
		return [][]string{
			{npmBin(), "i", "-g", "@moonshot-ai/kimi-cli"},
			{npmBin(), "i", "-g", "@moonshot-ai/kimi-code"},
		}
	default:
		return nil
	}
}

func loginExe(name string, look LookPath) string {
	if look == nil {
		return exeName(name)
	}
	path, err := look(exeName(name))
	if err != nil || strings.TrimSpace(path) == "" {
		return exeName(name)
	}
	return path
}

func defaultCommandRun(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	hideVersionCmd(cmd)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func InstallAndConnect(ctx context.Context, name string, confirmed bool, look LookPath, version VersionRunner, run CommandRunner) (InstallResult, error) {
	if name != "codex" && name != "cursor" && name != "kimi" {
		return InstallResult{}, fmt.Errorf("参数无效")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if look == nil {
		look = defaultLookPath
	}
	if version == nil {
		version = defaultVersion
	}
	if run == nil {
		run = defaultCommandRun
	}
	current := detectOne(name, look, version)
	if !confirmed {
		return installResult(name, look, version, current), nil
	}
	if current.State == "not_installed" {
		var lastErr error
		for _, args := range installRecipes(name) {
			if err := ctx.Err(); err != nil {
				return installResult(name, look, version, current), err
			}
			runCtx, cancel := context.WithTimeout(ctx, installRecipeTimeout)
			_, err := run(runCtx, args[0], args[1:]...)
			cancel()
			if err != nil {
				if ctx.Err() != nil {
					return installResult(name, look, version, current), ctx.Err()
				}
				lastErr = err
				continue
			}
			current = detectOne(name, look, version)
			if current.State != "not_installed" {
				break
			}
		}
		current = detectOne(name, look, version)
		if current.State == "not_installed" && lastErr != nil {
			current.Hint = clip(lastErr.Error(), 200)
		}
	}
	if current.State == "not_logged_in" {
		if err := ctx.Err(); err != nil {
			return installResult(name, look, version, current), err
		}
		runCtx, cancel := context.WithTimeout(ctx, installLoginTimeout)
		_, err := run(runCtx, loginExe(name, look), "login")
		cancel()
		if err != nil && ctx.Err() != nil {
			return installResult(name, look, version, current), ctx.Err()
		}
		current = detectOne(name, look, version)
	}
	return installResult(name, look, version, current), nil
}

func installResult(name string, look LookPath, version VersionRunner, current AgentStatus) InstallResult {
	agents := DetectAll(look, version)
	for i, item := range agents {
		if item.Name == name {
			agents[i] = current
		}
	}
	return InstallResult{
		Agents:    agents,
		Installed: current.State != "not_installed",
		Connected: current.State == "available",
		Hint:      current.Hint,
	}
}
