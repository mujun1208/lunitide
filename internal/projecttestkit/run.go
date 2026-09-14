package projecttestkit

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/projectboard"
	"github.com/lunitide/lunitide/internal/projectschema"
	"github.com/lunitide/lunitide/internal/projecttask"
)

type Kind = string

const (
	KindUnit         = "unit"
	KindCLI          = "cli"
	KindLink         = "link"
	KindStability    = "stability"
	KindStress       = "stress"
	KindFluency      = "fluency"
	KindCompleteness = "completeness"
	KindConsistency  = "consistency"
	KindStructured   = "structured"
	KindReuse        = "reuse"
	KindIntegration  = "integration"
	KindDevice       = "device"
)

type RunInput struct {
	Kind     string
	RootPath string
	DBPath   string
	Schema   projectschema.Schema
	Board    projecttask.Doc
	Item     projecttask.Item
	Command  string
	Evidence string
	Timeout  time.Duration
}

func AutoKind(kind string) bool {
	switch kind {
	case KindUnit, KindCLI, KindCompleteness, KindConsistency, KindStructured:
		return true
	default:
		return false
	}
}

func Run(ctx context.Context, in RunInput) (projecttask.KindResult, error) {
	if in.Timeout <= 0 {
		in.Timeout = 60 * time.Second
	}
	if in.Timeout > 300*time.Second {
		in.Timeout = 300 * time.Second
	}
	now := time.Now().UTC().Format(time.RFC3339)
	switch in.Kind {
	case KindUnit:
		if strings.TrimSpace(in.Evidence) == "" {
			return projecttask.KindResult{}, projectapp.ErrTestKindUnsupported
		}
		status := "pass"
		summary := strings.TrimSpace(in.Evidence)
		if strings.HasPrefix(strings.ToLower(summary), "fail") {
			status = "fail"
		}
		return projecttask.KindResult{Status: status, At: now, Summary: summary}, nil
	case KindCLI:
		return runCLI(ctx, in, now)
	case KindCompleteness:
		return runCompleteness(in, now)
	case KindConsistency:
		if projectboard.Dirty(in.Board) {
			return projecttask.KindResult{Status: "fail", At: now, Summary: "清单与工作台不一致"}, nil
		}
		return projecttask.KindResult{Status: "pass", At: now, Summary: "一致性通过"}, nil
	case KindStructured:
		if strings.TrimSpace(in.Item.Title) == "" {
			return projecttask.KindResult{Status: "fail", At: now, Summary: "标题为空"}, nil
		}
		if in.Item.SourceKind == "interface" && (in.Item.Method == "" || in.Item.Path == "") {
			return projecttask.KindResult{Status: "fail", At: now, Summary: "接口缺少 method/path"}, nil
		}
		if in.Item.SourceKind != "interface" && strings.TrimSpace(in.Item.Acceptance) == "" && strings.TrimSpace(in.Item.Title) != "" {
			if in.Item.SourceKind == "dev" || in.Item.SourceKind == "" {
				return projecttask.KindResult{Status: "fail", At: now, Summary: "开发条目缺少验收"}, nil
			}
		}
		return projecttask.KindResult{Status: "pass", At: now, Summary: "结构化通过"}, nil
	case KindLink, KindStability, KindStress, KindFluency, KindReuse, KindIntegration, KindDevice:
		if strings.TrimSpace(in.Evidence) == "" {
			return projecttask.KindResult{}, projectapp.ErrTestKindUnsupported
		}
		return projecttask.KindResult{Status: "pass", At: now, Summary: in.Evidence}, nil
	default:
		return projecttask.KindResult{}, projectapp.ErrTestKindUnsupported
	}
}

func cliCommand(ctx context.Context, command, dir string) *exec.Cmd {
	var c *exec.Cmd
	if strings.Contains(command, " ") {
		if runtime.GOOS == "windows" {
			c = exec.CommandContext(ctx, "cmd", "/c", command)
		} else {
			c = exec.CommandContext(ctx, "sh", "-c", command)
		}
	} else {
		c = exec.CommandContext(ctx, command)
	}
	c.Dir = dir
	return c
}

func runCLI(ctx context.Context, in RunInput, now string) (projecttask.KindResult, error) {
	cmd := strings.TrimSpace(in.Command)
	if cmd == "" || len(cmd) > 500 {
		return projecttask.KindResult{}, projectapp.ErrTestKindUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, in.Timeout)
	defer cancel()
	c := cliCommand(ctx, cmd, in.RootPath)
	out, err := c.CombinedOutput()
	summary := strings.TrimSpace(string(out))
	if len(summary) > 2000 {
		summary = summary[:2000]
	}
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return projecttask.KindResult{Status: "fail", At: now, Summary: "CLI 超时"}, nil
		}
		if summary == "" {
			summary = err.Error()
		}
		return projecttask.KindResult{Status: "fail", At: now, Summary: summary}, nil
	}
	if summary == "" {
		summary = "CLI 退出码 0"
	}
	return projecttask.KindResult{Status: "pass", At: now, Summary: summary}, nil
}

func runCompleteness(in RunInput, now string) (projecttask.KindResult, error) {
	if len(in.Schema.Tables) > 0 {
		if err := projectschema.Verify(in.DBPath, in.Schema); err != nil {
			return projecttask.KindResult{Status: "fail", At: now, Summary: "库表未核齐"}, nil
		}
	}
	if in.Item.TargetRelPath != "" && in.RootPath != "" {
		if _, err := os.Stat(filepath.Join(in.RootPath, in.Item.TargetRelPath)); err != nil {
			return projecttask.KindResult{Status: "fail", At: now, Summary: "目标路径不存在"}, nil
		}
	}
	if in.Item.SourceKind == "interface" && strings.TrimSpace(in.Item.Path) == "" {
		return projecttask.KindResult{Status: "fail", At: now, Summary: "接口 path 为空"}, nil
	}
	return projecttask.KindResult{Status: "pass", At: now, Summary: "完整性通过"}, nil
}

func RequiredPassed(item projecttask.Item) bool {
	kinds := item.RequiredKinds
	if len(kinds) == 0 {
		kinds = []string{KindUnit}
	}
	for _, kind := range kinds {
		res, ok := item.KindResults[kind]
		if !ok || res.Status != "pass" {
			return false
		}
	}
	return true
}
