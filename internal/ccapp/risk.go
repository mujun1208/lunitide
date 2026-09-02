package ccapp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// DefaultProcessBlocklist seeds the first-read configuration: shells and
// system consoles where synthetic input could escalate privileges.
var DefaultProcessBlocklist = []string{
	"cmd.exe", "powershell.exe", "pwsh.exe", "regedit.exe",
	"taskmgr.exe", "mmc.exe", "eventvwr.exe", "consent.exe",
}

// forbiddenCombos are always rejected at the input-filter layer no matter
// the confirmation state (system-reserved sequences).
var forbiddenCombos = map[string]bool{
	"alt+ctrl+delete": true,
}

// criticalCombos carry system-level impact (close window, run dialog,
// lock, task manager, desktop flip); they ride the critical risk class.
var criticalCombos = map[string]bool{
	"alt+f4":         true,
	"alt+shift+esc":  false, // placeholder keeps map literal tidy
	"ctrl+shift+esc": true,
	"win+d":          true,
	"win+l":          true,
	"win+r":          true,
	"win+tab":        true,
}

// keyVocabulary is the portable key-name set accepted by
// keyboard_shortcut (lowercase; "del" normalizes to "delete").
var keyVocabulary = map[string]bool{
	"ctrl": true, "shift": true, "alt": true, "win": true,
	"enter": true, "esc": true, "space": true, "tab": true,
	"backspace": true, "delete": true, "home": true, "end": true,
	"pageup": true, "pagedown": true, "up": true, "down": true,
	"left": true, "right": true, "printscreen": true, "capslock": true,
	"a": true, "b": true, "c": true, "d": true, "e": true, "f": true,
	"g": true, "h": true, "i": true, "j": true, "k": true, "l": true,
	"m": true, "n": true, "o": true, "p": true, "q": true, "r": true,
	"s": true, "t": true, "u": true, "v": true, "w": true, "x": true,
	"y": true, "z": true,
	"0": true, "1": true, "2": true, "3": true, "4": true,
	"5": true, "6": true, "7": true, "8": true, "9": true,
	"f1": true, "f2": true, "f3": true, "f4": true, "f5": true,
	"f6": true, "f7": true, "f8": true, "f9": true, "f10": true,
	"f11": true, "f12": true, "f13": true, "f14": true, "f15": true,
	"f16": true, "f17": true, "f18": true, "f19": true, "f20": true,
	"f21": true, "f22": true, "f23": true, "f24": true,
	"media_play": true, "media_pause": true, "media_next": true, "media_prev": true, "media_stop": true,
}

// classifyRisk answers the four-level risk class for one tool call
// (layer 1: intent recognition).
func classifyRisk(tool string, normalizedShortcut []string) string {
	switch tool {
	case ToolMouseMove, ToolGetActiveWindow, ToolObserveDialog, ToolWindowList, ToolObserveUI, ToolWait, ToolAppList:
		return RiskLow
	case ToolMouseClick, ToolMouseDrag, ToolKeyboardType, ToolScreenCapture, ToolConfirmDialog, ToolWindowFocus:
		return RiskMedium
	case ToolClipboard, ToolPaste, ToolPress, ToolSetValue:
		return RiskMedium
	case ToolWindowAction:
		if len(normalizedShortcut) > 0 && destructiveWindowOp(normalizedShortcut[0]) {
			return RiskHigh
		}
		return RiskMedium
	case ToolAppQuit, ToolMenuClick:
		return RiskHigh
	case ToolKeyboardShortcut:
		if isCriticalCombo(normalizedShortcut) {
			return RiskCritical
		}
		return RiskHigh
	}
	return RiskCritical
}

// normalizeShortcut validates and normalizes key names: lowercase, del →
// delete, order-insensitive combo signature.
func normalizeShortcut(keys []string) ([]string, error) {
	if len(keys) < 1 || len(keys) > CcMaxShortcutKeys {
		return nil, fmt.Errorf("%w: combo size", ErrCcInputFiltered)
	}
	hasNonModifier := false
	seen := map[string]bool{}
	out := make([]string, 0, len(keys))
	for _, raw := range keys {
		key := strings.ToLower(strings.TrimSpace(raw))
		if key == "del" {
			key = "delete"
		}
		if !keyVocabulary[key] {
			return nil, fmt.Errorf("%w: key %q", ErrCcInputFiltered, raw)
		}
		if seen[key] {
			return nil, fmt.Errorf("%w: duplicate key %q", ErrCcInputFiltered, key)
		}
		seen[key] = true
		if key != "ctrl" && key != "shift" && key != "alt" && key != "win" {
			hasNonModifier = true
		}
		out = append(out, key)
	}
	if !hasNonModifier {
		return nil, fmt.Errorf("%w: modifier-only combo", ErrCcInputFiltered)
	}
	sort.Strings(out)
	if forbiddenCombos[strings.Join(out, "+")] {
		return nil, fmt.Errorf("%w: forbidden combo", ErrCcInputFiltered)
	}
	return out, nil
}

func isCriticalCombo(normalized []string) bool {
	return criticalCombos[strings.Join(normalized, "+")]
}

// screenAffecting reports whether a tool acts on the shared desktop and
// therefore rides the foreground-process gate (layer 3).
func screenAffecting(tool string) bool {
	switch tool {
	case ToolGetActiveWindow, ToolObserveDialog, ToolObserveUI, ToolWindowList, ToolWait, ToolAppList:
		return false
	case ToolClipboard:
		return false
	}
	return true
}

func blocklistHit(blocklist []string, process string) bool {
	name := strings.ToLower(strings.TrimSpace(process))
	for _, item := range blocklist {
		if strings.EqualFold(strings.TrimSpace(item), name) {
			return true
		}
	}
	return false
}

func (s *Service) rejectTargetProcess(settings Settings, tool string, args json.RawMessage) error {
	if tool != ToolWindowAction && tool != ToolAppQuit {
		return nil
	}
	if s.host == nil {
		return nil
	}
	var a struct {
		Title string `json:"title"`
		Name  string `json:"name"`
		Op    string `json:"op"`
	}
	_ = json.Unmarshal(args, &a)
	query := strings.TrimSpace(a.Title)
	if query == "" {
		query = strings.TrimSpace(a.Name)
	}
	if query == "" {
		query = "foreground"
	}
	wins, err := s.host.ListWindows()
	if err != nil || len(wins) == 0 {
		return nil
	}
	var targets []WindowInfo
	if tool == ToolAppQuit {
		targets = MatchWindows(wins, query)
	} else if info, ok := MatchWindow(wins, query); ok {
		targets = []WindowInfo{info}
	}
	op := strings.ToLower(strings.TrimSpace(a.Op))
	checkProtected := tool == ToolAppQuit || destructiveWindowOp(op)
	for _, w := range targets {
		if blocklistHit(settings.ProcessBlocklist, w.Process) {
			return ErrCcProcessBlocked
		}
		if checkProtected && ProtectedDesktopProcess(w.Process) {
			return fmt.Errorf("%w: protected process %s", ErrCcRiskBlocked, w.Process)
		}
		if checkProtected && documentEditorProcess(w.Process) {
			q := strings.ToLower(strings.TrimSpace(query))
			if q == "" || q == "foreground" {
				return fmt.Errorf("%w: refusing to close the open document", ErrCcRiskBlocked)
			}
		}
	}
	return nil
}
