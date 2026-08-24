package toolruntime

import (
	"path/filepath"
	"regexp"
	"strings"
)

// An unconditional refusal floor for command.run.
//
// Every other gate here can be switched off. Approval mode is a setting, the
// allowlist is lifted wholesale by the full-disk opt-in, and a companion
// voice turn upgrades itself to full access without asking anyone. That
// leaves nothing at all between a misheard sentence — or an instruction
// planted in a file the model happened to read — and a command that cannot
// be taken back. This floor is the one gate with no switch: it refuses
// whole-machine destruction regardless of mode, approval, or opt-in.
//
// It is deliberately narrow, because a false positive here breaks real work.
// `rm -rf node_modules` and `git clean -fdx` must keep running. A rule fires
// only when the target is a system root, or the operation destroys a disk,
// the backups, or the machine's availability.

// protectedRoots are paths whose recursive deletion is unrecoverable. Matching
// is exact after normalization, so `C:\Windows` is refused while
// `C:\Windows\Temp\build.log` is not.
var protectedRoots = map[string]bool{
	`\`: true, `/`: true, `~`: true, `$home`: true,
	`c:\windows`: true, `c:\windows\system32`: true, `c:\winnt`: true,
	`c:\program files`: true, `c:\program files (x86)`: true,
	`c:\programdata`: true, `c:\users`: true,
	`%userprofile%`: true, `%systemroot%`: true, `%windir%`: true, `%programfiles%`: true,
	`/usr`: true, `/etc`: true, `/bin`: true, `/sbin`: true, `/lib`: true, `/lib64`: true,
	`/boot`: true, `/var`: true, `/opt`: true, `/home`: true, `/root`: true,
	`/dev`: true, `/proc`: true, `/sys`: true, `/system`: true,
	// Registry hives reachable through PowerShell's provider paths.
	`hklm:`: true, `hklm:\`: true, `hklm:\system`: true, `hklm:\software`: true,
	`hklm:\sam`: true, `hklm:\security`: true,
}

// criticalRegistryKeys are `reg delete` targets that brick a Windows install.
// Ordinary application keys under HKLM\SOFTWARE\<vendor> stay allowed.
var criticalRegistryKeys = []string{
	`hklm\system`, `hklm\sam`, `hklm\security`,
	`hklm\software\microsoft\windows`, `hklm\software\microsoft\windows nt`,
	`hkey_local_machine\system`, `hkey_local_machine\sam`, `hkey_local_machine\security`,
}

// deleteHeads covers POSIX, cmd.exe and the PowerShell aliases that all reach
// the same destructive verb.
var deleteHeads = map[string]bool{
	"rm": true, "del": true, "erase": true, "rd": true, "rmdir": true,
	"remove-item": true, "ri": true, "unlink": true,
}

// powerHeads halt the machine out from under the user mid-task.
var powerHeads = map[string]bool{
	"stop-computer": true, "restart-computer": true,
	"poweroff": true, "halt": true, "reboot": true,
}

// forkBomb matches the classic `:(){ :|:& };:` and its whitespace variants.
var forkBomb = regexp.MustCompile(`:\s*\(\s*\)\s*\{[^}]*\|[^}]*&[^}]*\}\s*;\s*:`)

// statementSep splits a shell body into the individual commands it chains, so
// a destructive call hidden behind `cd /tmp && rm -rf /` is still inspected.
var statementSep = regexp.MustCompile(`\s*(?:&&|\|\||[;&|\n\r])\s*`)

// hardlineRefusal returns a human-readable reason when argv must never run,
// or "" when it may proceed to the normal permission checks.
func hardlineRefusal(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	if forkBomb.MatchString(strings.Join(argv, " ")) {
		return "fork bomb"
	}
	for _, cmd := range flattenCommands(argv) {
		if reason := refuseCommand(cmd); reason != "" {
			return reason
		}
	}
	return ""
}

// flattenCommands unwraps interpreter invocations into the commands they
// actually run. `powershell -Command "cd x; rm -rf C:\"` has to be seen as the
// `rm` it contains, not as a single opaque argument to powershell.exe.
func flattenCommands(argv []string) [][]string {
	rest := stripCmdPrefix(argv)
	if len(rest) == 0 {
		return nil
	}
	body, ok := interpreterBody(rest)
	if !ok {
		return [][]string{rest}
	}
	out := [][]string{rest}
	for _, statement := range statementSep.Split(body, -1) {
		if tokens := shellSplit(statement); len(tokens) > 0 {
			out = append(out, tokens)
		}
	}
	return out
}

// interpreterBody extracts the script text from `powershell -Command <body>`
// or `sh -c <body>`, the two shapes that carry a whole program in one argv
// entry.
func interpreterBody(argv []string) (string, bool) {
	if isPowerShellExe(argv[0]) {
		return powershellCommandBody(argv)
	}
	base := strings.ToLower(filepath.Base(strings.Trim(argv[0], `"`)))
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case "sh", "bash", "zsh", "dash", "ash":
		for i := 1; i < len(argv)-1; i++ {
			if argv[i] == "-c" {
				return argv[i+1], true
			}
		}
	}
	return "", false
}

// shellSplit is a quote-aware whitespace split. It only needs to be good
// enough to recover a command head and its path arguments.
func shellSplit(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t':
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// commandHead reduces an argv entry to a bare, comparable verb: `C:\Windows\
// System32\shutdown.exe` and `shutdown` are the same command.
func commandHead(token string) string {
	base := strings.ToLower(filepath.Base(strings.Trim(token, `"'`)))
	base = strings.ReplaceAll(base, "/", `\`)
	if i := strings.LastIndex(base, `\`); i >= 0 {
		base = base[i+1:]
	}
	return strings.TrimSuffix(base, ".exe")
}

// isProtectedRoot reports whether a path argument names a system root rather
// than something inside one. Switches need no special handling: protectedRoots
// is an exact-match set, so `/s` and `-rf` simply never match.
func isProtectedRoot(token string) bool {
	raw := strings.ReplaceAll(strings.ToLower(strings.Trim(token, `"'`)), "/", `\`)
	// `C:\*` and `/*` name the same destination as the root itself.
	p := strings.TrimRight(raw, `*.`)
	if len(p) > 1 {
		p = strings.TrimSuffix(p, `\`)
	}
	if p == "" || p == `\` {
		// A bare `*` is scoped to the working directory; only a wildcard
		// anchored at a root means the whole machine.
		return strings.HasPrefix(raw, `\`)
	}
	// A bare drive letter and its root are the same destination.
	if len(p) == 2 && p[1] == ':' {
		return true
	}
	if len(p) == 3 && p[1] == ':' && p[2] == '\\' {
		return true
	}
	if protectedRoots[p] {
		return true
	}
	// POSIX roots arrive with forward slashes that the fold above turned
	// into backslashes.
	return protectedRoots[strings.ReplaceAll(p, `\`, "/")]
}

func refuseCommand(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	head := commandHead(argv[0])
	args := argv[1:]
	lower := make([]string, len(args))
	for i, a := range args {
		lower[i] = strings.ToLower(strings.Trim(a, `"'`))
	}
	has := func(want string) bool {
		for _, a := range lower {
			if a == want {
				return true
			}
		}
		return false
	}
	hasPrefix := func(want string) bool {
		for _, a := range lower {
			if strings.HasPrefix(a, want) {
				return true
			}
		}
		return false
	}

	switch {
	case deleteHeads[head]:
		for _, a := range args {
			if isProtectedRoot(a) {
				return "recursive delete of a system root: " + a
			}
		}
	case head == "format":
		// Only the volume formatter, never `npm run format`, which never
		// reaches here as a command head.
		return "disk format"
	case head == "mkfs" || strings.HasPrefix(head, "mkfs."):
		return "filesystem creation over an existing volume"
	case head == "diskpart" || head == "wipefs":
		return "raw partition table edit"
	case head == "cipher" && has("/w"):
		return "free-space wipe"
	case head == "dd":
		for _, a := range lower {
			if target, ok := strings.CutPrefix(a, "of="); ok {
				if strings.HasPrefix(target, "/dev/") || strings.HasPrefix(target, `\\.\physicaldrive`) {
					return "raw write to a block device"
				}
			}
		}
	case head == "vssadmin" && has("delete") && hasPrefix("shadow"):
		return "shadow copy deletion"
	case head == "wbadmin" && has("delete"):
		return "backup catalog deletion"
	case head == "bcdedit" && (has("/delete") || has("/deletevalue")):
		return "boot configuration deletion"
	case head == "shutdown":
		for _, a := range lower {
			switch a {
			case "/s", "/r", "/p", "/h", "/g", "-s", "-r", "-p", "-h":
				return "system shutdown"
			}
		}
	case powerHeads[head]:
		return "system shutdown"
	case head == "init" && (has("0") || has("6")):
		return "system shutdown"
	case head == "reg" && has("delete"):
		for _, a := range lower {
			target := strings.ReplaceAll(a, "/", `\`)
			for _, key := range criticalRegistryKeys {
				if target == key {
					return "deletion of a critical registry hive: " + a
				}
			}
		}
	case head == "takeown" || head == "icacls":
		if has("/r") || has("/t") {
			for _, a := range args {
				if isProtectedRoot(a) {
					return "recursive ownership change of a system root: " + a
				}
			}
		}
	case head == "netsh" && has("advfirewall") && has("off"):
		return "firewall shutdown"
	case head == "set-mppreference" && hasPrefix("-disable"):
		return "antivirus shutdown"
	}
	return ""
}
