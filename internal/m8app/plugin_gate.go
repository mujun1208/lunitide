package m8app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// RequireEnabled checks the committed install and binding on every execution.
// An absent builtin install preserves pre-roster deployments; an explicit
// disabled/uninstalled/quarantined install never falls back to that default.
func (s *PluginService) RequireEnabled(ctx context.Context, pluginIDs ...string) error {
	if len(pluginIDs) == 0 {
		return nil
	}
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	return s.uow.TransactPlugin(ctx, func(tx PluginTx) error {
		for _, id := range pluginIDs {
			install, exists, err := tx.GetInstallBySubjectPlugin(s.subject, id)
			if err != nil {
				return err
			}
			if !exists {
				continue
			}
			if install.State != m8core.InstallEnabled {
				return fmt.Errorf("%w: %s", ErrBindingInactive, id)
			}
			bindings, err := tx.ListBindings(install.InstallID)
			if err != nil {
				return err
			}
			active := false
			for _, binding := range bindings {
				if binding.State == m8core.BindingActive {
					active = true
					break
				}
			}
			if !active {
				return fmt.Errorf("%w: %s", ErrBindingInactive, id)
			}
		}
		return nil
	})
}

// ToolPluginIDs maps runtime capabilities to the shipped enablement controls.
// command.run is called after run_terminal_cmd normalization by toolruntime.
func ToolPluginIDs(name string, args json.RawMessage) []string {
	switch {
	case strings.HasPrefix(name, "workspace."), name == "html.gen", name == "excel.gen", name == "docx.gen", name == "pptx.gen", name == "pdf.gen":
		return []string{"workspace", "filesystem"}
	case name == "web.search":
		return []string{"web-search"}
	case name == "web.fetch", name == "video.understand":
		return []string{"web-fetch"}
	case strings.HasPrefix(name, "browser."):
		return []string{"browser"}
	case strings.HasPrefix(name, "memory."):
		return []string{"memory"}
	case strings.HasPrefix(name, "skill."):
		return []string{"skills"}
	case strings.HasPrefix(name, "git."):
		return []string{"git"}
	case strings.HasPrefix(name, "clipboard."):
		return []string{"clipboard"}
	case strings.HasPrefix(name, "notification."):
		return []string{"notification"}
	case name == "command.run":
		var input struct {
			Argv []string `json:"argv"`
		}
		if json.Unmarshal(args, &input) != nil || len(input.Argv) == 0 {
			return nil
		}
		command := strings.TrimSuffix(strings.ToLower(filepath.Base(strings.ReplaceAll(input.Argv[0], "\\", "/"))), ".exe")
		switch command {
		case "git":
			return []string{"git"}
		case "powershell", "pwsh":
			return []string{"tool-pwsh"}
		case "cmd":
			return []string{"tool-cmd"}
		case "bash", "sh", "zsh":
			return []string{"tool-bash"}
		case "python", "python3", "pythonw", "py":
			return []string{"tool-python"}
		}
	}
	return nil
}
