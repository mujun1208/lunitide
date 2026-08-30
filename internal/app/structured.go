package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/jsonutil"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

const (
	toolStructuredOutput = "structured.output"
)

func structuredOutputDefinition() gateway.ToolDefinition {
	return gateway.ToolDefinition{
		Name: toolStructuredOutput,
		Description: "Emit validated JSON for the user. Use when they want structured data (calendar event, form fields, key-value summary) instead of prose. " +
			"template=event → data {title, start, end?, location?, attendees?}; " +
			"template=form → data {fields:[{label,value}]}; " +
			"template=kv → data {pairs:[{key,value}]}; " +
			"template=custom → pass schemaJson (a JSON Schema object string) and data matching it. " +
			"Example: {\"template\":\"event\",\"data\":{\"title\":\"站会\",\"start\":\"2026-08-27T09:30:00+08:00\"}}",
		Schema: []byte(`{"type":"object","properties":{"template":{"type":"string","enum":["event","form","kv","custom"],"description":"event=calendar; form=labeled fields; kv=pairs; custom=schemaJson"},"data":{"type":"object","description":"payload matching the template"},"schemaJson":{"type":"string","maxLength":8000,"description":"JSON Schema when template=custom"}},"required":["template","data"],"additionalProperties":false}`),
	}
}

func replyStyleInstruction(style string, companion bool) string {
	if companion {
		return ""
	}
	switch strings.TrimSpace(style) {
	case "assistant":
		return "\n\n[说话风格] 保持月汐身份。语气专业克制、条理清楚，先结论后依据。不要客套堆砌。\n"
	case "support":
		return "\n\n[说话风格] 保持月汐身份。用客服口吻：先确认诉求，再给步骤或结果，必要时复述将要执行的操作。禁止承诺做不到的事。\n"
	case "teacher":
		return "\n\n[说话风格] 保持月汐身份。用老师口吻：短讲解 + 一个例子 + 可跟做的下一步。不要长篇讲义。\n"
	case "npc":
		return "\n\n[说话风格] 保持月汐身份。可用轻角色感（沉浸、短句），但仍是本机助理；禁止编造工具已执行。\n"
	default:
		return ""
	}
}

func structuredTemplateInstruction(template string) string {
	switch strings.TrimSpace(template) {
	case "event", "form", "kv":
		return "\n\n[结构化输出] 本轮对用户的最终交付必须调用 structured.output，template=" + template + "。先收集字段，再发工具；不要只输出散文或未校验的代码块。缺字段时问一句再调用。\n"
	default:
		return ""
	}
}

// inferStructuredTemplate picks event/form/kv from the user turn.
// A settings lock (event|form|kv) wins; "off"/empty falls through to intent
// so structured.output is a contract, not a hidden switch.
func inferStructuredTemplate(turnText, setting string) string {
	switch strings.TrimSpace(setting) {
	case "event", "form", "kv":
		return strings.TrimSpace(setting)
	}
	t := strings.ToLower(strings.TrimSpace(turnText))
	if t == "" {
		return ""
	}
	if strings.Contains(t, "日程") || strings.Contains(t, "calendar") ||
		(strings.Contains(t, "json") && (strings.Contains(t, "会议") || strings.Contains(t, "事件") || strings.Contains(t, "抽成日程"))) {
		return "event"
	}
	if strings.Contains(t, "表单") || strings.Contains(t, "form field") {
		return "form"
	}
	if strings.Contains(t, "键值") || (strings.Contains(t, "json") && (strings.Contains(t, "摘要") || strings.Contains(t, "总结") || strings.Contains(t, "抽成"))) {
		return "kv"
	}
	return ""
}

func identityAndFewShotInstruction() string {
	return "\n\n[身份与边界] 你是月汐，用户这台电脑上的私人助理。能做：对话、工作区文件、网页搜索/抓取、browser.act 浏览器自动化、本机 computer.act 电脑控制（仅当该工具出现在本轮列表）、定时任务。禁止：操作远程电脑或局域网设备、自动确认 UAC/提权、把失败的工具说成成功。打开/保存文件对话框交给用户去点。\n" +
		"示例：用户「帮我搜北京明天天气」→ 调用 web.search，query=\"北京明天天气\"，max 默认 5，再用一两句汇报来源。不要空口编气温。\n" +
		"示例：用户「把下面文字抽成日程 JSON」→ 调用 structured.output template=event，把 title/start 填进 data。\n"
}

func identityAnchorReminder() string {
	return "\n[身份提醒] 上面的工作流不替换身份：你仍是月汐。工具失败不要说成成功。\n"
}

func emitStructuredOutput(args json.RawMessage) (toolruntime.Result, error) {
	repaired := jsonutil.Repair(args)
	if err := jsonutil.Validate(structuredOutputDefinition().Schema, repaired); err != nil {
		return toolruntime.Result{}, fmt.Errorf("%s", jsonutil.RetryMessage(toolStructuredOutput, err.Error()))
	}
	var a struct {
		Template   string          `json:"template"`
		Data       json.RawMessage `json:"data"`
		SchemaJSON string          `json:"schemaJson"`
	}
	if err := json.Unmarshal(repaired, &a); err != nil {
		return toolruntime.Result{}, fmt.Errorf("%s", jsonutil.RetryMessage(toolStructuredOutput, "arguments are not valid JSON"))
	}
	schema := templateSchema(a.Template, a.SchemaJSON)
	if schema == nil {
		return toolruntime.Result{}, fmt.Errorf("%s", jsonutil.RetryMessage(toolStructuredOutput, "unknown template"))
	}
	if err := jsonutil.Validate(schema, a.Data); err != nil {
		return toolruntime.Result{}, fmt.Errorf("%s", jsonutil.RetryMessage(toolStructuredOutput, err.Error()))
	}
	var payload any
	if err := json.Unmarshal(jsonutil.Repair(a.Data), &payload); err != nil {
		return toolruntime.Result{}, fmt.Errorf("%s", jsonutil.RetryMessage(toolStructuredOutput, "data is not valid JSON"))
	}
	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return toolruntime.Result{}, err
	}
	return resultFromOutput(string(out)), nil
}

func templateSchema(template, custom string) json.RawMessage {
	switch template {
	case "event":
		return json.RawMessage(`{"type":"object","properties":{"title":{"type":"string","minLength":1},"start":{"type":"string","minLength":1},"end":{"type":"string"},"location":{"type":"string"},"attendees":{"type":"array","items":{"type":"string"}}},"required":["title","start"],"additionalProperties":false}`)
	case "form":
		return json.RawMessage(`{"type":"object","properties":{"fields":{"type":"array","minItems":1,"items":{"type":"object","properties":{"label":{"type":"string","minLength":1},"value":{"type":"string"}},"required":["label","value"],"additionalProperties":false}}},"required":["fields"],"additionalProperties":false}`)
	case "kv":
		return json.RawMessage(`{"type":"object","properties":{"pairs":{"type":"array","minItems":1,"items":{"type":"object","properties":{"key":{"type":"string","minLength":1},"value":{"type":"string"}},"required":["key","value"],"additionalProperties":false}}},"required":["pairs"],"additionalProperties":false}`)
	case "custom":
		custom = strings.TrimSpace(custom)
		if custom == "" {
			return nil
		}
		repaired := jsonutil.Repair([]byte(custom))
		if !json.Valid(repaired) {
			return nil
		}
		return json.RawMessage(repaired)
	default:
		return nil
	}
}

func resultFromOutput(s string) toolruntime.Result {
	return toolruntime.Result{Output: s, Digest: toolruntime.Digest(toolStructuredOutput, json.RawMessage(s))}
}

func toolSchemaByName(defs []gateway.ToolDefinition, name string) json.RawMessage {
	for _, d := range defs {
		if d.Name == name {
			return d.Schema
		}
	}
	return nil
}

func prepareToolArguments(name string, args json.RawMessage, schema json.RawMessage) (json.RawMessage, string) {
	repaired := jsonutil.Repair(args)
	if len(strings.TrimSpace(string(repaired))) == 0 {
		repaired = []byte(`{}`)
	}
	if toolruntime.Digest(name, repaired) == "" {
		return repaired, jsonutil.RetryMessage(name, "arguments are not valid JSON; resend a JSON object matching the tool schema")
	}
	if len(schema) > 0 {
		if err := jsonutil.Validate(schema, repaired); err != nil {
			return repaired, jsonutil.RetryMessage(name, err.Error())
		}
	}
	return repaired, ""
}

func argsDigestOrFallback(name string, args json.RawMessage) string {
	if d := toolruntime.Digest(name, args); d != "" {
		return d
	}
	sum := sha256.Sum256(append(append([]byte(name), 0), args...))
	return hex.EncodeToString(sum[:])
}
