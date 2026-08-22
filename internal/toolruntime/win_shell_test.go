package toolruntime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestExtractMkdirPathCJK(t *testing.T) {
	t.Parallel()
	want := filepath.Join("C:", "Users", "mu", "Desktop", "小宝")
	cases := [][]string{
		{"mkdir", want},
		{"md", want},
		{"cmd", "/c", "mkdir", want},
		{"cmd.exe", "/d", "/s", "/c", "mkdir", want},
		{"powershell", "-NoProfile", "-Command", "New-Item -ItemType Directory -Force -Path '" + want + "'"},
		{"powershell.exe", "-Command", `New-Item -Path "` + want + `" -ItemType Directory`},
	}
	for _, argv := range cases {
		got, ok := extractMkdirPath(argv)
		if !ok || got != want {
			t.Fatalf("argv=%v got=%q ok=%v", argv, got, ok)
		}
	}
	if _, ok := extractMkdirPath([]string{"go", "version"}); ok {
		t.Fatal("go version is not mkdir")
	}
	if _, ok := extractMkdirPath([]string{"powershell", "-Command", "[Environment]::GetFolderPath('Desktop') | New-Item -ItemType Directory"}); ok {
		t.Fatal("expression paths must go through the UTF-8 script wrap, not native mkdir")
	}
}

func TestWrapShellArgvWritesUTF8PS1WithCJK(t *testing.T) {
	t.Parallel()
	argv := []string{"powershell", "-NoProfile", "-Command", `New-Item -ItemType Directory -Force -Path ([Environment]::GetFolderPath('Desktop') + '\小宝')`}
	out, cleanup, err := wrapShellArgv(argv)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(out) < 2 || out[len(out)-2] != "-File" {
		t.Fatalf("expected -File wrap, got %v", out)
	}
	ps1 := out[len(out)-1]
	raw, err := os.ReadFile(ps1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) {
		t.Fatal("ps1 must start with UTF-8 BOM")
	}
	body := string(raw)
	if !strings.Contains(body, "小宝") {
		t.Fatalf("CJK name lost in wrap: %q", body)
	}
	if strings.Contains(body, "灏忓疂") {
		t.Fatal("wrapped script contains GBK mojibake of 小宝")
	}
	if !strings.Contains(body, "chcp 65001") || !strings.Contains(body, "UTF8Encoding") {
		t.Fatalf("missing UTF-8 preamble: %s", body)
	}
	joined := strings.Join(out, " ")
	if strings.Contains(joined, "-Command") {
		t.Fatalf("-Command must not remain on the rewritten argv: %v", out)
	}
}

func TestWrapShellArgvEncodedCommand(t *testing.T) {
	t.Parallel()
	script := "Write-Output 测试"
	u := utf16.Encode([]rune(script))
	raw := make([]byte, len(u)*2)
	for i, r := range u {
		raw[i*2] = byte(r)
		raw[i*2+1] = byte(r >> 8)
	}
	argv := []string{"powershell", "-EncodedCommand", base64.StdEncoding.EncodeToString(raw)}
	out, cleanup, err := wrapShellArgv(argv)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	body, err := os.ReadFile(out[len(out)-1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "测试") {
		t.Fatalf("encoded command lost CJK: %q", body)
	}
}

func TestDecodeCommandOutputGBKAndUTF8(t *testing.T) {
	t.Parallel()
	utf8Name := "小宝"
	if got := decodeCommandOutput([]byte(utf8Name)); got != utf8Name {
		t.Fatalf("utf8=%q", got)
	}
	gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(utf8Name))
	if err != nil {
		t.Fatal(err)
	}
	if utf8.Valid(gbk) {
		t.Fatal("fixture must be raw GBK, not UTF-8")
	}
	if got := decodeCommandOutput(gbk); got != utf8Name {
		t.Fatalf("gbk decode=%q want %q", got, utf8Name)
	}
	mojibake, err := simplifiedchinese.GBK.NewDecoder().Bytes([]byte(utf8Name))
	if err != nil {
		t.Fatal(err)
	}
	if string(mojibake) != "灏忓疂" {
		t.Fatalf("mojibake fixture=%q", mojibake)
	}
}

func TestFormatCommandOutputMarksFailure(t *testing.T) {
	t.Parallel()
	if got := formatCommandOutput(true, "created directory: 小宝"); !strings.HasPrefix(got, "ok:true\n") || !strings.Contains(got, "小宝") {
		t.Fatalf("ok=%q", got)
	}
	if got := formatCommandOutput(false, "command failed: New-Item"); !strings.HasPrefix(got, "ok:false\n") || !strings.Contains(got, "New-Item") {
		t.Fatalf("fail=%q", got)
	}
}

func TestCommandRunCreatesCJKDirectoryExactName(t *testing.T) {
	root := t.TempDir()
	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err = r.SetCommandPolicyJSON([]byte(`{"commands":[],"fullAccess":true}`)); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "测试", "小宝")
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	args, err := json.Marshal(map[string]any{"argv": []string{"mkdir", target}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.ExecuteUnconfined(context.Background(), session, "command.run", args, false)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if !strings.Contains(out.Output, "ok:true") || strings.Contains(out.Output, "灏忓疂") {
		t.Fatalf("output=%q", out.Output)
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		t.Fatalf("stat %s: %v", target, err)
	}
	parent := filepath.Dir(target)
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "小宝" {
		t.Fatalf("directory names=%v (mojibake would be 灏忓疂)", names)
	}
}

func TestCommandRunFailureIsOkFalse(t *testing.T) {
	root := t.TempDir()
	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err = r.SetCommandPolicyJSON([]byte(`{"commands":[],"fullAccess":true}`)); err != nil {
		t.Fatal(err)
	}
	_, err = r.ExecuteUnconfined(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", "command.run", []byte(`{"argv":["cmd","/c","echo","boom","&&","exit","2"]}`), false)
	if err == nil {
		t.Fatal("expected failure")
	}
	if !strings.Contains(err.Error(), "ok:false") {
		t.Fatalf("failure must be ok:false, got %v", err)
	}
}
