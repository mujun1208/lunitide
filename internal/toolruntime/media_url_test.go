package toolruntime

import (
	"encoding/json"
	"runtime"
	"strconv"
	"testing"
)

func TestExecuteMediaPlayRefusesNonHTTPURL(t *testing.T) {
	// The address is picked by the model. media.play hands it to the OS, so
	// anything wider than http(s) turns "play this" into "launch this" —
	// file: reaches an exe, the ms-* handlers reach diagnostic tooling, and a
	// UNC path reaches a remote binary.
	opened := ""
	openMediaURL = func(u string) error {
		opened = u
		return nil
	}
	t.Cleanup(func() { openMediaURL = openHTTPURL })

	for _, raw := range []string{
		`file:///C:/Windows/System32/calc.exe`,
		`javascript:alert(1)`,
		`vbscript:msgbox(1)`,
		`ms-msdt:/id PCWDiagnostic`,
		`search-ms:query=payload`,
		`\\attacker\share\payload.exe`,
		`C:\Windows\System32\calc.exe`,
		`https://`,
	} {
		args := json.RawMessage(`{"action":"open","url":` + strconv.Quote(raw) + `}`)
		if _, err := executeMediaPlay(args, true); err == nil {
			t.Fatalf("url %q was accepted; media.play must refuse it", raw)
		}
		if opened != "" {
			t.Fatalf("url %q reached the opener as %q", raw, opened)
		}
	}
}

func TestExecuteMediaPlayKeepsRealSearchURLIntact(t *testing.T) {
	// The scheme gate must not mangle the addresses media.play itself builds:
	// query strings carry & and # and have to survive byte for byte.
	opened := ""
	openMediaURL = func(u string) error {
		opened = u
		return nil
	}
	t.Cleanup(func() { openMediaURL = openHTTPURL })

	const want = "https://y.qq.com/n/ryqq/search?w=%E5%91%A8%E6%9D%B0%E4%BC%A6&t=song&p=1"
	args := json.RawMessage(`{"action":"open","url":` + strconv.Quote(want) + `}`)
	if _, err := executeMediaPlay(args, true); err != nil {
		t.Fatalf("legitimate https url rejected: %v", err)
	}
	if opened != want {
		t.Fatalf("opener saw %q; want the url unchanged: %q", opened, want)
	}
}

func TestMediaOpenArgvDoesNotUseAShell(t *testing.T) {
	// cmd /c start re-parses & | < > ^ in the address as command separators,
	// and Go only quotes an argv element when it holds a space or a quote. The
	// launcher therefore has to take the url as a plain argv, no shell.
	argv := mediaOpenArgv("https://example.com/a?x=1&y=2")
	if len(argv) == 0 {
		t.Fatal("mediaOpenArgv returned nothing")
	}
	if runtime.GOOS == "windows" {
		if argv[0] == "cmd" || argv[0] == "cmd.exe" {
			t.Fatalf("argv %q still launches through cmd", argv)
		}
	}
	last := argv[len(argv)-1]
	if last != "https://example.com/a?x=1&y=2" {
		t.Fatalf("url is not a standalone argv element; got %q in %q", last, argv)
	}
}
