package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/jsonutil"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/skillapp"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
	"strings"
)

// skillToolDefinitions exposes published skills as one model-callable tool
// (voice companion / ordinary chat alike). The catalog injected into the
// system instruction carries each skill's skillId; the tool routes through
// the governed skillapp Invoke/Execute pipeline (risk assessment, audit,
// version pinning) rather than raw execution.
func (e *Engine) skillToolDefinitions() []gateway.ToolDefinition {
	if !skillServiceAvailable(e.skills) {
		return nil
	}
	return []gateway.ToolDefinition{
		{Name: "skill.invoke", Description: "Invoke one published skill by skillId (ULID or catalog template id such as slide-builder). input is the user's request text for the skill", Schema: []byte(`{"type":"object","properties":{"skillId":{"type":"string","description":"published skill ULID or catalog template id"},"input":{"type":"string","minLength":1,"maxLength":2048,"description":"the user request passed to the skill"}},"required":["skillId","input"],"additionalProperties":false}`)},
		{Name: "skill.view", Description: "Read one skill's working agreement (SKILL.md / prompt) by skillId or market template id. Optional path reads a reference file (L2). Use when the catalog summary is not enough.", Schema: []byte(`{"type":"object","properties":{"skillId":{"type":"string","minLength":1,"maxLength":128,"description":"installed skill ULID or catalog template id"},"path":{"type":"string","maxLength":256,"description":"optional reference file path"}},"required":["skillId"],"additionalProperties":false}`)},
		{Name: "skill.create", Description: "Create one local skill from a SKILL.md-style folder (name, displayName, permissions, entryPoint, manifestJson). Call once per skill. After it succeeds, write a short Chinese confirmation naming the skill and telling the user to install/publish it in Skill Center. Then continue any remaining user work.", Schema: []byte(`{"type":"object","properties":{"name":{"type":"string","minLength":1,"maxLength":128,"description":"stable skill id slug"},"displayName":{"type":"string","maxLength":200,"description":"human title; defaults to name"},"description":{"type":"string","maxLength":4096},"version":{"type":"string","maxLength":32,"description":"semver, default 1.0.0"},"permissions":{"type":"array","minItems":1,"items":{"type":"string","enum":["read_only","read_write","network","file_system","shell","admin"]}},"entryPoint":{"type":"string","maxLength":512,"description":"SKILL.md path or builtin:// entry"},"manifestJson":{"type":"string","minLength":2,"maxLength":65536,"description":"JSON manifest with prompt and triggers"}},"required":["name","permissions","manifestJson"],"additionalProperties":false}`)},
		{Name: "skill.manage", Description: "Create or patch a local skill draft. create stays draft until the user publishes in Skill Center (write approval). patch updates displayName/description/entryPoint/manifestJson of an existing skill.", Schema: []byte(`{"type":"object","properties":{"action":{"type":"string","enum":["create","patch"]},"skillId":{"type":"string","description":"required for patch"},"name":{"type":"string","maxLength":128},"displayName":{"type":"string","maxLength":200},"description":{"type":"string","maxLength":4096},"version":{"type":"string","maxLength":32},"permissions":{"type":"array","items":{"type":"string","enum":["read_only","read_write","network","file_system","shell","admin"]}},"entryPoint":{"type":"string","maxLength":512},"manifestJson":{"type":"string","maxLength":65536}},"required":["action"],"additionalProperties":false}`)},
	}
}

// invokeSkillTool runs one model-initiated skill invocation through the
// governed pipeline. Full-access conversations auto-approve (mirroring the
// mode's no-approval semantics for every other tool); other modes keep the
// skill's own risk gate — a requiresApproval skill answers a plain error
// telling the model to ask the user to run it via the / command instead of
// parking the stream in an approval flow the caller may not be able to
// answer (voice companion).
func (e *Engine) invokeSkillTool(ctx context.Context, mode executionMode, session string, args json.RawMessage) (toolruntime.Result, error) {
	var a struct {
		SkillID string `json:"skillId"`
		Input   string `json:"input"`
	}
	if json.Unmarshal(args, &a) != nil || strings.TrimSpace(a.SkillID) == "" || strings.TrimSpace(a.Input) == "" {
		return toolruntime.Result{}, errors.New("invalid skill.invoke arguments")
	}
	skillID, resolveErr := e.resolvePublishedSkillID(ctx, a.SkillID)
	if resolveErr != nil {
		return toolruntime.Result{}, resolveErr
	}
	a.SkillID = skillID
	if len(a.Input) > 2048 {
		return toolruntime.Result{}, errors.New("skill input too long (max 2048)")
	}
	inv, err := e.skills.Invoke(ctx, a.SkillID, session, a.Input, string(mode))
	if err != nil {
		return toolruntime.Result{}, err
	}
	approved := mode == executionModeFullAccess
	if inv.RequiresApproval && !approved {
		return toolruntime.Result{}, fmt.Errorf("skill %s requires user approval (risk %s); ask the user to run it via the / command", a.SkillID, inv.Risk)
	}
	out, err := e.skills.Execute(ctx, inv.ID, session, approved)
	if err != nil {
		return toolruntime.Result{}, err
	}
	return toolruntime.Result{Output: out.Output}, nil
}

func (e *Engine) invokeSkillCreateTool(ctx context.Context, args json.RawMessage) (toolruntime.Result, error) {
	if !skillServiceAvailable(e.skills) {
		return toolruntime.Result{}, errors.New("skill service unavailable")
	}
	var a struct {
		Name         string   `json:"name"`
		DisplayName  string   `json:"displayName"`
		Description  string   `json:"description"`
		Version      string   `json:"version"`
		Permissions  []string `json:"permissions"`
		EntryPoint   string   `json:"entryPoint"`
		ManifestJSON string   `json:"manifestJson"`
	}
	if json.Unmarshal(jsonutil.Repair(args), &a) != nil {
		return toolruntime.Result{}, fmt.Errorf("%s", jsonutil.RetryMessage("skill.create", "arguments are not valid JSON"))
	}
	name := strings.TrimSpace(a.Name)
	display := strings.TrimSpace(a.DisplayName)
	if display == "" {
		display = name
	}
	version := strings.TrimSpace(a.Version)
	if version == "" {
		version = "1.0.0"
	}
	entry := strings.TrimSpace(a.EntryPoint)
	if entry == "" {
		entry = "SKILL.md"
	}
	perms := make([]skill.PermissionLevel, 0, len(a.Permissions))
	for _, p := range a.Permissions {
		perms = append(perms, skill.PermissionLevel(p))
	}
	created, err := e.skills.Create(ctx, skill.Skill{
		Name: name, DisplayName: display, Description: a.Description,
		Version: version, Permissions: perms, EntryPoint: entry,
		ManifestJSON: a.ManifestJSON,
	})
	if err != nil {
		return toolruntime.Result{}, fmt.Errorf("%s", jsonutil.RetryMessage("skill.create", err.Error()))
	}
	label := created.DisplayName
	if label == "" {
		label = name
	}
	id := created.ID
	if id == "" {
		id = name
	}
	return toolruntime.Result{Output: "技能「" + label + "」已创建（id=" + id + "，status=" + string(created.Status) + "）。"}, nil
}

const skillViewMaxRunes = 8000

func (e *Engine) invokeSkillViewTool(ctx context.Context, args json.RawMessage) (toolruntime.Result, error) {
	if !skillServiceAvailable(e.skills) {
		return toolruntime.Result{}, errors.New("skill service unavailable")
	}
	var a struct {
		SkillID string `json:"skillId"`
		Path    string `json:"path"`
	}
	if json.Unmarshal(jsonutil.Repair(args), &a) != nil || strings.TrimSpace(a.SkillID) == "" {
		return toolruntime.Result{}, errors.New("invalid skill.view arguments")
	}
	id := strings.TrimSpace(a.SkillID)
	label, body, source, err := e.skillViewBody(ctx, id)
	if err != nil {
		return toolruntime.Result{}, err
	}
	runes := []rune(body)
	truncated := false
	if len(runes) > skillViewMaxRunes {
		body = string(runes[:skillViewMaxRunes]) + "\n…(truncated)"
		truncated = true
	}
	out := "技能「" + label + "」正文：\n" + body
	refs := skillReferencesFromManifest(source)
	path := strings.TrimSpace(a.Path)
	if path != "" {
		if file, ok := readLocalSkillAttachment(e.skillAttachmentRoots(), skillViewFolderKeys(id, label), path); ok {
			runes := []rune(file)
			if len(runes) > skillViewMaxRunes {
				file = string(runes[:skillViewMaxRunes]) + "\n…(truncated)"
			}
			out += "\n\n附件「" + path + "」：\n" + file
		} else if listedSkillReference(refs, path) {
			out += "\n\n附件「" + path + "」列在 references，工作区没有这份文件。"
		} else {
			out += "\n\n该技能没有附件「" + path + "」，只有 SKILL.md 正文。"
		}
	} else if len(refs) > 0 {
		out += "\n\nreferences：\n- " + strings.Join(refs, "\n- ")
	} else {
		out += "\n\n无附件，只有 SKILL.md 正文。"
	}
	if truncated {
		out += "\n需要执行时用 skill.invoke。"
	}
	return toolruntime.Result{Output: out}, nil
}

func (e *Engine) skillViewBody(ctx context.Context, id string) (label, body, source string, err error) {
	if validCanonicalULID(id) {
		sk, getErr := e.skills.Get(ctx, id)
		if getErr == nil && sk != nil {
			return skillViewLabel(*sk), skillPromptFromManifest(sk.ManifestJSON), sk.ManifestJSON, nil
		}
	}
	for _, tpl := range skillapp.Catalog() {
		if tpl.ID == id || tpl.Name == id {
			prompt, _ := tpl.Manifest["prompt"].(string)
			name := tpl.DisplayName
			if name == "" {
				name = tpl.Name
			}
			raw, _ := json.Marshal(tpl.Manifest)
			return name, strings.TrimSpace(prompt), string(raw), nil
		}
	}
	return "", "", "", errors.New("skill not found")
}

func skillViewLabel(sk skill.Skill) string {
	if strings.TrimSpace(sk.DisplayName) != "" {
		return sk.DisplayName
	}
	if strings.TrimSpace(sk.Name) != "" {
		return sk.Name
	}
	return sk.ID
}

func skillPromptFromManifest(raw string) string {
	var m struct {
		Prompt string `json:"prompt"`
	}
	if json.Unmarshal([]byte(raw), &m) == nil && strings.TrimSpace(m.Prompt) != "" {
		return strings.TrimSpace(m.Prompt)
	}
	return strings.TrimSpace(raw)
}

func skillReferencesFromManifest(raw string) []string {
	var m struct {
		References []string `json:"references"`
	}
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	var out []string
	for _, item := range m.References {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func listedSkillReference(refs []string, path string) bool {
	for _, ref := range refs {
		if ref == path || strings.HasSuffix(ref, path) {
			return true
		}
	}
	return false
}

func (e *Engine) skillAttachmentRoots() []string {
	var roots []string
	if e != nil && e.tools != nil {
		if root, ok := e.tools.FullAccessRootHint(); ok && strings.TrimSpace(root) != "" {
			roots = append(roots, root)
		}
	}
	if home := homeAgentSkillsRoot(); home != "" {
		roots = append(roots, home)
	}
	return roots
}

func skillViewFolderKeys(id, label string) []string {
	keys := []string{id, label}
	norm := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(id)), "tpl-")
	if norm != "" {
		keys = append(keys, norm)
	}
	for _, tpl := range skillapp.Catalog() {
		if tpl.ID == id || tpl.Name == id || strings.EqualFold(tpl.DisplayName, id) || strings.TrimPrefix(tpl.Name, "tpl-") == norm {
			keys = append(keys, tpl.ID, strings.TrimPrefix(tpl.Name, "tpl-"))
		}
	}
	return uniqueStrings(keys)
}

// invokeExpertCreateTool routes a model-initiated expert.create call through
// the M8 expert service. The expert is immediately available for mounting.
func (e *Engine) invokeExpertCreateTool(ctx context.Context, session string, args json.RawMessage) (toolruntime.Result, error) {
	var a struct {
		Name                string   `json:"name"`
		Division            string   `json:"division"`
		Description         string   `json:"description"`
		Semver              string   `json:"semver"`
		Identity            string   `json:"identity"`
		Mission             string   `json:"mission"`
		Rules               string   `json:"rules"`
		Workflow            string   `json:"workflow"`
		DeliverableTemplate string   `json:"deliverableTemplate"`
		SuccessMetrics      string   `json:"successMetrics"`
		SkillKeys           []string `json:"skillKeys"`
	}
	if json.Unmarshal(jsonutil.Repair(args), &a) != nil {
		return toolruntime.Result{}, fmt.Errorf("%s", jsonutil.RetryMessage("expert.create", "arguments are not valid JSON"))
	}
	if e.m8expert == nil {
		return toolruntime.Result{}, errors.New("expert service unavailable")
	}
	if strings.TrimSpace(a.Name) == "" || len(a.Name) > 128 {
		return toolruntime.Result{}, errors.New("expert name must be 1-128 characters")
	}
	res, err := e.m8expert.Create(ctx, m8app.CreateInput{
		Source: "local",
		Frontmatter: m8core.Frontmatter{
			Name: a.Name, Division: a.Division,
			Description: a.Description, Semver: a.Semver,
		},
		SixSection: m8core.SixSection{
			Identity: a.Identity, Mission: a.Mission,
			Rules: a.Rules, Workflow: a.Workflow,
			DeliverableTemplate: a.DeliverableTemplate,
			SuccessMetrics:      a.SuccessMetrics,
		},
		RequestID: ulid.Make().String(),
		SkillKeys: a.SkillKeys,
	})
	if err != nil {
		return toolruntime.Result{}, fmt.Errorf("%s", jsonutil.RetryMessage("expert.create", err.Error()))
	}
	b, _ := json.Marshal(res)
	return toolruntime.Result{Output: "专家「" + a.Name + "」已创建成功。\n" + string(b)}, nil
}

func (e *Engine) invokePluginCreateTool(ctx context.Context, session string, args json.RawMessage) (toolruntime.Result, error) {
	var a struct {
		PluginID    string         `json:"pluginId"`
		Name        string         `json:"name"`
		Kind        string         `json:"kind"`
		Description string         `json:"description"`
		Entrypoint  string         `json:"entrypoint"`
		Semver      string         `json:"semver"`
		Publisher   string         `json:"publisher"`
		Manifest    map[string]any `json:"manifest"`
	}
	if json.Unmarshal(jsonutil.Repair(args), &a) != nil {
		return toolruntime.Result{}, fmt.Errorf("%s", jsonutil.RetryMessage("plugin.create", "arguments are not valid JSON"))
	}
	pluginID := strings.TrimSpace(a.PluginID)
	if pluginID == "" || len(pluginID) > 128 {
		return toolruntime.Result{}, errors.New("pluginId must be 1-128 characters")
	}
	switch strings.ToLower(strings.TrimSpace(a.Kind)) {
	case "mcp":
		return toolruntime.Result{}, errors.New("plugin.create 不能安装 MCP。请用 mcp.presets 查看现役预置，再 mcp.install")
	case "agent-pack":
		return toolruntime.Result{}, errors.New("plugin.create 不能加载可执行 Agent 包。请用 skill.create 或 expert.create")
	}
	if !m8core.ValidPluginKind(a.Kind) {
		return toolruntime.Result{}, errors.New("invalid plugin kind")
	}
	if e.m8plugin == nil {
		return toolruntime.Result{}, errors.New("plugin service unavailable")
	}
	manifest := a.Manifest
	if manifest == nil {
		manifest = map[string]any{}
	}
	if _, ok := manifest["pluginId"]; !ok {
		manifest["pluginId"] = pluginID
	}
	if _, ok := manifest["id"]; !ok {
		manifest["id"] = pluginID
	}
	if _, ok := manifest["kind"]; !ok {
		manifest["kind"] = a.Kind
	}
	if _, ok := manifest["semver"]; !ok {
		semver := strings.TrimSpace(a.Semver)
		if semver == "" {
			semver = "1.0.0"
		}
		manifest["semver"] = semver
	}
	if _, ok := manifest["publisher"]; !ok {
		publisher := strings.TrimSpace(a.Publisher)
		if publisher == "" {
			publisher = "local"
		}
		manifest["publisher"] = publisher
	}
	if a.Description != "" {
		if _, ok := manifest["description"]; !ok {
			manifest["description"] = a.Description
		}
	}
	entry := packEntrypointOrDefault(a.Entrypoint)
	workspace := session
	if workspace == "" {
		workspace = "chat"
	}
	res, err := e.m8plugin.CreateAndMount(ctx, m8app.DevCreateInput{
		WorkspaceID: workspace, Manifest: manifest, Entrypoint: entry,
	})
	if err != nil {
		return toolruntime.Result{}, fmt.Errorf("%s", jsonutil.RetryMessage("plugin.create", err.Error()))
	}
	label := strings.TrimSpace(a.Name)
	if label == "" {
		label = pluginID
	}
	notes, failed := e.applyCapabilityPack(ctx, packSpecFromManifest(manifest))
	if failed != "" && res.State != "quarantined" {
		res.State = "quarantined"
	}
	return toolruntime.Result{Output: formatPackInstallResult(label, pluginID, res.State, notes, failed)}, nil
}
