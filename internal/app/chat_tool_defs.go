package app

import (
	"context"
	"encoding/json"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

// fullDiskChat answers whether this conversation runs with the persisted
// full-disk opt-in: the user chose full-access mode AND command-policy.json
// carries "fullAccess": true. Only then do absolute paths and unlisted
// commands become available - and only through the user tool-call path.
func (e *Engine) fullDiskChat(mode executionMode) bool {
	return mode == executionModeFullAccess && e.tools != nil && e.tools.FullDiskEnabled()
}

// engineToolDefinitionsFor adapts the static tool descriptions to the
// full-disk opt-in so the model knows absolute paths and arbitrary commands
// are accepted in this conversation. Subagent read-only definitions keep
// the sandbox wording (they stay confined at the runtime level).
func (e *Engine) engineToolDefinitionsFor(mode executionMode) []gateway.ToolDefinition {
	defs := engineToolDefinitions()
	if !e.fullDiskChat(mode) {
		return defs
	}
	for i := range defs {
		switch defs[i].Name {
		case "command.run":
			defs[i].Description = "Run any command on this machine (full-disk full-access is enabled). Prefer workspace.write for files and desktop.open for opening one named Desktop file. Windows PowerShell -Command is rewritten to UTF-8; mkdir/New-Item Directory uses Unicode APIs. Failed commands return ok:false — do not tell the user it succeeded. argv max 16 items"
		case "workspace.list", "workspace.read":
			defs[i].Description += "; absolute paths on any drive are accepted (full-disk full-access is enabled)"
		case "workspace.write", "workspace.edit", "workspace.search":
			defs[i].Description += "; absolute paths on any drive are accepted and missing parent directories are created (full-disk full-access is enabled)"
		case "html.gen", "excel.gen", "docx.gen", "pptx.gen", "pdf.gen":
			defs[i].Description += "; desktop=true writes a double-clickable file on the real Desktop (full-disk full-access is enabled)"
		case "desktop.open":
			defs[i].Description += "; full-disk full-access is enabled — opens one real Desktop file with the default app"
		case "media.play":
			defs[i].Description += "; full-disk full-access is enabled — foreground target types into the active music app; browser targets open music URLs and send media keys"
		}
	}
	return defs
}

// executeUserTool routes one user-conversation tool call. Full-access
// conversations with the full-disk opt-in reach the unconfined runtime
// entry point; every other mode stays on the confined one. Subagent and
// delegation paths call toolruntime Execute directly and never get here.
func (e *Engine) executeUserTool(ctx context.Context, mode executionMode, session, name string, args json.RawMessage) (toolruntime.Result, error) {
	return e.executeUserToolStreaming(ctx, mode, session, name, args, nil)
}

// executeUserToolStreaming is executeUserTool with an optional live
// progress sink (P1-2): command.run pushes bounded output chunks to the
// stream between tool_started and tool_completed so long-running commands
// stop black-boxing.
func (e *Engine) executeUserToolStreaming(ctx context.Context, mode executionMode, session, name string, args json.RawMessage, progress func(chunk string)) (toolruntime.Result, error) {
	if name == "docx.gen" {
		args = enrichDocxGenArgs(e, "", args)
	}
	approved := mode == executionModeFullAccess && name != "user.ask"
	if e.fullDiskChat(mode) {
		return e.tools.ExecuteUnconfinedStreaming(ctx, session, name, args, approved, progress)
	}
	return e.tools.ExecuteStreaming(ctx, toolruntime.Mode(mode), session, name, args, approved, progress)
}

func engineToolDefinitions() []gateway.ToolDefinition {
	return []gateway.ToolDefinition{
		{Name: "workspace.list", Description: "List a controlled session workspace directory", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"}},"additionalProperties":false}`)},
		{Name: "workspace.read", Description: "Read a controlled session workspace file", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)},
		{Name: "workspace.write", Description: "Atomically write a controlled session workspace file", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`)},
		{Name: "workspace.search", Description: "Search session workspace files for a literal substring or regex; answers path:line: text matches (binary and oversized files skipped)", Schema: []byte(`{"type":"object","properties":{"query":{"type":"string","description":"literal substring, or regex when regex=true"},"path":{"type":"string","description":"workspace-relative directory to search (default .)"},"regex":{"type":"boolean"},"max":{"type":"integer","minimum":1,"maximum":200}},"required":["query"],"additionalProperties":false}`)},
		{Name: "workspace.edit", Description: "Anchored edit of controlled session workspace file(s). oldText must match exactly once (or pass replaceAll=true) and is replaced by newText. Several replacements in one file: edits[{oldText,newText,replaceAll?}]. Several files in one call: files[{path,oldText,newText,replaceAll?,edits?}]. If any hunk's oldText is missing the whole call fails and no file is written.", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"},"oldText":{"type":"string"},"newText":{"type":"string"},"replaceAll":{"type":"boolean"},"edits":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"object","additionalProperties":false,"properties":{"oldText":{"type":"string"},"newText":{"type":"string"},"replaceAll":{"type":"boolean"}},"required":["oldText","newText"]}},"files":{"type":"array","minItems":1,"maxItems":8,"items":{"type":"object","additionalProperties":false,"properties":{"path":{"type":"string"},"oldText":{"type":"string"},"newText":{"type":"string"},"replaceAll":{"type":"boolean"},"edits":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"object","additionalProperties":false,"properties":{"oldText":{"type":"string"},"newText":{"type":"string"},"replaceAll":{"type":"boolean"}},"required":["oldText","newText"]}}},"required":["path"]}}},"additionalProperties":false}`)},
		{Name: "todo.write", Description: "Persist the full task checklist for this session (write the complete list every time; at most one item in_progress)", Schema: []byte(`{"type":"object","properties":{"todos":{"type":"array","maxItems":50,"items":{"type":"object","additionalProperties":false,"properties":{"content":{"type":"string","minLength":1,"maxLength":500},"status":{"type":"string","enum":["pending","in_progress","completed"]},"priority":{"type":"string","enum":["high","medium","low"]}},"required":["content"]}}},"required":["todos"],"additionalProperties":false}`)},
		{Name: "user.ask", Description: "Ask the user to decide with numbered options (Claude/Cursor-style). One pack of 1–8 questions, each with 2–5 options. The UI shows one question at a time plus 其他. 拍板必须用选项，不要用长文代替决策。Always wait — never assume an answer.", Schema: []byte(`{"type":"object","properties":{"title":{"type":"string","maxLength":200,"description":"Short heading for the decision pack"},"questions":{"type":"array","minItems":1,"maxItems":8,"items":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string","maxLength":64},"prompt":{"type":"string","minLength":1,"maxLength":500},"options":{"type":"array","minItems":2,"maxItems":5,"items":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string","maxLength":64},"label":{"type":"string","minLength":1,"maxLength":200}},"required":["label"]}}},"required":["prompt","options"]}}},"required":["questions"],"additionalProperties":false}`)},
		{Name: "command.run", Description: "Run one allowlisted command in the controlled workspace (built-in read-only git/go set plus the user command-policy.json whitelist). Windows PowerShell -Command is rewritten to a UTF-8 script so CJK paths round-trip; mkdir/New-Item Directory uses Unicode APIs. Failed commands return ok:false — do not tell the user it succeeded.", Schema: []byte(`{"type":"object","properties":{"argv":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":16}},"required":["argv"],"additionalProperties":false}`)},
		{Name: "run_terminal_cmd", Description: "Terminal: run one command line in the controlled workspace and stream its output — use this for build/test/lint/git loops (e.g. \"go test ./...\", \"npm run build\", \"git --no-pager diff\"). Pass the whole command as a string; it is executed directly (no shell), so pipes/redirect/&&/$() are not supported — run one program per call. Same allowlist + approval as command.run: reversible git writes (add/commit/stash) and the read-only git/go set run by default; broader commands need the full-disk opt-in or command-policy.json. Failed commands return ok:false — never claim success.", Schema: []byte(`{"type":"object","properties":{"command":{"type":"string","minLength":1,"maxLength":2000,"description":"the command line, e.g. go test ./..."}},"required":["command"],"additionalProperties":false}`)},
		{Name: "web.fetch", Description: "Fetch one public http(s) URL through the SSRF-pinned transport and return extracted text (title, final URL, body). The workspace browser address bar shows this URL.", Schema: []byte(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"],"additionalProperties":false}`)},
		{Name: "web.search", Description: "Search the public web and return ranked results with titles, URLs and snippets. Use for current facts, docs, or links — do not invent temperatures or prices. The in-app browser tab shows a SERP and its address bar is set to the real results URL (never a blank https:// or a homepage). Do not fetch bing.com without a query. Example: {\"query\":\"北京明天天气\",\"max\":5}", Schema: []byte(`{"type":"object","properties":{"query":{"type":"string","description":"Search query. Example: 北京明天天气"},"max":{"type":"integer","description":"Number of results to return, default 5 (1-10).","minimum":1,"maximum":10}},"required":["query"],"additionalProperties":false}`)},
		{Name: "memory.search", Description: "Search confirmed long-term memories and compacted summaries. Never returns raw chat transcripts or unconfirmed candidates.", Schema: []byte(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":2048},"max":{"type":"integer","minimum":1,"maximum":12}},"required":["query"],"additionalProperties":false}`)},
		{Name: "memory.get", Description: "Read one confirmed memory by id from memory.search. Does not return raw chat logs.", Schema: []byte(`{"type":"object","properties":{"id":{"type":"string","minLength":1,"maxLength":64}},"required":["id"],"additionalProperties":false}`)},
		{Name: "excel.gen", Description: "Generate an .xlsx workbook (headers, rows and an optional bar/col/line/pie chart over the first two columns) into the session workspace. Set desktop=true to write onto the real Desktop (filename in path is enough). Never build XLSX via Excel COM, Python, or command.run — that truncates the tool call and fails the turn. Keep sheets compact (monthly totals, not hundreds of daily rows).", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string","description":"output path ending in .xlsx; with desktop=true a relative name lands on the real Desktop"},"desktop":{"type":"boolean"},"sheets":{"type":"array","minItems":1,"maxItems":16,"items":{"type":"object","additionalProperties":false,"properties":{"name":{"type":"string"},"headers":{"type":"array","items":{"type":"string"}},"rows":{"type":"array","items":{"type":"array","items":{}}},"chart":{"type":"object","additionalProperties":false,"properties":{"type":{"type":"string","enum":["bar","col","line","pie"]},"title":{"type":"string"}}}},"required":["rows"]}}},"required":["path","sheets"],"additionalProperties":false}`)},
		{Name: "excel.parse", Description: "Parse an .xlsx workbook from the session workspace and return sheet names, dimensions and a bounded cell preview as JSON", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)},
		{Name: "docx.gen", Description: "Generate a print-ready .docx (Chinese 宋体/黑体, Heading 1/2, body, optional quote/caption, 1.5 line spacing). Empty or unstyled single-style bodies are rejected. Reports: kind=report (cover + sections). Novels: kind=novel (title+author, chapter Heading 1, substantial prose — not an outline dump). Call only after the report/novel pipeline. Set desktop=true to write onto the real Desktop. Never build DOCX via Word COM or command.run.", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string","description":"output path ending in .docx; with desktop=true a relative name lands on the real Desktop"},"desktop":{"type":"boolean"},"title":{"type":"string"},"subtitle":{"type":"string"},"author":{"type":"string"},"kind":{"type":"string","enum":["report","novel","document"]},"blocks":{"type":"array","minItems":1,"maxItems":500,"items":{"type":"object","additionalProperties":false,"properties":{"type":{"type":"string","enum":["heading","heading2","paragraph","bullet","quote","caption"]},"text":{"type":"string"},"level":{"type":"integer","minimum":1,"maximum":2}},"required":["text"]}}},"required":["path","title","blocks"],"additionalProperties":false}`)},
		{Name: "pptx.gen", Description: "Generate a widescreen business .pptx (navy/teal cover, section dividers, content slides with headers and bullets, Microsoft YaHei). Every slide needs a visible title; dark backgrounds must use light text. Empty or fill-only slides are rejected. Put speaker notes in slides[].notes. Call this only after the PPT pipeline (outline, copy, two web research passes). Write it into the session workspace. Set desktop=true to write onto the real Desktop. Never build PPTX via PowerPoint COM, ZipFile XML, or command.run.", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string","description":"output path ending in .pptx; with desktop=true a relative name lands on the real Desktop"},"desktop":{"type":"boolean"},"title":{"type":"string"},"slides":{"type":"array","minItems":1,"maxItems":30,"items":{"type":"object","additionalProperties":false,"properties":{"title":{"type":"string","minLength":1},"subtitle":{"type":"string"},"layout":{"type":"string","enum":["title","section","content"]},"bullets":{"type":"array","maxItems":12,"items":{"type":"string"}},"notes":{"type":"string","description":"speaker notes for this slide"}},"required":["title"]}}},"required":["path","title","slides"],"additionalProperties":false}`)},
		{Name: "html.gen", Description: "Generate a built-in single-file HTML app (World Cup penalty shootout, countdown timer, or a local checklist). Use this for desktop mini-games, timers, and to-do pages. Never dump a full HTML page into workspace.write or command.run — that truncates the tool call and fails the turn. Set desktop=true to write onto the real Desktop.", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string","description":"output .html path; with desktop=true a relative name lands on the real Desktop"},"title":{"type":"string"},"template":{"type":"string","enum":["penalty-shootout","timer","checklist"]},"desktop":{"type":"boolean"}},"required":["template"],"additionalProperties":false}`)},
		{Name: "desktop.open", Description: "Open exactly one Desktop file, folder, shortcut, or installed app whose name best matches the query (e.g. 协议 → 协议.docx, 汽水音乐 / 网易云音乐 → desktop shortcut, Start Menu, or known install path like cloudmusic.exe). Never open unrelated items. If several tie, return the list and open nothing.", Schema: []byte(`{"type":"object","properties":{"name":{"type":"string","minLength":1,"maxLength":200,"description":"filename or app name fragment the user said"}},"required":["name"],"additionalProperties":false}`)},
		{Name: "desktop.type", Description: "Type into a named UIA edit or a visible labeled field. Use after= for the on-screen name such as 身份证号码. Do not use Ctrl+F: if no named edit or labeled field is visible this returns 无法执行 — then use computer.act (screenshot, frameId, click, type, verifyAfter). submit=true presses Enter and clicks 发送/确定. Pass window= to focus the dialog first so keys do not hit 月伴.", Schema: []byte(`{"type":"object","properties":{"text":{"type":"string","minLength":1,"maxLength":4096,"description":"literal text to type"},"after":{"type":"string","maxLength":200,"description":"visible field name (e.g. 身份证号码, 证件号码)"},"window":{"type":"string","maxLength":200,"description":"window title fragment to focus first"},"submit":{"type":"boolean","description":"press Enter / click 发送 after typing"}},"required":["text"],"additionalProperties":false}`)},
		{Name: "media.play", Description: "Play, pause, or skip music/video on this machine. target=foreground launches/focuses the named desktop player if needed (网易云音乐=cloudmusic.exe), searches in that app, and plays; artist queries like 周杰伦 click a search result in the focused player. Never click 我喜欢的音乐 / 收藏. Prefer this over website search. target=browser opens a search URL only when the user asked for the web player. Full-access mode is enough; the full-disk switch is not required.", Schema: []byte(`{"type":"object","properties":{"action":{"type":"string","enum":["play","open_and_play","open","pause","toggle","next","prev","stop"],"description":"default play"},"query":{"type":"string","description":"song or artist to search"},"url":{"type":"string","description":"direct http(s) music page"},"target":{"type":"string","enum":["auto","foreground","browser","netease","qqmusic"],"description":"foreground=desktop player on this PC; auto prefers session context"},"app":{"type":"string","description":"app name to focus when target=foreground"}},"additionalProperties":false}`)},
		{Name: "im.send", Description: "Send a message on a configured IM channel (设置 → 消息通道). channel=feishu|wecom|dingtalk uses the pasted https webhook; channel=wechat|qq opens the logged-in desktop client. Pass to= for a contact name when using the desktop client. If the channel is off, tell the user to enable it in Settings.", Schema: []byte(`{"type":"object","properties":{"channel":{"type":"string","enum":["feishu","wecom","dingtalk","wechat","qq"]},"to":{"type":"string","maxLength":80,"description":"optional contact or group name"},"text":{"type":"string","minLength":1,"maxLength":4000}},"required":["channel","text"],"additionalProperties":false}`)},
		{Name: "pdf.gen", Description: "Generate a .pdf report (title plus body paragraphs) into the session workspace. Latin text renders best; Chinese reports should use docx.gen. Set desktop=true to write onto the real Desktop.", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string","description":"output path ending in .pdf; with desktop=true a relative name lands on the real Desktop"},"desktop":{"type":"boolean"},"title":{"type":"string"},"body":{"type":"string"}},"required":["path","title","body"],"additionalProperties":false}`)},
		{Name: "browser.act", Description: "Browser automation on this PC in one managed browser. Typical flow: navigate → use returned snapshot refs to click/type (do not guess CSS). Most mutating ops return a fresh snapshot; if a ref is stale, snapshot once and retry that one action. Login walls, 2FA, captcha, and file pickers are manual — stop and ask. Do not use evaluate, file upload, or install. navigate prefers Playwright MCP (auto-installed); click/type/snapshot error with BROWSER_MCP_NOT_READY if Playwright is missing — that is not a successful click. read extracts public-page text via fetch. Do not fall back to media.play or desktop pixels. Example: {\"op\":\"navigate\",\"url\":\"https://example.com/login\"}.", Schema: []byte(`{"type":"object","properties":{"op":{"type":"string","enum":["navigate","snapshot","click","type","read","scroll","back","hover","select","press","tabs","wait","dialog"],"description":"navigate opens url; snapshot first if you have no refs; click/type/hover/select/press use those refs; scroll/back/tabs/wait/dialog are Playwright extras; read extracts text"},"url":{"type":"string","description":"Absolute URL for navigate. Example: https://example.com/login. read reuses the last navigated URL when omitted"},"selector":{"type":"string","description":"CSS selector or snapshot ref for click/type/hover/select. Prefer refs from the last snapshot."},"text":{"type":"string","description":"Text to type, or option value for select"},"key":{"type":"string","description":"Key name for press (e.g. Enter, Escape)"},"direction":{"type":"string","enum":["up","down"],"description":"scroll direction"},"ms":{"type":"integer","minimum":0,"maximum":30000,"description":"wait milliseconds"},"accept":{"type":"boolean","description":"dialog accept=true or dismiss=false"},"tab":{"type":"string","enum":["list","new","close","select"],"description":"tabs action"},"index":{"type":"integer","minimum":0,"description":"tab index for tabs select/close"}},"required":["op"],"additionalProperties":false}`)},
		{Name: "image.generate", Description: "Generate an image with the configured 生图模型 catalog (default, then backups). Use when the user asks to draw, illustrate, or generate a picture. Prompt is the image description.", Schema: []byte(`{"type":"object","properties":{"prompt":{"type":"string","minLength":1,"maxLength":4000,"description":"Image description"},"path":{"type":"string","description":"Optional workspace-relative hint for where to save"}},"required":["prompt"],"additionalProperties":false}`)},
		{Name: "video.generate", Description: "Generate a video with the configured 生视频模型 catalog (default, then backups). Use when the user asks to make or generate a video.", Schema: []byte(`{"type":"object","properties":{"prompt":{"type":"string","minLength":1,"maxLength":4000,"description":"Video description"},"path":{"type":"string","description":"Optional workspace-relative hint for where to save"}},"required":["prompt"],"additionalProperties":false}`)},
		structuredOutputDefinition(),
	}
}

// expertToolDefinitions exposes the expert.create tool when the expert
// service is wired. The model can create a six-section expert profile
// directly from the conversation.
func (e *Engine) expertToolDefinitions() []gateway.ToolDefinition {
	if e.m8expert == nil {
		return nil
	}
	return []gateway.ToolDefinition{
		{Name: "expert.create", Description: "Create a six-section expert profile (name, division, description, and six-section body: identity, mission, rules, workflow, deliverableTemplate, successMetrics). Optionally bind published skill catalog keys with skillKeys — skills hang on the expert, not the chat composer. After success, tell the user to confirm skills in Expert Center.", Schema: []byte(`{"type":"object","properties":{"name":{"type":"string","minLength":1,"maxLength":128,"description":"Expert display name"},"division":{"type":"string","enum":["engineering","design","product","project-management","testing","security","operations","data"],"description":"Expert domain"},"description":{"type":"string","minLength":1,"maxLength":2000,"description":"Short description of the expert"},"semver":{"type":"string","description":"Semantic version like 1.0.0"},"identity":{"type":"string","minLength":1,"maxLength":65536,"description":"Expert identity prompt section"},"mission":{"type":"string","minLength":1,"maxLength":65536,"description":"Expert mission prompt section"},"rules":{"type":"string","minLength":1,"maxLength":65536,"description":"Expert rules prompt section"},"workflow":{"type":"string","minLength":1,"maxLength":65536,"description":"Expert workflow prompt section"},"deliverableTemplate":{"type":"string","minLength":1,"maxLength":65536,"description":"Expert deliverable template prompt section"},"successMetrics":{"type":"string","minLength":1,"maxLength":65536,"description":"Expert success metrics prompt section"},"skillKeys":{"type":"array","maxItems":32,"items":{"type":"string","minLength":1,"maxLength":64},"description":"Optional published skill catalog keys bound to this expert"}},"required":["name","division","description","semver","identity","mission","rules","workflow","deliverableTemplate","successMetrics"],"additionalProperties":false}`)},
	}
}

func (e *Engine) pluginToolDefinitions() []gateway.ToolDefinition {
	if e.m8plugin == nil {
		return nil
	}
	return []gateway.ToolDefinition{
		{Name: "plugin.create", Description: "Create one capability pack: a manifest of skills[] + mcpPresetIds[] + toolGates[]. This installs those catalog items; it does not execute Cordis/TypeScript. kind=mcp or agent-pack is refused. After success, tell the user in Chinese to open 能力包.", Schema: []byte(`{"type":"object","properties":{"pluginId":{"type":"string","minLength":1,"maxLength":128},"name":{"type":"string","minLength":1,"maxLength":128},"kind":{"type":"string","enum":["skill","workflow","template","tool"]},"description":{"type":"string","maxLength":2000},"entrypoint":{"type":"string","maxLength":512},"semver":{"type":"string","maxLength":32},"publisher":{"type":"string","maxLength":128},"manifest":{"type":"object","description":"include skills, mcpPresetIds, toolGates arrays"}},"required":["pluginId","name","kind"],"additionalProperties":false}`)},
	}
}

// ccToolDefinitions are the M10 computer-control tools. They are appended
// to the model tool list only when the ccapp service is wired and the
// operator enabled the domain (M10-CC-012 keeps them hidden otherwise, and
// the armed emergency latch hides them too). Subagents never see them:
// readOnlyEngineToolDefinitions stays file-read-only and runs sub-sessions
// in FullAccess, which would bypass the confirmation gate.
func (e *Engine) ccToolDefinitions() []gateway.ToolDefinition {
	if e.ccctrl == nil {
		return nil
	}
	settings, err := e.ccctrl.GetConfig(context.Background())
	if err != nil || !settings.Enabled || settings.EmergencyStopped {
		return nil
	}
	return []gateway.ToolDefinition{
		{Name: "computer.act", Description: "Unified desktop action (OpenClaw-shaped). action=screenshot|click|double_click|right_click|move|drag|type|key|press|hold_key|key_up|scroll|wait|observe|observe_dialog|confirm|focus|list|paste|menu|set_value|clipboard|window_action. Click may pass modifiers=[ctrl|shift|alt|win] (held only around that click). hold_key holds a key; key_up releases it (auto-release after 8s). Default screenshot is the foreground window (target=foreground); target=desktop captures the virtual desktop. Pixel actions must echo frameId from the latest screenshot (id binds screenIndex + display topology; reconnect/DPI fails closed). Expands onto governed cc.* (audit, rate limit, emergency stop) — do not call cc.* yourself. Prefer name=/id= over raw x,y. Never click UAC or file Open/Save — the runtime will ask the user.", Schema: []byte(`{"type":"object","properties":{"action":{"type":"string","minLength":1,"maxLength":40},"frameId":{"type":"string","maxLength":40},"x":{"type":"integer","minimum":0,"maximum":65535},"y":{"type":"integer","minimum":0,"maximum":65535},"x1":{"type":"integer","minimum":0,"maximum":65535},"y1":{"type":"integer","minimum":0,"maximum":65535},"x2":{"type":"integer","minimum":0,"maximum":65535},"y2":{"type":"integer","minimum":0,"maximum":65535},"button":{"type":"string"},"clicks":{"type":"integer","minimum":1,"maximum":3},"modifiers":{"type":"array","maxItems":3,"items":{"type":"string","enum":["ctrl","shift","alt","win"]}},"scroll":{"type":"integer","minimum":-12,"maximum":12},"scrollAxis":{"type":"string","enum":["vertical","horizontal"]},"name":{"type":"string","maxLength":80},"id":{"type":"string","maxLength":8},"text":{"type":"string","maxLength":8192},"keys":{"type":"array","maxItems":4,"items":{"type":"string"}},"key":{"type":"string","maxLength":24},"count":{"type":"integer","minimum":1,"maximum":8},"window":{"type":"string","maxLength":200},"title":{"type":"string","maxLength":200},"process":{"type":"string","maxLength":200},"target":{"type":"string"},"ms":{"type":"integer","minimum":0,"maximum":8000},"until":{"type":"string","enum":["timeout","change"]},"maxNodes":{"type":"integer","minimum":0,"maximum":120},"path":{"type":"string","maxLength":240},"op":{"type":"string"},"value":{"type":"string","maxLength":4096},"w":{"type":"integer","minimum":1,"maximum":65535},"h":{"type":"integer","minimum":1,"maximum":65535}},"required":["action"],"additionalProperties":false}`)},
	}
}

const maxCaptureVisionImages = 4

func appendCaptureVision(images []gateway.Image, mime string, data []byte) []gateway.Image {
	if len(data) == 0 {
		return images
	}
	if mime == "" {
		mime = "image/png"
	}
	images = append(images, gateway.Image{MIME: mime, Data: data})
	if len(images) > maxCaptureVisionImages {
		images = images[len(images)-maxCaptureVisionImages:]
	}
	return images
}
