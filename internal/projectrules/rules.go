package projectrules

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	StartMark = "<!-- lunitide:project-rules:start -->"
	EndMark   = "<!-- lunitide:project-rules:end -->"
	MaxBody   = 16 * 1024
)

var ErrRulesFailed = errors.New("project rules materialize failed")

type Source struct {
	DeliverableID    string `json:"deliverableId"`
	AttachmentDigest string `json:"attachmentDigest"`
}

type Manifest struct {
	Version        int               `json:"version"`
	ProjectID      string            `json:"projectId"`
	Digest         string            `json:"digest"`
	Source         map[string]Source `json:"source"`
	MaterializedAt string            `json:"materializedAt"`
}

type Input struct {
	ProjectID   string
	DevStandard string
	TechStandard string
	BizStandard string
	Sources     map[string]Source
	At          string
}

func Materialize(root string, in Input) (Manifest, error) {
	if strings.TrimSpace(root) == "" {
		return Manifest{}, ErrRulesFailed
	}
	dir := filepath.Join(root, ".lunitide", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Manifest{}, ErrRulesFailed
	}
	dev := clip(in.DevStandard, MaxBody/2)
	tech := clip(in.TechStandard, MaxBody/2)
	biz := clip(in.BizStandard, MaxBody/2)
	if err := os.WriteFile(filepath.Join(dir, "dev-standard.md"), []byte(dev), 0o644); err != nil {
		return Manifest{}, ErrRulesFailed
	}
	if err := os.WriteFile(filepath.Join(dir, "tech-standard.md"), []byte(tech), 0o644); err != nil {
		return Manifest{}, ErrRulesFailed
	}
	if biz != "" {
		if err := os.WriteFile(filepath.Join(dir, "biz-standard.md"), []byte(biz), 0o644); err != nil {
			return Manifest{}, ErrRulesFailed
		}
	}
	sum := sha256.Sum256([]byte(dev + "\n" + tech + "\n" + biz))
	man := Manifest{
		Version: 1, ProjectID: in.ProjectID, Digest: hex.EncodeToString(sum[:]),
		Source: in.Sources, MaterializedAt: in.At,
	}
	if man.Source == nil {
		man.Source = map[string]Source{}
	}
	raw, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return Manifest{}, ErrRulesFailed
	}
	if err = os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o644); err != nil {
		return Manifest{}, ErrRulesFailed
	}
	if err = upsertAgents(root, dev, tech); err != nil {
		return Manifest{}, ErrRulesFailed
	}
	return man, nil
}

func LoadManifest(root string) (Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(root, ".lunitide", "rules", "manifest.json"))
	if err != nil {
		return Manifest{}, err
	}
	var man Manifest
	if json.Unmarshal(raw, &man) != nil || man.Version != 1 {
		return Manifest{}, ErrRulesFailed
	}
	return man, nil
}

func Guidance(root string) string {
	dev, _ := os.ReadFile(filepath.Join(root, ".lunitide", "rules", "dev-standard.md"))
	tech, _ := os.ReadFile(filepath.Join(root, ".lunitide", "rules", "tech-standard.md"))
	d := clip(string(dev), 4096)
	t := clip(string(tech), 4096)
	if d == "" && t == "" {
		return ""
	}
	return "\n\n[项目规范] 以下来自本项目已批准的开发/技术规范，作为后续阶段固定规则。写错请改阶段 1 文档后重新物化。\n\n开发规范：\n" + d + "\n\n技术规范：\n" + t + "\n"
}

func upsertAgents(root, dev, tech string) error {
	path := filepath.Join(root, "AGENTS.md")
	existing, _ := os.ReadFile(path)
	text := string(existing)
	block := StartMark + "\n# 本项目规范（月汐托管，勿手改本标记之间的内容；请改阶段 1 交付物后重新物化）\n\n" + clip(dev+"\n\n"+tech, MaxBody) + "\n" + EndMark + "\n"
	start := strings.Index(text, StartMark)
	end := strings.Index(text, EndMark)
	if start >= 0 && end > start {
		text = text[:start] + block + text[end+len(EndMark):]
		text = strings.TrimRight(text, "\n") + "\n"
	} else if text == "" {
		text = block
	} else {
		text = strings.TrimRight(text, "\n") + "\n\n" + block
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}
