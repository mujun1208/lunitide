package projecttree

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

var ErrInvalid = errors.New("project tree is invalid")

type Tree struct {
	Version  int               `json:"version"`
	Dirs     []string          `json:"dirs"`
	PhaseMap map[string]string `json:"phaseMap"`
	CodeRoot string            `json:"codeRoot"`
}

type Receipt struct {
	Created  []string          `json:"created"`
	Existed  []string          `json:"existed"`
	Failed   map[string]string `json:"failed,omitempty"`
	Skipped  []string          `json:"skipped,omitempty"`
	Digest   string            `json:"digest"`
	TreeFile string            `json:"treeFile"`
}

func Parse(raw []byte) (Tree, error) {
	var tree Tree
	if err := json.Unmarshal(raw, &tree); err != nil {
		return Tree{}, ErrInvalid
	}
	return Normalize(tree)
}

func Default(operations bool) (Tree, error) {
	if operations {
		return Normalize(Tree{
			Version: 1,
			Dirs: []string{
				"docs/01-需求架构规范",
				"docs/03-数据库",
				"docs/04-接口",
				"docs/05-开发",
				"docs/06-测试",
				"docs/08-发布",
				"src",
				"tests",
				"deploy",
			},
			PhaseMap: map[string]string{
				"1": "docs/01-需求架构规范",
				"2": "docs/03-数据库",
				"3": "docs/04-接口",
				"4": "src",
				"5": "docs/06-测试",
				"6": "docs/08-发布",
			},
			CodeRoot: "src",
		})
	}
	return Normalize(Tree{
		Version: 1,
		Dirs: []string{
			"docs/01-需求架构规范",
			"docs/02-方案和UI设计",
			"docs/03-数据库",
			"docs/04-接口",
			"docs/05-开发",
			"docs/06-测试",
			"docs/07-集成",
			"docs/08-发布",
			"src",
			"tests",
			"deploy",
		},
		PhaseMap: map[string]string{
			"1": "docs/01-需求架构规范",
			"2": "docs/02-方案和UI设计",
			"3": "docs/03-数据库",
			"4": "docs/04-接口",
			"5": "src",
			"6": "docs/06-测试",
			"7": "docs/07-集成",
			"8": "docs/08-发布",
		},
		CodeRoot: "src",
	})
}

func Normalize(tree Tree) (Tree, error) {
	if tree.Version != 1 {
		return Tree{}, ErrInvalid
	}
	if tree.CodeRoot == "" {
		tree.CodeRoot = "src"
	}
	if err := validRel(tree.CodeRoot); err != nil {
		return Tree{}, err
	}
	seen := map[string]bool{}
	var dirs []string
	for _, d := range tree.Dirs {
		d = strings.TrimSpace(strings.ReplaceAll(d, "\\", "/"))
		if d == "" {
			continue
		}
		if err := validRel(d); err != nil {
			return Tree{}, err
		}
		if seen[d] {
			continue
		}
		seen[d] = true
		dirs = append(dirs, d)
	}
	if !seen[tree.CodeRoot] {
		return Tree{}, ErrInvalid
	}
	if len(tree.PhaseMap) == 0 {
		return Tree{}, ErrInvalid
	}
	for _, v := range tree.PhaseMap {
		v = strings.TrimSpace(strings.ReplaceAll(v, "\\", "/"))
		if err := validRel(v); err != nil {
			return Tree{}, err
		}
		if !seen[v] && !hasPrefixDir(seen, v) {
			return Tree{}, ErrInvalid
		}
	}
	tree.Dirs = dirs
	return tree, nil
}

func hasPrefixDir(seen map[string]bool, path string) bool {
	for d := range seen {
		if path == d || strings.HasPrefix(path, d+"/") {
			return true
		}
	}
	return false
}

func validRel(p string) error {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "..") {
		return ErrInvalid
	}
	if len(p) >= 2 && unicode.IsLetter(rune(p[0])) && p[1] == ':' {
		return ErrInvalid
	}
	if strings.HasPrefix(p, "//") || strings.HasPrefix(strings.ToLower(p), "\\\\") {
		return ErrInvalid
	}
	if filepath.IsAbs(p) {
		return ErrInvalid
	}
	return nil
}

func Digest(tree Tree) (string, error) {
	body, err := json.Marshal(tree)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func Materialize(root string, tree Tree) (Receipt, error) {
	tree, err := Normalize(tree)
	if err != nil {
		return Receipt{}, err
	}
	digest, err := Digest(tree)
	if err != nil {
		return Receipt{}, err
	}
	receipt := Receipt{Digest: digest, Failed: map[string]string{}}
	for _, d := range tree.Dirs {
		abs := filepath.Join(root, filepath.FromSlash(d))
		info, statErr := os.Stat(abs)
		if statErr == nil && info.IsDir() {
			receipt.Existed = append(receipt.Existed, d)
			continue
		}
		if err = os.MkdirAll(abs, 0o755); err != nil {
			receipt.Failed[d] = err.Error()
			continue
		}
		receipt.Created = append(receipt.Created, d)
	}
	if len(receipt.Failed) > 0 {
		return receipt, fmt.Errorf("%w: %d dirs failed", errFailed, len(receipt.Failed))
	}
	meta := filepath.Join(root, ".lunitide")
	if err = os.MkdirAll(meta, 0o755); err != nil {
		return receipt, err
	}
	body, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return receipt, err
	}
	treeFile := filepath.Join(meta, "project-tree.json")
	if err = os.WriteFile(treeFile, body, 0o644); err != nil {
		return receipt, err
	}
	recBody, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return receipt, err
	}
	if err = os.WriteFile(filepath.Join(meta, "tree-receipt.json"), recBody, 0o644); err != nil {
		return receipt, err
	}
	receipt.TreeFile = treeFile
	return receipt, nil
}

var errFailed = errors.New("project tree materialize failed")

func IsFailed(err error) bool { return errors.Is(err, errFailed) }

func SafeFileName(raw, fallback string) string {
	raw = strings.TrimSpace(raw)
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r < 32 || strings.ContainsRune(`<>:"/\|?*`, r):
			b.WriteByte('-')
		default:
			b.WriteRune(r)
		}
	}
	name := strings.Trim(b.String(), " .")
	if name == "" {
		return fallback
	}
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:80])
	}
	return name
}

func ExportCopy(root, relDir, fileName string, content []byte) (string, error) {
	if root == "" || fileName == "" {
		return "", ErrInvalid
	}
	dir := filepath.Join(root, filepath.FromSlash(relDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	candidate := filepath.Join(dir, fileName)
	if _, err := os.Stat(candidate); err == nil {
		ext := filepath.Ext(fileName)
		stem := strings.TrimSuffix(fileName, ext)
		for n := 2; n < 1000; n++ {
			candidate = filepath.Join(dir, fmt.Sprintf("%s-v%d%s", stem, n, ext))
			if _, err = os.Stat(candidate); os.IsNotExist(err) {
				break
			}
		}
	}
	return candidate, os.WriteFile(candidate, content, 0o644)
}
