// Package skillarchive reads pinned skill instructions without extracting or
// executing repository code. Archive and decompression budgets are independent.
package skillarchive

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/networkpolicy"
	"gopkg.in/yaml.v3"
)

const (
	MaxArchiveBytes  = 8 << 20
	MaxExpandedBytes = 32 << 20
	MaxFiles         = 4096
	MaxPromptBytes   = 48 << 10
)

var ErrInvalid = errors.New("skill import source invalid")
var ErrFetch = errors.New("skill import download failed")
var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var segmentPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

type Source struct{ URL, ArchiveURL, Commit, Directory, Publisher string }

// ParseSource accepts repository URLs or a directory URL pinned to the same SHA.
// All URL components are checked before a network request can be made.
func ParseSource(raw, commit string) (Source, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || !shaPattern.MatchString(commit) {
		return Source{}, fmt.Errorf("%w: 请提供 GitHub HTTPS 地址和完整的 40 位小写提交 SHA", ErrInvalid)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return Source{}, fmt.Errorf("%w: 仓库地址缺少拥有者或仓库名", ErrInvalid)
	}
	parts[1] = strings.TrimSuffix(parts[1], ".git")
	for _, p := range parts {
		if !segmentPattern.MatchString(p) || p == "." || p == ".." {
			return Source{}, fmt.Errorf("%w: 地址包含不支持的路径", ErrInvalid)
		}
	}
	dir := ""
	if len(parts) != 2 {
		if len(parts) < 5 || parts[2] != "tree" || parts[3] != commit {
			return Source{}, fmt.Errorf("%w: 子目录地址须使用 tree/同一提交SHA/目录", ErrInvalid)
		}
		dir = strings.Join(parts[4:], "/")
	}
	repo := "https://github.com/" + parts[0] + "/" + parts[1]
	canonical := repo
	if dir != "" {
		canonical += "/tree/" + commit + "/" + dir
	}
	return Source{URL: canonical, ArchiveURL: "https://codeload.github.com/" + parts[0] + "/" + parts[1] + "/zip/" + commit, Commit: commit, Directory: dir, Publisher: parts[0]}, nil
}

type Package struct {
	Source                                                   Source
	ArchiveHash, Name, Description, Version, Prompt, License string
	SkippedFiles                                             int
}

type FetchFunc func(context.Context, string, networkpolicy.FetchOptions) (networkpolicy.FetchResult, error)
type Loader struct{ Fetch FetchFunc }

func (l Loader) Load(ctx context.Context, raw, commit string) (Package, error) {
	src, err := ParseSource(raw, commit)
	if err != nil {
		return Package{}, err
	}
	fetch := l.Fetch
	if fetch == nil {
		fetch = networkpolicy.Fetch
	}
	res, err := fetch(ctx, src.ArchiveURL, networkpolicy.FetchOptions{MaxBodyBytes: MaxArchiveBytes, OverallTimeout: 12 * time.Second})
	if err != nil {
		return Package{}, fmt.Errorf("%w: %v", ErrFetch, err)
	}
	if res.Status != 200 || res.Truncated || len(res.Body) > MaxArchiveBytes || res.FinalURL != src.ArchiveURL {
		return Package{}, fmt.Errorf("%w: 下载状态 %d，归档上限为 8 MiB", ErrFetch, res.Status)
	}
	return Read(src, res.Body)
}

// Read validates all archive entries, but imports only the selected SKILL.md.
// Supporting code is counted for the review report and never installed.
func Read(src Source, archive []byte) (Package, error) {
	canonical, err := ParseSource(src.URL, src.Commit)
	if err != nil {
		return Package{}, err
	}
	src = canonical
	if len(archive) > MaxArchiveBytes {
		return Package{}, fmt.Errorf("%w: 归档过大", ErrInvalid)
	}
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil || len(zr.File) > MaxFiles {
		return Package{}, fmt.Errorf("%w: ZIP 无效或文件过多", ErrInvalid)
	}
	files := map[string]*zip.File{}
	root := ""
	var expanded uint64
	for _, f := range zr.File {
		n := strings.TrimSuffix(f.Name, "/")
		if n == "" || n == ".." || strings.ContainsAny(n, "\\:\x00") || strings.HasPrefix(n, "/") || path.Clean(n) != n || strings.HasPrefix(n, "../") || (!f.Mode().IsRegular() && !f.Mode().IsDir()) {
			return Package{}, fmt.Errorf("%w: ZIP 含不安全路径或链接", ErrInvalid)
		}
		parts := strings.SplitN(n, "/", 2)
		if root == "" {
			root = parts[0]
		}
		if root != parts[0] {
			return Package{}, fmt.Errorf("%w: ZIP 必须为单一仓库根目录", ErrInvalid)
		}
		if f.UncompressedSize64 > MaxExpandedBytes || expanded > MaxExpandedBytes-f.UncompressedSize64 {
			return Package{}, fmt.Errorf("%w: 解压内容超过 32 MiB", ErrInvalid)
		}
		expanded += f.UncompressedSize64
		if f.FileInfo().IsDir() {
			continue
		}
		if len(parts) != 2 {
			return Package{}, fmt.Errorf("%w: ZIP 缺少仓库目录", ErrInvalid)
		}
		key := strings.ToLower(parts[1])
		if _, exists := files[key]; exists {
			return Package{}, fmt.Errorf("%w: ZIP 含重复文件路径", ErrInvalid)
		}
		files[key] = f
	}
	entry := path.Join(src.Directory, "SKILL.md")
	f := files[strings.ToLower(entry)]
	if f == nil {
		return Package{}, fmt.Errorf("%w: 所选目录缺少 SKILL.md；子目录请使用固定提交的 tree 地址", ErrInvalid)
	}
	body, err := readEntry(f, MaxPromptBytes)
	if err != nil {
		return Package{}, err
	}
	name, description, prompt, declaredLicense, err := parseSkill(body)
	if err != nil {
		return Package{}, err
	}
	license := "unknown"
	for _, dir := range []string{src.Directory, ""} {
		for _, base := range []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "COPYING"} {
			if lf := files[strings.ToLower(path.Join(dir, base))]; lf != nil {
				lb, e := readEntry(lf, 128<<10)
				if e != nil {
					return Package{}, e
				}
				license = "LicenseRef-" + hash(lb)[:12]
				if declaredLicense != "" {
					license = declaredLicense
				}
				break
			}
		}
		if license != "unknown" {
			break
		}
	}
	skipped := 0
	prefix := strings.ToLower(src.Directory)
	if prefix != "" {
		prefix += "/"
	}
	for key := range files {
		if strings.HasPrefix(key, prefix) && key != strings.ToLower(entry) {
			skipped++
		}
	}
	pkg := Package{Source: src, ArchiveHash: hash(archive), Name: name, Description: description, Version: "0.0.0+" + src.Commit[:12], Prompt: prompt, License: license, SkippedFiles: skipped}
	if len(pkg.Attestation()) > 16384 {
		return Package{}, fmt.Errorf("%w: 技能来源摘要编码后超过 16 KiB，请缩短名称或描述", ErrInvalid)
	}
	return pkg, nil
}

func readEntry(f *zip.File, limit int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("%w: %s 超出大小限制", ErrInvalid, path.Base(f.Name))
	}
	r, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: ZIP 条目无法读取", ErrInvalid)
	}
	b, readErr := io.ReadAll(io.LimitReader(r, limit+1))
	closeErr := r.Close()
	if readErr != nil || closeErr != nil || int64(len(b)) > limit || !utf8.Valid(b) || bytes.ContainsRune(b, 0) {
		return nil, fmt.Errorf("%w: ZIP 条目损坏、过大或非 UTF-8 文本", ErrInvalid)
	}
	return b, nil
}

func parseSkill(body []byte) (name, description, prompt, license string, err error) {
	bad := func() (string, string, string, string, error) {
		return "", "", "", "", fmt.Errorf("%w: SKILL.md 需要有效 YAML name、description 和非空正文", ErrInvalid)
	}
	s := strings.ReplaceAll(strings.TrimPrefix(string(body), "\ufeff"), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return bad()
	}
	end := strings.Index(s[4:], "\n---\n")
	if end < 0 {
		return bad()
	}
	var doc yaml.Node
	if yaml.Unmarshal([]byte(s[4:4+end]), &doc) != nil || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return bad()
	}
	seen := map[string]bool{}
	for i := 0; i < len(doc.Content[0].Content); i += 2 {
		key, value := doc.Content[0].Content[i], doc.Content[0].Content[i+1]
		if key.Kind != yaml.ScalarNode || seen[key.Value] {
			return bad()
		}
		seen[key.Value] = true
		if key.Value == "name" || key.Value == "description" || key.Value == "license" {
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return bad()
			}
			switch key.Value {
			case "name":
				name = strings.TrimSpace(value.Value)
			case "description":
				description = strings.TrimSpace(value.Value)
			case "license":
				license = strings.TrimSpace(value.Value)
			}
		}
	}
	prompt = strings.TrimSpace(s[4+end+5:])
	if name == "" || len(name) > 128 || description == "" || len(description) > 4096 || prompt == "" || len(license) > 128 {
		return bad()
	}
	return
}

func hash(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

func (p Package) Attestation() string {
	b, _ := json.Marshal(map[string]any{"sourceUrl": p.Source.URL, "commit": p.Source.Commit, "archiveHash": p.ArchiveHash, "name": p.Name, "description": p.Description, "license": p.License, "promptHash": hash([]byte(p.Prompt)), "skippedFiles": p.SkippedFiles, "scope": "SKILL.md instructions only; repository scripts are not installed or executed"})
	return string(b)
}
