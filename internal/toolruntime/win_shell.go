package toolruntime

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// powershellUTF8Preamble forces Windows PowerShell 5.1 (code page 936 by
// default) to round-trip UTF-8 so CJK paths like 小宝 are not created as
// 灏忓疂 (UTF-8 bytes misread as GBK).
const powershellUTF8Preamble = `$ErrorActionPreference = 'Stop'
[Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)
try { chcp 65001 | Out-Null } catch {}
`

var windowsEnvToken = regexp.MustCompile(`%([^%]+)%`)

var (
	newItemDirRe         = regexp.MustCompile(`(?is)New-Item\b.*(?:-ItemType|-Type)\s+Directory`)
	newItemPathQuotedRe  = regexp.MustCompile(`(?i)(?:-LiteralPath|-Path)\s+'([^']+)'`)
	newItemPathDQuotedRe = regexp.MustCompile(`(?i)(?:-LiteralPath|-Path)\s+"([^"]+)"`)
	newItemPathBareRe    = regexp.MustCompile(`(?i)(?:-LiteralPath|-Path)\s+(\S+)`)
)

func isPowerShellExe(name string) bool {
	base := strings.ToLower(filepath.Base(strings.Trim(name, `"`)))
	return base == "powershell" || base == "powershell.exe" || base == "pwsh" || base == "pwsh.exe"
}

func isCmdExe(name string) bool {
	base := strings.ToLower(filepath.Base(strings.Trim(name, `"`)))
	return base == "cmd" || base == "cmd.exe"
}

func flagName(arg string) string {
	s := strings.TrimSpace(arg)
	if strings.HasPrefix(s, "--") {
		s = s[2:]
	} else {
		s = strings.TrimLeft(s, "-/")
	}
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}

func powershellExeFor(original string) string {
	base := strings.ToLower(filepath.Base(strings.Trim(original, `"`)))
	if base == "pwsh" || base == "pwsh.exe" {
		if original != "" {
			return original
		}
		return "pwsh"
	}
	if runtime.GOOS != "windows" {
		if original != "" {
			return original
		}
		return "powershell"
	}
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
}

func utf16LEToString(b []byte) string {
	if len(b) >= 2 && b[0] == 0xff && b[1] == 0xfe {
		b = b[2:]
	}
	if len(b)%2 == 1 {
		b = b[:len(b)-1]
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return string(utf16.Decode(u))
}

// powershellCommandBody extracts the script passed to -Command / -c /
// -EncodedCommand. -File invocations are left alone (ok=false).
func powershellCommandBody(argv []string) (body string, ok bool) {
	if len(argv) < 2 || !isPowerShellExe(argv[0]) {
		return "", false
	}
	for i := 1; i < len(argv); i++ {
		switch flagName(argv[i]) {
		case "file", "f":
			return "", false
		case "encodedcommand", "enc", "encodedc":
			if i+1 >= len(argv) {
				return "", false
			}
			raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(argv[i+1]))
			if err != nil {
				return "", false
			}
			return strings.TrimSpace(utf16LEToString(raw)), true
		case "command", "c":
			if i+1 >= len(argv) {
				return "", false
			}
			return strings.TrimSpace(strings.Join(argv[i+1:], " ")), true
		}
	}
	return "", false
}

func parseNewItemDirectoryPath(body string) (string, bool) {
	if !newItemDirRe.MatchString(body) {
		return "", false
	}
	if strings.Contains(body, "GetFolderPath") || strings.Contains(body, "Join-Path") {
		return "", false
	}
	if m := newItemPathQuotedRe.FindStringSubmatch(body); len(m) == 2 {
		return strings.TrimSpace(m[1]), m[1] != ""
	}
	if m := newItemPathDQuotedRe.FindStringSubmatch(body); len(m) == 2 {
		return strings.TrimSpace(m[1]), m[1] != ""
	}
	if m := newItemPathBareRe.FindStringSubmatch(body); len(m) == 2 {
		p := strings.Trim(m[1], `"'`)
		return p, p != ""
	}
	return "", false
}

func stripCmdPrefix(argv []string) []string {
	if len(argv) == 0 || !isCmdExe(argv[0]) {
		return argv
	}
	i := 1
	for i < len(argv) {
		a := argv[i]
		if strings.EqualFold(a, "/c") || strings.EqualFold(a, "-c") {
			return argv[i+1:]
		}
		if strings.HasPrefix(a, "/") || strings.HasPrefix(a, "-") {
			i++
			continue
		}
		break
	}
	return argv[i:]
}

func isMkdirHead(name string) bool {
	base := strings.ToLower(filepath.Base(strings.Trim(name, `"`)))
	return base == "mkdir" || base == "md" || base == "mkdir.exe"
}

// extractMkdirPath recognizes mkdir/md/New-Item Directory so the runtime
// can create the folder through Go's Unicode APIs instead of a GBK shell.
func extractMkdirPath(argv []string) (string, bool) {
	if len(argv) == 0 {
		return "", false
	}
	if isPowerShellExe(argv[0]) {
		body, ok := powershellCommandBody(argv)
		if !ok {
			return "", false
		}
		return parseNewItemDirectoryPath(body)
	}
	rest := stripCmdPrefix(argv)
	if len(rest) >= 2 && isMkdirHead(rest[0]) {
		return strings.Trim(rest[len(rest)-1], `"'`), true
	}
	if len(rest) >= 1 && isMkdirHead(rest[0]) && len(rest) == 1 {
		return "", false
	}
	return "", false
}

func expandWindowsEnv(p string) string {
	p = os.ExpandEnv(p)
	return windowsEnvToken.ReplaceAllStringFunc(p, func(tok string) string {
		name := strings.Trim(tok, "%")
		if v, ok := os.LookupEnv(name); ok && v != "" {
			return v
		}
		return tok
	})
}

func writeUTF8PS1(body string) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp("", "lunitide-ps-*.ps1")
	if err != nil {
		return "", func() {}, err
	}
	path = f.Name()
	cleanup = func() { _ = os.Remove(path) }
	var b strings.Builder
	b.WriteString("\uFEFF")
	b.WriteString(powershellUTF8Preamble)
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteByte('\n')
	}
	_, err = f.WriteString(b.String())
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}

// wrapShellArgv rewrites powershell -Command / -EncodedCommand to
// powershell -File <utf8-bom .ps1> so cmd.exe / code page 936 cannot
// mojibake CJK. Non-PowerShell argv is returned unchanged.
func wrapShellArgv(argv []string) (out []string, cleanup func(), err error) {
	nop := func() {}
	if len(argv) == 0 || !isPowerShellExe(argv[0]) {
		return argv, nop, nil
	}
	body, ok := powershellCommandBody(argv)
	if !ok {
		return argv, nop, nil
	}
	path, cleanup, err := writeUTF8PS1(body)
	if err != nil {
		return nil, nop, err
	}
	exe := powershellExeFor(argv[0])
	return []string{exe, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", path}, cleanup, nil
}

func prepareCommandArgv(argv []string) ([]string, func(), error) {
	if runtime.GOOS != "windows" {
		return argv, func() {}, nil
	}
	return wrapShellArgv(argv)
}

func looksLikeGBKMojibake(s string) bool {
	return strings.Contains(s, "灏忓疂")
}

// decodeCommandOutput prefers UTF-8, then GBK/GB18030 (Windows OEM 936).
func decodeCommandOutput(raw []byte) string {
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	if len(raw) == 0 {
		return ""
	}
	if utf8.Valid(raw) {
		s := string(raw)
		if !looksLikeGBKMojibake(s) {
			return s
		}
	}
	if decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(raw); err == nil && utf8.Valid(decoded) {
		return string(decoded)
	}
	if utf8.Valid(raw) {
		return string(raw)
	}
	return strings.ToValidUTF8(string(raw), "\uFFFD")
}

func formatCommandOutput(ok bool, output string) string {
	output = strings.TrimRight(output, "\r\n")
	if ok {
		if output == "" {
			return "ok:true"
		}
		return "ok:true\n" + output
	}
	if output == "" {
		return "ok:false\ncommand failed"
	}
	if strings.HasPrefix(output, "ok:false") {
		return output
	}
	if strings.Contains(strings.ToLower(output), "command failed") {
		return "ok:false\n" + output
	}
	return "ok:false\ncommand failed: " + output
}

func commandFailure(output string) error {
	return fmt.Errorf("%s", formatCommandOutput(false, output))
}

