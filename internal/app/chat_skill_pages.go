package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/jsonutil"
	"github.com/lunitide/lunitide/internal/skillapp"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

const skillReadPageBytes = 3800
const skillInvocationMaxBytes = 96 << 10

type skillReadPage struct {
	SkillID    string `json:"skillId"`
	Label      string `json:"label"`
	Version    string `json:"version"`
	Revision   int64  `json:"revision"`
	Source     string `json:"source"`
	RootPath   string `json:"rootPath,omitempty"`
	Path       string `json:"path,omitempty"`
	Digest     string `json:"digest"`
	Offset     int    `json:"offset"`
	NextOffset int    `json:"nextOffset"`
	TotalRunes int    `json:"totalRunes"`
	HasMore    bool   `json:"hasMore"`
	Text       string `json:"text"`
	Notice     string `json:"notice"`
}

func (e *Engine) invokeSkillViewTool(ctx context.Context, args json.RawMessage) (toolruntime.Result, error) {
	if err := e.CheckCapability(ctx, "skills"); err != nil {
		return toolruntime.Result{}, err
	}
	if !skillServiceAvailable(e.skills) {
		return toolruntime.Result{}, errors.New("skill service unavailable")
	}
	var a struct {
		SkillID        string `json:"skillId"`
		Path           string `json:"path"`
		Offset         int    `json:"offset"`
		ExpectedDigest string `json:"expectedDigest"`
	}
	if json.Unmarshal(jsonutil.Repair(args), &a) != nil || len(a.SkillID) == 0 || len(a.SkillID) > 128 || len(a.Path) > 256 || a.Offset < 0 {
		return toolruntime.Result{}, errors.New("invalid skill.view arguments")
	}
	if a.Offset > 0 && a.ExpectedDigest == "" {
		return toolruntime.Result{}, errors.New("分页续读必须携带上一页 digest 作为 expectedDigest，避免拼接不同版本的技能")
	}
	if a.ExpectedDigest != "" {
		if b, err := hex.DecodeString(a.ExpectedDigest); err != nil || len(b) != 32 {
			return toolruntime.Result{}, errors.New("invalid skill.view expectedDigest")
		}
	}
	p, body, identity, err := e.skillReadSource(ctx, strings.TrimSpace(a.SkillID), strings.TrimSpace(a.Path))
	if err != nil {
		return toolruntime.Result{}, err
	}
	digest := sha256.Sum256([]byte(identity + "\x00" + body))
	p.Digest = hex.EncodeToString(digest[:])
	if a.ExpectedDigest != "" && a.ExpectedDigest != p.Digest {
		return toolruntime.Result{}, errors.New("SKILL_SOURCE_CHANGED：技能版本或参考文件已变化；请从 offset=0 重新读取，不能拼接旧页")
	}
	runes := []rune(body)
	p.TotalRunes = len(runes)
	p.Offset = a.Offset
	if p.Offset > len(runes) {
		return toolruntime.Result{}, errors.New("skill.view offset exceeds source length")
	}
	p.Notice = "hasMore=true 时以 nextOffset 和 expectedDigest=digest 继续 skill.view；完整读取后再按技能执行。"
	// Search against encoded JSON bytes, including escaping and cursor fields.
	// The complete page always survives the shared 4 KiB model-result budget.
	low, high := p.Offset, min(len(runes), p.Offset+3800)
	var best []byte
	for low <= high {
		end := low + (high-low)/2
		p.NextOffset = end
		p.HasMore = end < len(runes)
		p.Text = string(runes[p.Offset:end])
		raw, marshalErr := json.Marshal(p)
		if marshalErr != nil {
			return toolruntime.Result{}, marshalErr
		}
		if len(raw) <= skillReadPageBytes {
			best = raw
			low = end + 1
		} else {
			high = end - 1
		}
	}
	if len(best) == 0 {
		return toolruntime.Result{}, errors.New("skill source metadata exceeds page budget")
	}
	var checked skillReadPage
	_ = json.Unmarshal(best, &checked)
	if checked.HasMore && checked.NextOffset <= p.Offset {
		return toolruntime.Result{}, errors.New("skill source page cannot make progress")
	}
	return toolruntime.Result{Output: string(best)}, nil
}

func (e *Engine) assembleSkillViewForModel(ctx context.Context, firstArgs json.RawMessage, firstOutput string) string {
	var page skillReadPage
	if json.Unmarshal([]byte(firstOutput), &page) != nil || !page.HasMore {
		return firstOutput
	}
	var text strings.Builder
	text.WriteString(page.Text)
	digest := page.Digest
	offset := page.NextOffset
	skillID := page.SkillID
	path := page.Path
	for hops := 0; hops < 40 && page.HasMore; hops++ {
		if text.Len() > skillInvocationMaxBytes {
			break
		}
		args, err := json.Marshal(map[string]any{
			"skillId": skillID, "path": path, "offset": offset, "expectedDigest": digest,
		})
		if err != nil {
			break
		}
		out, err := e.invokeSkillViewTool(ctx, args)
		if err != nil {
			break
		}
		if json.Unmarshal([]byte(out.Output), &page) != nil {
			break
		}
		text.WriteString(page.Text)
		offset = page.NextOffset
		digest = page.Digest
	}
	page.Text = text.String()
	page.Offset = 0
	page.NextOffset = utf8.RuneCountInString(page.Text)
	if page.TotalRunes > 0 && page.NextOffset >= page.TotalRunes {
		page.HasMore = false
		page.Notice = "已一次读完技能正文，无需再分页 skill.view。"
	} else if page.HasMore {
		page.Notice = "正文过长，已尽量一次读完；hasMore=true 时从 nextOffset 续读。"
	}
	raw, err := json.Marshal(page)
	if err != nil || len(raw) > skillInvocationMaxBytes+4096 {
		return firstOutput
	}
	return string(raw)
}

func (e *Engine) skillReadSource(ctx context.Context, id, path string) (skillReadPage, string, string, error) {
	p := skillReadPage{SkillID: id, Path: path}
	var body, manifest, folderKey string
	var folderKeys []string
	var packageSkill skill.Skill
	if validCanonicalULID(id) {
		sk, err := e.skills.Get(ctx, id)
		if err != nil {
			return p, "", "", err
		}
		if sk == nil {
			return p, "", "", errors.New("skill not found")
		}
		p.Label = skillViewLabel(*sk)
		p.Version = sk.Version
		p.Revision = sk.Rev
		p.Source = "installed"
		manifest = sk.ManifestJSON
		packageSkill = *sk
		if e.persistDir != "" {
			_, _, rootPath, _, packageErr := e.loadSkillPackage(ctx, id, "manifest.json")
			if packageErr != nil {
				return p, "", "", packageErr
			}
			p.RootPath = rootPath
		}
		body = skillPromptFromManifest(manifest)
		folderKey = sk.Name
		folderKeys = []string{id, folderKey}
	} else {
		found := false
		for _, tpl := range skillapp.Catalog() {
			if tpl.ID == id || tpl.Name == id {
				p.SkillID = tpl.ID
				p.Label = tpl.DisplayName
				p.Version = tpl.Version
				p.Source = "catalog"
				folderKey = tpl.Name
				body, _ = tpl.Manifest["prompt"].(string)
				raw, _ := json.Marshal(tpl.Manifest)
				manifest = string(raw)
				packageSkill = skill.Skill{Name: tpl.Name, Version: tpl.Version, ManifestJSON: manifest}
				found = true
				break
			}
		}
		if !found {
			return p, "", "", errors.New("skill not found")
		}
		folderKeys = append([]string{folderKey}, skillViewFolderKeys(id, p.Label)...)
	}
	identity := fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%s\x00%s", p.SkillID, p.Version, p.Revision, p.Source, path, manifest)
	refs := skillReferencesFromManifest(manifest)
	if path != "" {
		files, packageErr := e.skillPackageFiles(packageSkill)
		if packageErr != nil {
			return p, "", "", packageErr
		}
		resource, packaged := files[path]
		if !packaged {
			resource, packaged = files["upstream/"+path]
		}
		if packaged {
			if len(resource) > skillDocumentMaxBytes || !utf8.Valid(resource) || strings.ContainsRune(string(resource), '\x00') {
				return p, "", "", errors.New("技能资源不是可分页读取的 UTF-8 文本或超过 1 MiB；请从文件目录查看")
			}
			p.Source += "/package"
			return p, string(resource), identity + "\x00package", nil
		}
		file, origin, found, err := readLocalSkillDocument(e.skillAttachmentRoots(), folderKeys, path)
		if err != nil {
			return p, "", "", err
		}
		if found {
			body = file
			p.Source += "/reference"
			identity += "\x00" + origin
		} else if listedSkillReference(refs, path) {
			body = "附件「" + path + "」列在 references，工作区没有这份文件。"
		} else {
			body = "该技能没有附件「" + path + "」，只有 SKILL.md 正文。"
		}
	} else if len(refs) > 0 {
		body += "\n\nreferences：\n- " + strings.Join(refs, "\n- ")
	} else {
		body += "\n\n无附件，只有 SKILL.md 正文。"
	}
	if !utf8.ValidString(body) {
		return p, "", "", errors.New("技能正文不是有效 UTF-8；未修改或省略源字节")
	}
	p.Label = truncateUTF8Bytes(p.Label, 120)
	return p, body, identity, nil
}
