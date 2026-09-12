package agenthub

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const kimiPromptArg = "请阅读并执行本目录 .agenthub-prompt.txt。只在本目录创建或修改文件。"

func Adapter(name string) (AgentAdapter, error) {
	switch name {
	case "codex":
		return codexAdapter{}, nil
	case "cursor":
		return cursorAdapter{}, nil
	case "kimi":
		return kimiAdapter{}, nil
	default:
		return nil, fmt.Errorf("unknown agent %q", name)
	}
}

type codexAdapter struct{}

func (codexAdapter) Name() string { return "codex" }

func (a codexAdapter) Detect(look LookPath, version VersionRunner) AgentStatus {
	return detectOne(a.Name(), look, version)
}

func (codexAdapter) BuildCommand(req TaskRequest) (string, []string, []byte, error) {
	sandbox := req.Sandbox
	if sandbox == "" {
		sandbox = "workspace-write"
	}
	return "codex", []string{"exec", "--json", "--skip-git-repo-check", "--ignore-user-config", "--sandbox", sandbox, "--cd", req.WorkDir, "-o", filepath.Join(req.WorkDir, "codex-last-message.md")}, []byte(req.Prompt), nil
}

func (codexAdapter) ParseLine(line string) (AgentEvent, bool) { return ParseLine("codex", line) }

type cursorAdapter struct{}

func (cursorAdapter) Name() string { return "cursor" }

func (a cursorAdapter) Detect(look LookPath, version VersionRunner) AgentStatus {
	return detectOne(a.Name(), look, version)
}

func (cursorAdapter) BuildCommand(req TaskRequest) (string, []string, []byte, error) {
	return "cursor-agent", []string{"-p", "--force", "--trust", "--workspace", req.WorkDir, "--output-format", "stream-json"}, []byte(req.Prompt), nil
}

func (cursorAdapter) ParseLine(line string) (AgentEvent, bool) { return ParseLine("cursor", line) }

type kimiAdapter struct{}

func (kimiAdapter) Name() string { return "kimi" }

func (a kimiAdapter) Detect(look LookPath, version VersionRunner) AgentStatus {
	return detectOne(a.Name(), look, version)
}

func (kimiAdapter) BuildCommand(req TaskRequest) (string, []string, []byte, error) {
	if strings.TrimSpace(req.WorkDir) == "" {
		return "", nil, nil, fmt.Errorf("工作目录无效")
	}
	if err := os.WriteFile(filepath.Join(req.WorkDir, promptFileName), []byte(req.Prompt), 0o644); err != nil {
		return "", nil, nil, err
	}
	args := []string{"-p", kimiPromptArg, "--output-format", "stream-json"}
	for _, dir := range kimiSkillDirs() {
		args = append(args, "--skills-dir", dir)
	}
	return "kimi", args, nil, nil
}

func (kimiAdapter) ParseLine(line string) (AgentEvent, bool) { return ParseLine("kimi", line) }

func normalizeAgent(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
