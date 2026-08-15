package command

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func injectionSpec() CommandSpec {
	return CommandSpec{
		ID: "spec-echo", Name: "echo", Description: "echo text to a file",
		ArgvTemplate: []string{"cmd.exe", "/c", "echo", "{text}", "{file}"},
		ParamSchema: map[string]ParamSpec{
			"text": {Type: ParamString, Required: true, MaxLen: 200},
			"file": {Type: ParamPath, Required: true},
		},
		EnvAllowlist:   []string{"PATH"},
		CwdPolicy:      CwdPolicyWorkspace,
		TimeoutMsUpper: 30000, Version: "1",
	}
}

func TestArgvInjection(t *testing.T) {
	root := t.TempDir()
	spec := injectionSpec()

	// A hostile value stays one literal argv entry: no shell ever sees it,
	// so "& whoami" cannot spawn a second process.
	args := map[string]string{"text": "calc.exe & whoami", "file": "notes.txt"}
	argv, err := RenderArgv(spec, args)
	if err != nil {
		t.Fatalf("RenderArgv: %v", err)
	}
	if len(argv) != 5 {
		t.Fatalf("injection must not split argv, got %d entries: %q", len(argv), argv)
	}
	if argv[3] != "calc.exe & whoami" {
		t.Fatalf("hostile value must stay one literal entry, got %q", argv[3])
	}

	// The same value passes full validation (it is just data).
	in := StartInput{
		Spec: spec, Args: args, Env: map[string]string{"PATH": "p"},
		Cwd: filepath.Join(root, "sub"), WorkspaceRoot: root,
	}
	if err := ValidateStart(in); err != nil {
		t.Fatalf("valid start rejected: %v", err)
	}

	// Path traversal in a path parameter is refused with field location.
	bad := StartInput{
		Spec: spec, Args: map[string]string{"text": "ok", "file": `..\..\escape`},
		Env: map[string]string{"PATH": "p"}, Cwd: filepath.Join(root, "sub"), WorkspaceRoot: root,
	}
	err = ValidateStart(bad)
	if !errors.Is(err, ErrParamInvalid) {
		t.Fatalf("path traversal want ErrParamInvalid, got %v", err)
	}
	if !strings.Contains(err.Error(), "file") {
		t.Fatalf("CMD-002 must locate the offending field, got %v", err)
	}

	// An environment key outside the allowlist is refused with the key.
	envy := StartInput{
		Spec: spec, Args: args, Env: map[string]string{"PATH": "p", "EVIL": "1"},
		Cwd: filepath.Join(root, "sub"), WorkspaceRoot: root,
	}
	err = ValidateStart(envy)
	if !errors.Is(err, ErrEnvNotAllowed) {
		t.Fatalf("env key want ErrEnvNotAllowed, got %v", err)
	}
	if !strings.Contains(err.Error(), "EVIL") {
		t.Fatalf("CMD-002 must name the offending key, got %v", err)
	}

	// A cwd escaping the workspace root via .. is refused.
	esc := StartInput{
		Spec: spec, Args: args, Env: map[string]string{"PATH": "p"},
		Cwd: root + `\..\..\Windows`, WorkspaceRoot: root,
	}
	err = ValidateStart(esc)
	if !errors.Is(err, ErrCwdOutsideWorkspace) {
		t.Fatalf("cwd escape want ErrCwdOutsideWorkspace, got %v", err)
	}

	// A template placeholder without a supplied value is refused.
	tpl := CommandSpec{
		ArgvTemplate: []string{"tool", "--flag={nope}"},
		ParamSchema:  map[string]ParamSpec{"a": {Type: ParamString}},
	}
	if _, err := RenderArgv(tpl, map[string]string{"a": "v"}); !errors.Is(err, ErrTemplateUnknown) {
		t.Fatalf("unknown placeholder want ErrTemplateUnknown, got %v", err)
	}
}

func TestValidateStart(t *testing.T) {
	root := t.TempDir()
	spec := CommandSpec{
		ID: "spec-tool", Name: "tool", Description: "typed parameter probe",
		ArgvTemplate: []string{"tool", "--lines={count}", "--color={color}", "--label={label}", "{target}"},
		ParamSchema: map[string]ParamSpec{
			"count":  {Type: ParamInt, Required: true},
			"color":  {Type: ParamBool},
			"label":  {Type: ParamString, MaxLen: 16},
			"target": {Type: ParamPath, Required: true},
		},
		EnvAllowlist: []string{"PATH", "LANG"}, CwdPolicy: CwdPolicyWorkspace,
	}
	good := StartInput{
		Spec: spec,
		Args: map[string]string{"count": "3", "color": "true", "label": "run", "target": "src/main.go"},
		Env:  map[string]string{"PATH": "p", "LANG": "en"},
		Cwd:  root, WorkspaceRoot: root, // the root itself is allowed
	}
	if err := ValidateStart(good); err != nil {
		t.Fatalf("valid start rejected: %v", err)
	}
	argv, err := RenderArgv(spec, good.Args)
	if err != nil {
		t.Fatalf("RenderArgv: %v", err)
	}
	want := []string{"tool", "--lines=3", "--color=true", "--label=run", "src/main.go"}
	if len(argv) != len(want) {
		t.Fatalf("argv len %d, want %d: %q", len(argv), len(want), argv)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv[%d]=%q, want %q", i, argv[i], want[i])
		}
	}

	cases := []struct {
		name string
		args map[string]string
		want string // substring expected in the error message
	}{
		{"missing required", map[string]string{"count": "3", "color": "true", "label": "run"}, "target"},
		{"unknown param", map[string]string{"count": "3", "color": "true", "label": "run", "target": "a.go", "nope": "x"}, "nope"},
		{"int garbage", map[string]string{"count": "3.5", "color": "true", "label": "run", "target": "a.go"}, "count"},
		{"bool garbage", map[string]string{"count": "3", "color": "yes", "label": "run", "target": "a.go"}, "color"},
		{"string too long", map[string]string{"count": "3", "color": "true", "label": "0123456789abcdef0", "target": "a.go"}, "label"},
		{"absolute path", map[string]string{"count": "3", "color": "true", "label": "run", "target": `C:\Windows\system32`}, "target"},
	}
	for _, tc := range cases {
		in := good
		in.Args = tc.args
		err := ValidateStart(in)
		if !errors.Is(err, ErrParamInvalid) {
			t.Fatalf("%s: want ErrParamInvalid, got %v", tc.name, err)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: error must name field %q, got %v", tc.name, tc.want, err)
		}
	}
}
