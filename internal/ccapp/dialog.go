package ccapp

import (
	"strings"
	"unicode"
)

// DialogSnapshot is one observed top-level dialog (or dialog-like window)
// plus the push-button names found through UI Automation / Win32 children.
type DialogSnapshot struct {
	Title       string   `json:"title"`
	Process     string   `json:"process"`
	Class       string   `json:"class,omitempty"`
	X           int      `json:"x,omitempty"`
	Y           int      `json:"y,omitempty"`
	W           int      `json:"w,omitempty"`
	H           int      `json:"h,omitempty"`
	Buttons     []string `json:"buttons"`
	Nodes       []UINode `json:"nodes,omitempty"`
	Confirmable bool     `json:"confirmable"`
	Refused     string   `json:"refused,omitempty"`
}

var confirmButtonNames = []string{
	"ok", "yes", "确定", "确认", "是", "是的",
}

var uacProcesses = []string{
	"consent.exe", "useraccountcontrolsettings.exe",
}

var uacTitleHints = []string{
	"用户账户控制", "user account control",
}

var elevationHints = []string{
	"要对你的设备进行更改", "make changes to your device",
	"要允许此应用", "allow this app to make changes",
	"do you want to allow this app",
}

var fileDialogTitleHints = []string{
	"打开", "open", "另存为", "save as", "保存文件", "打开文件",
	"open file", "save file",
}

// ClassifyDialog decides whether a discovered window may be auto-confirmed.
// UAC / elevation and file Open/Save pickers are never confirmable so the
// companion cannot accept unknown files or elevate via UAC tricks.
func ClassifyDialog(title, process, class string, buttons []string) (confirmable bool, reason string) {
	proc := strings.ToLower(strings.TrimSpace(process))
	for _, name := range uacProcesses {
		if proc == name {
			return false, "uac dialog"
		}
	}
	folded := strings.ToLower(title)
	for _, hint := range uacTitleHints {
		if strings.Contains(folded, hint) {
			return false, "uac dialog"
		}
	}
	for _, hint := range elevationHints {
		if strings.Contains(folded, hint) {
			return false, "elevation dialog"
		}
	}
	if looksLikeFilePicker(title, buttons) {
		return false, "file open/save dialog"
	}
	if ConfirmButtonName(buttons, "") == "" {
		return false, "no Yes/OK/确认 button"
	}
	_ = class
	return true, ""
}

// SensitiveSurfaceReason reports UAC / elevation / file Open-Save surfaces
// that computer-control must not click or confirm. Ordinary windows return "".
func SensitiveSurfaceReason(title, process, class string, buttons []string) string {
	_, reason := ClassifyDialog(title, process, class, buttons)
	switch reason {
	case "uac dialog", "elevation dialog", "file open/save dialog":
		return reason
	}
	return ""
}

func looksLikeFilePicker(title string, buttons []string) bool {
	folded := strings.ToLower(strings.TrimSpace(title))
	titleHit := false
	for _, hint := range fileDialogTitleHints {
		if folded == hint || strings.HasPrefix(folded, hint+" ") || strings.HasPrefix(folded, hint+" -") {
			titleHit = true
			break
		}
	}
	if !titleHit && (folded == "save" || folded == "保存") {
		titleHit = true
	}
	if !titleHit {
		return false
	}
	for _, b := range buttons {
		n := normalizeButton(b)
		if n == "打开" || n == "open" || n == "保存" || n == "save" || n == "另存为" || n == "save as" {
			return true
		}
	}
	return false
}

// IsConfirmButton reports whether a button caption is a standard confirm
// action (Yes / OK / 确认 / 是 / 确定), ignoring accelerator ampersands.
func IsConfirmButton(caption string) bool {
	n := normalizeButton(caption)
	if n == "" {
		return false
	}
	for _, want := range confirmButtonNames {
		if n == want {
			return true
		}
	}
	return false
}

// ConfirmButtonName picks the live caption to click. requested may be empty,
// "auto", "ok", "yes", "confirm", or a literal caption.
func ConfirmButtonName(buttons []string, requested string) string {
	req := normalizeButton(requested)
	if req == "" || req == "auto" || req == "confirm" {
		for _, b := range buttons {
			if IsConfirmButton(b) {
				return b
			}
		}
		return ""
	}
	aliases := confirmAliases(req)
	for _, b := range buttons {
		n := normalizeButton(b)
		if n == req {
			return b
		}
		for _, a := range aliases {
			if n == a {
				return b
			}
		}
	}
	return ""
}

func confirmAliases(req string) []string {
	switch req {
	case "ok", "确定", "确认":
		return []string{"ok", "确定", "确认"}
	case "yes", "是", "是的":
		return []string{"yes", "是", "是的"}
	}
	return nil
}

func normalizeButton(caption string) string {
	s := strings.TrimSpace(caption)
	if i := strings.Index(s, "(&"); i >= 0 {
		if j := strings.Index(s[i:], ")"); j >= 0 {
			s = s[:i] + s[i+j+1:]
		}
	}
	s = strings.ReplaceAll(s, "&", "")
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) || r == '(' || r == ')' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func formatDialogSummary(action string, snap DialogSnapshot) string {
	title := strings.TrimSpace(snap.Title)
	if title == "" {
		title = "(untitled)"
	}
	return action + " \"" + title + "\" (process: " + snap.Process + ", buttons: " + strings.Join(snap.Buttons, "/") + ")"
}
