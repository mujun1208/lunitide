package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/skillapp"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"strings"
)

func shippedCatalogPrompt(raw string) (id, label, prompt string, ok bool) {
	id = strings.TrimSpace(raw)
	norm := strings.TrimPrefix(strings.ToLower(id), "tpl-")
	for _, tpl := range skillapp.Catalog() {
		name := strings.TrimPrefix(strings.ToLower(tpl.Name), "tpl-")
		if tpl.ID != id && tpl.Name != id && name != norm && strings.TrimPrefix(tpl.ID, "tpl-") != norm {
			continue
		}
		body, _ := tpl.Manifest["prompt"].(string)
		body = strings.TrimSpace(body)
		if body == "" {
			return "", "", "", false
		}
		label = strings.TrimSpace(tpl.DisplayName)
		if label == "" {
			label = tpl.ID
		}
		return tpl.ID, label, body, true
	}
	return "", "", "", false
}

func (e *Engine) executeSkillMarketTool(ctx context.Context, name string, args json.RawMessage) (toolruntime.Result, error) {
	if err := e.CheckCapability(ctx, "skills"); err != nil {
		return toolruntime.Result{}, err
	}
	switch name {
	case "skill.catalog.list":
		return skillCatalogList(args), nil
	case "skill.list":
		return e.skillLibraryList(ctx, args)
	case "skill.install":
		return e.skillInstallTemplate(ctx, args)
	case "skill.publish":
		return e.skillPublishInstalled(ctx, args)
	default:
		return toolruntime.Result{}, errString("未知技能工具")
	}
}

func skillCatalogList(args json.RawMessage) toolruntime.Result {
	var p struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal(args, &p)
	q := strings.ToLower(strings.TrimSpace(p.Query))
	type row struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
	}
	out := make([]row, 0, 32)
	for _, tpl := range skillapp.Catalog() {
		blob := strings.ToLower(tpl.ID + " " + tpl.Name + " " + tpl.DisplayName + " " + tpl.Description)
		if q != "" && !strings.Contains(blob, q) {
			continue
		}
		out = append(out, row{ID: tpl.ID, Name: tpl.Name, DisplayName: tpl.DisplayName, Description: clipRunes(tpl.Description, 80)})
	}
	raw, _ := json.Marshal(map[string]any{"count": len(out), "templates": out})
	return toolruntime.Result{Output: string(raw)}
}

func (e *Engine) skillLibraryList(ctx context.Context, args json.RawMessage) (toolruntime.Result, error) {
	if !skillServiceAvailable(e.skills) {
		return toolruntime.Result{}, errString("skill service unavailable")
	}
	var p struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(args, &p)
	var status skill.SkillStatus
	switch strings.ToLower(strings.TrimSpace(p.Status)) {
	case "", "published":
		status = skill.SkillStatusPublished
	case "draft":
		status = skill.SkillStatusDraft
	case "all":
		status = ""
	default:
		return toolruntime.Result{}, errString("skill.list status 只能是 published、draft 或 all")
	}
	listed, err := e.skills.List(ctx, status)
	if err != nil {
		return toolruntime.Result{}, err
	}
	type row struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	out := make([]row, 0, len(listed))
	for _, sk := range listed {
		out = append(out, row{ID: sk.ID, Name: sk.Name, Status: string(sk.Status)})
	}
	raw, _ := json.Marshal(map[string]any{"count": len(out), "skills": out})
	return toolruntime.Result{Output: string(raw)}, nil
}

func catalogTemplate(raw string) (skillapp.CatalogTemplate, bool) {
	id := strings.TrimSpace(raw)
	norm := strings.TrimPrefix(strings.ToLower(id), "tpl-")
	for _, tpl := range skillapp.Catalog() {
		name := strings.TrimPrefix(strings.ToLower(tpl.Name), "tpl-")
		if tpl.ID == id || tpl.Name == id || name == norm {
			return tpl, true
		}
	}
	return skillapp.CatalogTemplate{}, false
}

func (e *Engine) skillInstallTemplate(ctx context.Context, args json.RawMessage) (toolruntime.Result, error) {
	if !skillServiceAvailable(e.skills) {
		return toolruntime.Result{}, errString("skill service unavailable")
	}
	var p struct {
		TemplateID string `json:"templateId"`
	}
	if json.Unmarshal(args, &p) != nil || strings.TrimSpace(p.TemplateID) == "" {
		return toolruntime.Result{}, errString("skill.install 需要 templateId")
	}
	tpl, ok := catalogTemplate(p.TemplateID)
	if !ok {
		return toolruntime.Result{}, errString("技能目录里没有这个模板")
	}
	sk, err := e.skills.InstallFromCatalog(ctx, tpl.ID)
	if errors.Is(err, skillapp.ErrTemplateInstalled) {
		if existing := e.librarySkillByTemplate(ctx, tpl); existing.ID != "" {
			notice := "技能库里已经有这一份。草稿用 skill.publish 发布。"
			if existing.Status == skill.SkillStatusPublished {
				notice = "技能库里已经有已发布的这一份，可以直接 skill.invoke。"
			}
			raw, _ := json.Marshal(map[string]string{"skillId": existing.ID, "name": existing.Name, "status": string(existing.Status), "notice": notice})
			return toolruntime.Result{Output: string(raw)}, nil
		}
		return toolruntime.Result{}, errString("这个模板已经在技能库里")
	}
	if err != nil {
		return toolruntime.Result{}, err
	}
	if sk.ID == "" {
		return toolruntime.Result{}, errString("安装没有返回技能编号")
	}
	raw, _ := json.Marshal(map[string]string{"skillId": sk.ID, "name": sk.Name, "status": string(sk.Status), "notice": "已安装为草稿。要让 skill.invoke 走技能库副本，再调用 skill.publish。"})
	return toolruntime.Result{Output: string(raw)}, nil
}

func (e *Engine) librarySkillByTemplate(ctx context.Context, tpl skillapp.CatalogTemplate) skill.Skill {
	listed, err := e.skills.List(ctx, "")
	if err != nil {
		return skill.Skill{}
	}
	want := skillapp.SkillNameKey(tpl.Name)
	for _, sk := range listed {
		if skillapp.SkillNameKey(sk.Name) == want {
			return sk
		}
	}
	return skill.Skill{}
}

func (e *Engine) skillPublishInstalled(ctx context.Context, args json.RawMessage) (toolruntime.Result, error) {
	if !skillServiceAvailable(e.skills) {
		return toolruntime.Result{}, errString("skill service unavailable")
	}
	var p struct {
		ID      string `json:"id"`
		SkillID string `json:"skillId"`
	}
	if json.Unmarshal(args, &p) != nil {
		return toolruntime.Result{}, errString("skill.publish 参数无效")
	}
	rawID := strings.TrimSpace(p.ID)
	if rawID == "" {
		rawID = strings.TrimSpace(p.SkillID)
	}
	if rawID == "" {
		return toolruntime.Result{}, errString("skill.publish 需要 id")
	}
	publishID := rawID
	if !validCanonicalULID(publishID) {
		tpl, ok := catalogTemplate(rawID)
		if !ok {
			return toolruntime.Result{}, errString("技能目录里没有这个模板")
		}
		existing := e.librarySkillByTemplate(ctx, tpl)
		if existing.ID == "" {
			installed, err := e.skillInstallTemplate(ctx, marketJSON(map[string]string{"templateId": tpl.ID}))
			if err != nil {
				return toolruntime.Result{}, err
			}
			var row struct {
				SkillID string `json:"skillId"`
			}
			_ = json.Unmarshal([]byte(installed.Output), &row)
			existing.ID = row.SkillID
		}
		if existing.ID == "" {
			return toolruntime.Result{}, errString("安装没有返回技能编号")
		}
		publishID = existing.ID
	}
	before := ""
	already := false
	if sk, err := e.skills.Get(ctx, publishID); err == nil && sk != nil {
		before = sk.Version
		already = sk.Status == skill.SkillStatusPublished
	}
	if !already {
		if err := e.skills.Publish(ctx, publishID); err != nil {
			if !errors.Is(err, skillapp.ErrInvalidTransition) {
				return toolruntime.Result{}, err
			}
			sk, gerr := e.skills.Get(ctx, publishID)
			if gerr != nil || sk == nil || sk.Status != skill.SkillStatusPublished {
				raw, _ := json.Marshal(map[string]string{"skillId": publishID, "status": "published", "notice": "当前状态不能再次发布。已发布的技能可以直接 skill.invoke。"})
				return toolruntime.Result{Output: string(raw)}, nil
			}
			before = sk.Version
			already = true
		}
	}
	return e.replyPublishedSkillVersion(ctx, publishID, before, already)
}

// replyPublishedSkillVersion reports the label the skill center lists.
// A newer manifest version replaces the 1.0.0 label written at create.
func (e *Engine) replyPublishedSkillVersion(ctx context.Context, id, before string, already bool) (toolruntime.Result, error) {
	aligned, err := e.alignInstalledSkillVersion(ctx, id, "")
	if err != nil {
		return toolruntime.Result{}, err
	}
	version := before
	if aligned != nil && aligned.Version != "" {
		version = aligned.Version
	}
	body := map[string]string{"skillId": id, "status": "published", "version": version}
	switch {
	case before != "" && version != before:
		body["notice"] = "注册版本已从 " + before + " 更新为 " + version + "，技能中心显示这一版。"
	case already:
		body["notice"] = "已经是发布状态，可以直接 skill.invoke。"
	}
	raw, _ := json.Marshal(body)
	return toolruntime.Result{Output: string(raw)}, nil
}

type skillVersionAligner interface {
	AlignRegisteredVersion(context.Context, string, string) (*skill.Skill, error)
}

func (e *Engine) alignInstalledSkillVersion(ctx context.Context, id, explicit string) (*skill.Skill, error) {
	if !skillServiceAvailable(e.skills) {
		return nil, errString("skill service unavailable")
	}
	aligner, ok := e.skills.(skillVersionAligner)
	if !ok {
		return e.skills.Get(ctx, id)
	}
	return aligner.AlignRegisteredVersion(ctx, id, explicit)
}

func marketJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}
