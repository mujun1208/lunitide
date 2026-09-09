// Package officerender converts preflighted Office snapshots in an owned,
// bounded worker. It never attaches to or terminates the user's Office process.
package officerender

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/commandworker"
)

var ErrUnavailable = errors.New("未检测到 LibreOffice，可继续查看结构预览或用本机软件打开")

type Capability struct {
	Available bool   `json:"available"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Notice    string `json:"notice"`
}

type Result struct {
	PDF             []byte          `json:"-"`
	UpdatedOffice   []byte          `json:"-"` // Native candidate only; never publish without selective merge.
	SHA256          string          `json:"sha256"`
	Renderer        string          `json:"renderer"`
	RendererVersion string          `json:"rendererVersion"`
	SourceDigest    string          `json:"sourceDigest"`
	Notice          string          `json:"notice"`
	Native          *NativeEvidence `json:"native,omitempty"`
}

type Runner func(context.Context, commandworker.Spec, commandworker.StartGuard, func([]byte)) (commandworker.Outcome, error)

type Renderer struct {
	Root string
	// Preflight must reject active content and external relationships. A fresh
	// profile/headless mode is resource isolation, not a security sandbox.
	Preflight  func(kind string, data []byte) error
	Executable string
	Run        Runner
	ListFonts  func(context.Context) (FontInventory, error)
}

func detect() string {
	names := []string{"soffice"}
	if runtime.GOOS == "windows" {
		names = []string{"soffice.com"}
	}
	for _, name := range names {
		if p, err := exec.LookPath(name); err == nil && filepath.IsAbs(p) {
			return p
		}
	}
	for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		if base == "" {
			continue
		}
		p := filepath.Join(base, "LibreOffice", "program", "soffice.com")
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p
		}
	}
	if runtime.GOOS == "darwin" {
		p := "/Applications/LibreOffice.app/Contents/MacOS/soffice"
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func (r *Renderer) runner() Runner {
	if r.Run != nil {
		return r.Run
	}
	return commandworker.Run
}
func (r *Renderer) executable() string {
	if r.Executable != "" {
		return r.Executable
	}
	return detect()
}

func environment(root string) []string {
	env := []string{"TEMP=" + root, "TMP=" + root, "TMPDIR=" + root}
	// Do not pass model keys or the full engine environment to converters.
	for _, key := range []string{"SYSTEMROOT", "WINDIR", "PATH", "LANG", "LC_ALL", "HOME", "USERPROFILE"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func (r *Renderer) Probe(ctx context.Context) Capability {
	c := Capability{Name: "LibreOffice", Notice: "结构检查可用；实际排版与目标软件兼容性尚未验证"}
	exe := r.executable()
	if exe == "" {
		c.Notice = ErrUnavailable.Error()
		return c
	}
	if err := os.MkdirAll(r.Root, 0700); err != nil {
		c.Notice = "无法准备排版检查目录"
		return c
	}
	var output bytes.Buffer
	out, err := r.runner()(ctx, commandworker.Spec{Exe: exe, Args: []string{"--version"}, Dir: r.Root, Env: environment(r.Root), Timeout: 15 * time.Second, MaxOutputBytes: 4096, MaxMemoryBytes: 512 << 20}, nil, func(p []byte) { output.Write(p) })
	if err != nil || out.ExitCode != 0 || out.TimedOut || out.Truncated {
		c.Notice = "检测到排版组件，但启动检查失败"
		return c
	}
	version := strings.TrimSpace(output.String())
	if !strings.Contains(strings.ToLower(version), "libreoffice") {
		c.Notice = "排版组件身份无法确认"
		return c
	}
	c.Available = true
	if len(version) > 256 {
		version = version[:256]
	}
	c.Version = version
	c.Notice = "可执行实际 PDF 排版检查；不代表已验证 Microsoft Office/WPS 或全部公式"
	return c
}

func (r *Renderer) Render(ctx context.Context, kind string, data []byte) (Result, error) {
	if kind != "pptx" && kind != "docx" && kind != "xlsx" {
		return Result{}, errors.New("此格式不支持 Office 排版转换")
	}
	if len(data) == 0 || len(data) > 32<<20 {
		return Result{}, errors.New("排版输入为空或超过 32 MiB")
	}
	if r.Preflight == nil {
		return Result{}, errors.New("排版安全预检未初始化")
	}
	if err := r.Preflight(kind, data); err != nil {
		return Result{}, err
	}
	c := r.Probe(ctx)
	if !c.Available {
		return Result{}, fmt.Errorf("%w：%s", ErrUnavailable, c.Notice)
	}
	job, err := os.MkdirTemp(r.Root, "render-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(job) // Only the private directory returned by MkdirTemp.
	profile := filepath.Join(job, "profile")
	output := filepath.Join(job, "output")
	for _, dir := range []string{filepath.Join(profile, "user"), output} {
		if err = os.MkdirAll(dir, 0700); err != nil {
			return Result{}, err
		}
	}
	// Highest macro security complements the mandatory active-content preflight.
	settings := `<?xml version="1.0" encoding="UTF-8"?><oor:items xmlns:oor="http://openoffice.org/2001/registry"><item oor:path="/org.openoffice.Office.Common/Security/Scripting"><prop oor:name="MacroSecurityLevel" oor:op="fuse"><value>3</value></prop></item></oor:items>`
	if err = os.WriteFile(filepath.Join(profile, "user", "registrymodifications.xcu"), []byte(settings), 0600); err != nil {
		return Result{}, err
	}
	input := filepath.Join(job, "source."+kind)
	if err = os.WriteFile(input, data, 0600); err != nil {
		return Result{}, err
	}
	profilePath := filepath.ToSlash(profile)
	if !strings.HasPrefix(profilePath, "/") {
		profilePath = "/" + profilePath
	}
	profileURL := (&url.URL{Scheme: "file", Path: profilePath}).String()
	args := []string{"-env:UserInstallation=" + profileURL, "--headless", "--nologo", "--nodefault", "--norestore", "--convert-to", "pdf", "--outdir", output, input}
	out, err := r.runner()(ctx, commandworker.Spec{Exe: r.executable(), Args: args, Dir: job, Env: environment(job), Timeout: 90 * time.Second, MaxMemoryBytes: 1 << 30, MaxOutputBytes: 16384}, nil, nil)
	if err != nil {
		return Result{}, err
	}
	if out.TimedOut {
		return Result{}, errors.New("排版检查超时，原文件已保留，可以重试")
	}
	if out.ExitCode != 0 {
		return Result{}, errors.New("排版组件转换失败，原文件已保留")
	}
	f, err := os.Open(filepath.Join(output, "source.pdf"))
	if err != nil {
		return Result{}, errors.New("转换未产生 PDF，不能视为检查通过")
	}
	defer f.Close()
	pdf, err := io.ReadAll(io.LimitReader(f, 32<<20+1))
	if err != nil {
		return Result{}, err
	}
	if len(pdf) > 32<<20 || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		return Result{}, errors.New("排版输出无效或超出限制")
	}
	h := sha256.Sum256(pdf)
	src := sha256.Sum256(data)
	return Result{PDF: pdf, SHA256: hex.EncodeToString(h[:]), SourceDigest: hex.EncodeToString(src[:]), Renderer: c.Name, RendererVersion: c.Version, Notice: "已实际导出 PDF；字体替换、目标软件兼容性与完整公式重算仍需独立验证"}, nil
}
