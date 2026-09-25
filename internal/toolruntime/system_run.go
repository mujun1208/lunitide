package toolruntime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/commandworker"
)

// systemRunRefusal keeps the hardline floor and the git verbs that stay
// off the product even when the command allowlist is not in force.
func systemRunRefusal(argv []string) string {
	if reason := hardlineRefusal(argv); reason != "" {
		return reason
	}
	for _, cmd := range flattenCommands(argv) {
		if gitDestructive(cmd) {
			return "git push, reset, and clean stay off"
		}
	}
	return ""
}

func gitDestructive(cmd []string) bool {
	if len(cmd) == 0 {
		return false
	}
	head := strings.ToLower(filepath.Base(cmd[0]))
	head = strings.TrimSuffix(head, ".exe")
	if head != "git" {
		return false
	}
	for _, arg := range cmd[1:] {
		switch strings.ToLower(arg) {
		case "push", "reset", "clean":
			return true
		}
	}
	return false
}

func (r *Runtime) runWorkspaceCommand(ctx context.Context, mode Mode, session string, argv []string, deadline time.Duration, progress func(chunk string)) (Result, error) {
	root, e := r.effectiveRoot(mode, session)
	if e != nil {
		return Result{}, e
	}
	if e = os.MkdirAll(root, 0700); e != nil {
		return Result{}, e
	}
	runArgv := append([]string(nil), argv...)
	runArgv, e = relocateMissingScripts(root, runArgv)
	if e != nil {
		return Result{}, commandFailure(e.Error())
	}
	runArgv, e = resolveInterpreter(runArgv)
	if e != nil {
		return Result{}, commandFailure(e.Error())
	}
	if isLocalServerCommand(runArgv) {
		msg, startErr := r.startLiveServer(ctx, session, root, runArgv)
		if startErr != nil {
			return Result{}, commandFailure(startErr.Error())
		}
		return result(formatCommandOutput(true, msg)), nil
	}
	if dir, ok := extractMkdirPath(argv); ok {
		dir = expandWindowsEnv(dir)
		if dir == "" {
			return Result{}, commandFailure("empty directory path")
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		if e = os.MkdirAll(dir, 0755); e != nil {
			return Result{}, commandFailure(e.Error())
		}
		return result(formatCommandOutput(true, "created directory: "+dir)), nil
	}
	prepared, cleanup, wrapErr := prepareCommandArgv(runArgv)
	if wrapErr != nil {
		return Result{}, commandFailure(wrapErr.Error())
	}
	defer cleanup()
	exe, resolveErr := exec.LookPath(prepared[0])
	if resolveErr != nil {
		return Result{}, commandFailure(resolveErr.Error())
	}
	exe, resolveErr = filepath.Abs(exe)
	if resolveErr != nil {
		return Result{}, commandFailure(resolveErr.Error())
	}
	output := &commandOutput{progress: progress}
	defer output.closeProgress()
	outcome, runErr := commandworker.Run(ctx, commandworker.Spec{
		Exe: exe, Args: prepared[1:], Dir: root, Timeout: deadline, MaxOutputBytes: commandworker.OutputHardCap, MaxArgBytes: 16 << 10,
		Env: commandEnv(os.Environ(), "GIT_PAGER=cat", "PAGER=cat", "TERM=dumb", "GIT_OPTIONAL_LOCKS=0", "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1"),
	}, nil, func(b []byte) { _, _ = output.Write(b) })
	text := output.text()
	if outcome.Truncated {
		text += "\n[worker output delivery limit reached]"
	}
	if runErr != nil || outcome.TimedOut || outcome.ExitCode != 0 {
		if runErr != nil {
			text += "\n" + runErr.Error()
		} else if outcome.TimedOut {
			text += "\ncommand deadline exceeded; process tree stopped"
		} else if strings.TrimSpace(text) == "" {
			text = fmt.Sprintf("exit status %d", outcome.ExitCode)
		}
		return Result{}, commandFailure(strings.TrimSpace(text))
	}
	return result(formatCommandOutput(true, text)), nil
}
