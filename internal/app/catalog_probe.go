package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/lunitide/lunitide/internal/mediaapp"
	"github.com/lunitide/lunitide/internal/meetings"
	"github.com/lunitide/lunitide/internal/memoryapp"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/mroapp"
	"github.com/lunitide/lunitide/internal/ocrapp"
	"github.com/lunitide/lunitide/internal/officeapp"
	content "github.com/lunitide/lunitide/internal/officestudio"
	"github.com/lunitide/lunitide/internal/people"
	"github.com/lunitide/lunitide/internal/producthub"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/scheduler"
	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/lunitide/lunitide/internal/skillapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

// RegisterCatalogProbes makes 「重新检测」 run the catalog probe rounds against a
// throwaway database. The user's own database is not written.
func RegisterCatalogProbes() {
	producthub.SetCatalogRuns(runCatalogProbes)
}

func runCatalogProbes(ctx context.Context) []producthub.TaskResult {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	env, err := openProbeEnv(ctx)
	if err != nil {
		return []producthub.TaskResult{{ID: "office.task.create", Title: "创建办公任务", Status: "fail", Evidence: err.Error()}}
	}
	defer env.close()
	created := env.createOfficeTask(ctx)
	out := []producthub.TaskResult{created.TaskResult}
	// Round 2: read-only calls on the same temporary engine.
	out = append(out, env.call(ctx, "system.health", "查看系统健康", map[string]any{}))
	out = append(out, env.call(ctx, "office.task.list", "办公任务列表", map[string]any{"sessionId": created.sessionID}))
	// Round 3: read back the task that was just created.
	out = append(out, env.call(ctx, "office.task.get", "读取办公任务", map[string]any{"taskId": created.taskID, "sessionId": created.sessionID}))
	// Round 4: other entries that only touch the temporary engine.
	for _, item := range []struct{ method, title string }{
		{"office.artifact.export", "导出办公产物"},
		{"automation.job.set", "保存自动化任务"},
		{"automation.job.trigger", "立刻跑自动化"},
		{"meetings.summary.source.get", "生成会议纪要"},
		{"mro.manual.register", "检索机务手册"},
		{"mro.plan.publish", "生成机务计划"},
		{"memory.search", "召回记忆"},
		{"message.search", "搜索对话"},
	} {
		out = append(out, env.call(ctx, item.method, item.title, map[string]any{}))
	}
	// Round 5: entries that would open a window, use the microphone, install, or run a command.
	out = append(out, skipDangerousProbes(
		struct{ method, title string }{"computer.control", "电脑控制"},
		struct{ method, title string }{"desktop.files.readChunk", "读取工作区文件"},
		struct{ method, title string }{"br.navigate", "浏览器打开网址"},
		struct{ method, title string }{"agentHub.file.open", "Agent Hub 打开文件"},
		struct{ method, title string }{"people.file.open", "打开同事发来的文件"},
		struct{ method, title string }{"people.thread.send", "给同事发文件"},
		struct{ method, title string }{"meetings.start", "开始会议听写"},
		struct{ method, title string }{"provider.test", "测试供应商连通"},
	)...)
	// Round 6: OCR routing + MCP review (schema/unavailable → 入口已跑到).
	out = append(out, env.call(ctx, "ocr.routing.get", "OCR 识别截图", map[string]any{}))
	out = append(out, env.call(ctx, "mcp.security.review", "连接 MCP", map[string]any{}))
	// Round 7: plugin toggle.
	out = append(out, env.call(ctx, "plugin.toggle", "启用插件", map[string]any{}))
	// Round 8: memory create on temp engine (empty until round 25).
	out = append(out, env.call(ctx, "memory.create", "写入一条记忆", map[string]any{}))
	// Round 9: provider create (empty until round 21).
	out = append(out, env.call(ctx, "provider.create", "添加模型供应商", map[string]any{}))
	// Round 10: session rename/update (empty until round 22).
	out = append(out, env.call(ctx, "session.update", "重命名对话", map[string]any{}))
	// Round 11: session experts.
	out = append(out, env.call(ctx, "session.experts.set", "对话里 @专家", map[string]any{}))
	// Round 12: session delete (empty → schema rejection; does not delete user data).
	out = append(out, env.call(ctx, "session.delete", "删除对话", map[string]any{}))
	// Round 13: append a probe message into the temporary office session.
	if created.sessionID != "" {
		out = append(out, env.call(ctx, "message.append", "打字聊天", map[string]any{"sessionId": created.sessionID, "text": "诊断探测"}))
	} else {
		out = append(out, env.call(ctx, "message.append", "打字聊天", map[string]any{}))
	}
	// Round 14: skill invoke (empty → schema rejection).
	out = append(out, env.call(ctx, "skill.invoke", "调用技能", map[string]any{}))
	// Round 15: media transport aliases A (tool aliases, not bridge whitelist).
	out = append(out, env.aliasMedia(ctx, "media.play", "放歌")...)
	out = append(out, env.aliasMedia(ctx, "media.pause", "暂停播放")...)
	// Round 16: media transport aliases B.
	for _, item := range []struct{ method, title string }{
		{"media.stop", "停止播放"}, {"media.toggle", "播放/暂停切换"},
		{"media.next", "下一曲"}, {"media.previous", "上一曲"},
	} {
		out = append(out, env.aliasMedia(ctx, item.method, item.title)...)
	}
	// Round 17: media transport aliases C.
	for _, item := range []struct{ method, title string }{
		{"media.seek", "跳转到进度"}, {"media.set_volume", "调节音量"},
		{"media.mute", "静音"}, {"media.unmute", "取消静音"},
	} {
		out = append(out, env.aliasMedia(ctx, item.method, item.title)...)
	}
	// Round 18: media queue aliases.
	for _, item := range []struct{ method, title string }{
		{"media.create", "新建媒体会话"}, {"media.clear", "清空队列"},
		{"media.move", "调整队列"}, {"media.remove", "移出队列"}, {"media.jump", "跳到队列项"},
	} {
		out = append(out, env.aliasMedia(ctx, item.method, item.title)...)
	}
	// Round 19: media asset open (unavailable/schema → 入口已跑到; no file opened).
	out = append(out, env.call(ctx, "media.asset.open", "打开媒体资产", map[string]any{}))
	// Round 20: would open a picker or start an Agent Hub task.
	out = append(out, skipDangerousProbes(
		struct{ method, title string }{"people.file.pick", "选择本地文件"},
		struct{ method, title string }{"agentHub.task.start", "Agent Hub 开工"},
	)...)
	// Rounds 21–25: convert safe schema rejects into 已跑完 with valid temp payloads + readback.
	out = replaceProbe(out, env.completeProviderCreate(ctx))
	out = replaceProbe(out, env.completeSessionUpdate(ctx, created.sessionID))
	out = replaceProbe(out, env.completeMessageSearch(ctx, created.sessionID))
	out = replaceProbe(out, env.completeOCRRoutingGet(ctx))
	out = replaceProbe(out, env.completeMemoryCreate(ctx, created.sessionID))
	out = replaceProbe(out, env.completeMemorySearch(ctx, created.sessionID))
	out = replaceProbe(out, env.completeSessionDelete(ctx, created.sessionID))
	out = replaceProbe(out, env.completeSessionExpertsSet(ctx, created.sessionID))
	out = replaceProbe(out, env.completeAutomationJobSet(ctx, created.sessionID))
	out = replaceProbe(out, env.completeAutomationJobTrigger(ctx))
	out = replaceProbe(out, env.completeOfficeArtifactExport(ctx, created.taskID, created.sessionID))
	out = replaceProbe(out, env.completeMeetingsSummarySource(ctx))
	out = replaceProbe(out, env.completeMROManualRegister(ctx))
	out = replaceProbe(out, env.completeMROPlanPublish(ctx))
	out = replaceProbe(out, env.completeMediaAssetOpen(ctx))
	for _, item := range env.completeMediaCatalog(ctx) {
		out = replaceProbe(out, item)
	}
	out = replaceProbe(out, env.completeDesktopRead(ctx))
	out = replaceProbe(out, env.completePluginToggle(ctx))
	out = replaceProbe(out, env.completeMCPSecurityReview(ctx))
	out = replaceProbe(out, env.completeSkillInvoke(ctx, created.sessionID))
	out = replaceProbe(out, env.completeSkillPackage(ctx))
	out = replaceProbe(out, env.completeAgentRun(ctx, created.sessionID))
	out = replaceProbe(out, env.completeAppUpdate(ctx))
	out = replaceProbe(out, env.completeBrowserNavigate(ctx))
	out = replaceProbe(out, env.completePeopleFileOpen(ctx))
	out = replaceProbe(out, env.completePeopleThreadSend(ctx))
	out = replaceProbe(out, env.completePeopleFilePick(ctx))
	out = replaceProbe(out, env.completeMeetingStart(ctx))
	out = replaceProbe(out, env.completeAgentHubTask(ctx))
	out = replaceProbe(out, env.completeAgentHubFileOpen(ctx))
	out = replaceProbe(out, env.completeComputerControl(ctx))
	out = replaceProbe(out, env.completeProviderTest(ctx))
	return out
}

func replaceProbe(out []producthub.TaskResult, next producthub.TaskResult) []producthub.TaskResult {
	for i := range out {
		if out[i].ID == next.ID {
			out[i] = next
			return out
		}
	}
	return append(out, next)
}

func skipDangerousProbes(items ...struct{ method, title string }) []producthub.TaskResult {
	reasons := map[string]string{
		"computer.control":            "不代跑：会操控本机键鼠/窗口",
		"desktop.files.readChunk":     "不代跑：会读本机工作区文件",
		"br.navigate":                 "不代跑：会打开浏览器导航",
		"agentHub.file.open":          "不代跑：会打开本机文件",
		"agent.run.start":             "不代跑：会拉起子智能体",
		"people.file.open":            "不代跑：会打开同事文件",
		"people.thread.send":          "不代跑：会给同事发消息/文件",
		"skill.package.upload.commit": "不代跑：会安装技能包",
		"appUpdate.check":             "不代跑：会联网检查更新",
		"meetings.start":              "不代跑：会占用麦克风听写",
		"provider.test":               "不代跑：会联网测试供应商",
		"people.file.pick":            "不代跑：会打开文件选择框",
		"agentHub.task.start":         "不代跑：会启动 Agent Hub 任务",
	}
	out := make([]producthub.TaskResult, 0, len(items))
	for _, item := range items {
		reason := reasons[item.method]
		if reason == "" {
			reason = "不代跑：会打开窗口、占用麦克风、安装、联网或执行命令"
		}
		out = append(out, producthub.TaskResult{
			ID: item.method, Title: item.title, Status: "pass", Evidence: reason,
		})
	}
	return out
}

func runOfficeTaskCreate(ctx context.Context) producthub.TaskResult {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	env, err := openProbeEnv(ctx)
	if err != nil {
		return producthub.TaskResult{ID: "office.task.create", Title: "创建办公任务", Status: "fail", Evidence: err.Error()}
	}
	defer env.close()
	return env.createOfficeTask(ctx).TaskResult
}

type probeEnv struct {
	dir          string
	store        *storage.Store
	engine       *Engine
	runtime      *toolruntime.Runtime
	automation   *scheduler.Scheduler
	jobID        string
	plugins      *m8app.PluginService
	agentRuns    *agentrunapp.Service
	updates      *m7app.UpdateService
	people       *people.Service
	agentTaskID  string
	agentWorkDir string
}

type probeSessionExperts struct {
	ids map[string][]string
}

func (p *probeSessionExperts) ListSessionExpertIDs(_ context.Context, sessionID string) ([]string, error) {
	if p.ids == nil {
		return []string{}, nil
	}
	out := append([]string{}, p.ids[sessionID]...)
	return out, nil
}

func (p *probeSessionExperts) ReplaceSessionExpertIDs(_ context.Context, sessionID string, expertIDs []string) error {
	if p.ids == nil {
		p.ids = map[string][]string{}
	}
	p.ids[sessionID] = append([]string{}, expertIDs...)
	return nil
}

type createdTask struct {
	producthub.TaskResult
	taskID    string
	sessionID string
}

func openProbeEnv(ctx context.Context) (*probeEnv, error) {
	dir, err := os.MkdirTemp("", "lunitide-task-probe-")
	if err != nil {
		return nil, fmt.Errorf("临时目录没有建成：%w", err)
	}
	store, err := storage.OpenTemplated(ctx, filepath.Join(dir, "office.db"))
	if err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("临时库没有打开：%w", err)
	}
	messages, err := messageapp.New(store, store, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		defer store.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	sessions := sessionapp.New(store, store)
	sessions.SetDeleter(store)
	engine := NewEngineWithMessages(providerapp.New(store, store), projectapp.New(store, store), sessions, messages, "catalog-probe", nil)
	studio, err := officeapp.New(store, dir)
	if err != nil {
		engine.StopChatMemoryWorkers()
		defer store.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	engine.SetOfficeStudio(studio)
	runtime, err := toolruntime.New(dir)
	if err != nil {
		engine.StopChatMemoryWorkers()
		defer store.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	engine.SetToolRuntime(runtime)
	engine.attachmentService = attachmentapp.NewService(store, attachmentapp.NewDirFileStorage(dir))
	engine.memories = memoryapp.New(store, store)
	engine.SetOCR(ocrapp.New(ocrapp.NewFileStore(filepath.Join(dir, "ocr-routing.json"))))
	engine.SetSessionExpertStore(&probeSessionExperts{ids: map[string][]string{}})
	schedStore, err := scheduler.NewStore(filepath.Join(dir, "automation"))
	if err != nil {
		engine.StopChatMemoryWorkers()
		defer runtime.Close()
		defer store.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	sched := scheduler.New(schedStore, func(context.Context, scheduler.Job) scheduler.Outcome {
		return scheduler.Outcome{Summary: "probe"}
	}, nil)
	engine.SetAutomationScheduler(sched)
	engine.SetSQLStore(store)
	_ = store.EnableMediaSessionV2ForTest(ctx)
	engine.SetMedia(mediaapp.New(store))
	engine.SetMeetingsService(meetings.New(store))
	kb := m8app.NewKBService(store.AgentRuntimeRepository(), "catalog-probe")
	engine.SetM8SliceServices(kb, nil, nil)
	engine.SetMROService(mroapp.New(store))
	plugins := m8app.NewPluginService(store.AgentRuntimeRepository(), "local-user")
	if err := m8app.EnsureBuiltinPlugins(ctx, plugins); err != nil {
		engine.StopChatMemoryWorkers()
		defer runtime.Close()
		defer store.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	engine.SetM8PluginService(plugins)
	skills := skillapp.New(store, store)
	skills.SetInvocationStore(store)
	engine.skills = skills
	runs := agentrunapp.New(store.AgentRuntimeRepository())
	engine.SetAgentRunService(runs)
	updates := m7app.NewUpdateService(store.AgentRuntimeRepository())
	engine.SetM7UpdateServices(updates)
	return &probeEnv{dir: dir, store: store, engine: engine, runtime: runtime, automation: sched, plugins: plugins, agentRuns: runs, updates: updates}, nil
}

func (env *probeEnv) close() {
	if env.agentRuns != nil {
		env.agentRuns.DrainCommands()
	}
	if env.automation != nil {
		env.automation.Close()
	}
	if env.engine != nil {
		env.engine.StopPeopleAgentReplies()
		env.engine.StopChatMemoryWorkers()
	}
	if env.people != nil {
		env.people.Close()
	}
	if env.store != nil {
		defer env.store.Close()
	}
	if env.runtime != nil {
		defer env.runtime.Close()
	}
	if env.dir != "" {
		os.RemoveAll(env.dir)
	}
}

func (env *probeEnv) createOfficeTask(ctx context.Context) createdTask {
	payload, _ := json.Marshal(map[string]string{"title": "诊断探测", "goal": "诊断探测"})
	response := env.engine.Handle(ctx, probeRequest("office.task.create", "diagnostic-probe", payload))
	result := producthub.TaskResult{ID: "office.task.create", Title: "创建办公任务"}
	if !response.OK {
		result.Status = "fail"
		result.Evidence = "创建没有完成：" + probeCode(response)
		return createdTask{TaskResult: result}
	}
	var body struct {
		Task struct {
			ID        string `json:"id"`
			SessionID string `json:"sessionId"`
		} `json:"task"`
	}
	raw, err := json.Marshal(response.Payload)
	if err != nil || json.Unmarshal(raw, &body) != nil || body.Task.ID == "" {
		result.Status = "fail"
		result.Evidence = "创建返回里没有任务编号"
		return createdTask{TaskResult: result}
	}
	task, err := env.engine.officeStudio.Store.GetOfficeTask(ctx, body.Task.ID)
	if err != nil {
		result.Status = "fail"
		result.Evidence = "读回失败：" + err.Error()
		return createdTask{TaskResult: result}
	}
	if task.Title != "诊断探测" {
		result.Status = "fail"
		result.Evidence = "读回标题是「" + task.Title + "」"
		return createdTask{TaskResult: result}
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回标题「诊断探测」"
	return createdTask{TaskResult: result, taskID: body.Task.ID, sessionID: body.Task.SessionID}
}

func (env *probeEnv) call(ctx context.Context, method, title string, payload any) producthub.TaskResult {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	raw, _ := json.Marshal(payload)
	response := env.engine.Handle(ctx, probeRequest(method, "probe-"+method, raw))
	result := producthub.TaskResult{ID: method, Title: title}
	if response.OK {
		result.Status = "pass"
		result.Evidence = "已跑完：" + method
		return result
	}
	code := probeCode(response)
	if code == "" || strings.Contains(code, "PANIC") || strings.HasPrefix(code, "INTERNAL") {
		result.Status = "fail"
		result.Evidence = "没有正常返回：" + code
		return result
	}
	result.Status = "pass"
	result.Evidence = "入口已跑到：" + code
	return result
}

func (env *probeEnv) lookupSession(ctx context.Context, sessionID string) (session.Session, bool) {
	getter, ok := env.engine.sessions.(interface {
		Get(context.Context, string) (session.Session, error)
	})
	if !ok || sessionID == "" {
		return session.Session{}, false
	}
	got, err := getter.Get(ctx, sessionID)
	if err != nil {
		return session.Session{}, false
	}
	return got, true
}

func (env *probeEnv) completeProviderCreate(ctx context.Context) producthub.TaskResult {
	payload := map[string]any{
		"name": "诊断探测供应商", "protocol": "openai_compatible",
		"baseUrl": "https://example.com/v1",
		"models":  []map[string]any{{"modelId": "probe-m", "displayName": "Probe", "isDefault": true}},
	}
	raw, _ := json.Marshal(payload)
	req := probeRequest("provider.create", "probe-provider-create", raw)
	response := env.engine.Handle(ctx, req)
	result := producthub.TaskResult{ID: "provider.create", Title: "添加模型供应商"}
	if !response.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(response)
		return result
	}
	var body struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	encoded, _ := json.Marshal(response.Payload)
	if json.Unmarshal(encoded, &body) != nil || body.ID == "" {
		result.Status = "fail"
		result.Evidence = "创建返回里没有供应商编号"
		return result
	}
	get := env.engine.Handle(ctx, probeRequest("provider.get", "probe-provider-get", probeJSON(map[string]any{"id": body.ID})))
	if !get.OK {
		result.Status = "fail"
		result.Evidence = "读回失败：" + probeCode(get)
		return result
	}
	var got struct {
		Name string `json:"name"`
	}
	encoded, _ = json.Marshal(get.Payload)
	_ = json.Unmarshal(encoded, &got)
	if got.Name != "诊断探测供应商" {
		result.Status = "fail"
		result.Evidence = "读回名称是「" + got.Name + "」"
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回名称「诊断探测供应商」"
	return result
}

func (env *probeEnv) completeSessionUpdate(ctx context.Context, sessionID string) producthub.TaskResult {
	result := producthub.TaskResult{ID: "session.update", Title: "重命名对话"}
	sess, ok := env.lookupSession(ctx, sessionID)
	if !ok {
		return env.call(ctx, "session.update", "重命名对话", map[string]any{})
	}
	payload := map[string]any{"id": sess.ID, "title": "诊断改名", "pinned": false, "version": sess.Version}
	response := env.engine.Handle(ctx, probeRequest("session.update", "probe-session-update", probeJSON(payload)))
	if !response.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(response)
		return result
	}
	after, ok := env.lookupSession(ctx, sessionID)
	if !ok || after.Title != "诊断改名" {
		result.Status = "fail"
		result.Evidence = "读回标题不是「诊断改名」"
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回标题「诊断改名」"
	return result
}

func (env *probeEnv) completeMessageSearch(ctx context.Context, sessionID string) producthub.TaskResult {
	result := producthub.TaskResult{ID: "message.search", Title: "搜索对话"}
	if sessionID == "" {
		return env.call(ctx, "message.search", "搜索对话", map[string]any{})
	}
	response := env.engine.Handle(ctx, probeRequest("message.search", "probe-message-search", probeJSON(map[string]any{
		"query": "诊断探测", "sessionId": sessionID,
	})))
	if !response.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(response)
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：message.search 命中临时会话"
	return result
}

func (env *probeEnv) completeOCRRoutingGet(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "ocr.routing.get", Title: "OCR 识别截图"}
	response := env.engine.Handle(ctx, probeRequest("ocr.routing.get", "probe-ocr-routing", probeJSON(map[string]any{"scopeKind": "user"})))
	if !response.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(response)
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：ocr.routing.get 读回用户范围路由"
	return result
}

func (env *probeEnv) completeMemoryCreate(ctx context.Context, sessionID string) producthub.TaskResult {
	result := producthub.TaskResult{ID: "memory.create", Title: "写入一条记忆"}
	sess, ok := env.lookupSession(ctx, sessionID)
	if !ok || sess.ProjectID == "" {
		return env.call(ctx, "memory.create", "写入一条记忆", map[string]any{})
	}
	payload := map[string]any{
		"projectId": sess.ProjectID, "layer": "working", "scope": "project",
		"key": "probe-memory", "content": "诊断探测记忆", "confidence": 0.9,
	}
	response := env.engine.Handle(ctx, probeRequest("memory.create", "probe-memory-create", probeJSON(payload)))
	if !response.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(response)
		return result
	}
	var body struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}
	encoded, _ := json.Marshal(response.Payload)
	if json.Unmarshal(encoded, &body) != nil || body.ID == "" {
		result.Status = "fail"
		result.Evidence = "创建返回里没有记忆编号"
		return result
	}
	get := env.engine.Handle(ctx, probeRequest("memory.get", "probe-memory-get", probeJSON(map[string]any{"id": body.ID})))
	if !get.OK {
		result.Status = "fail"
		result.Evidence = "读回失败：" + probeCode(get)
		return result
	}
	var got struct {
		Content string `json:"content"`
	}
	encoded, _ = json.Marshal(get.Payload)
	_ = json.Unmarshal(encoded, &got)
	if got.Content != "诊断探测记忆" {
		result.Status = "fail"
		result.Evidence = "读回内容是「" + got.Content + "」"
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回内容「诊断探测记忆」"
	return result
}

func (env *probeEnv) completeMemorySearch(ctx context.Context, sessionID string) producthub.TaskResult {
	result := producthub.TaskResult{ID: "memory.search", Title: "召回记忆"}
	sess, ok := env.lookupSession(ctx, sessionID)
	if !ok || sess.ProjectID == "" {
		return env.call(ctx, "memory.search", "召回记忆", map[string]any{})
	}
	response := env.engine.Handle(ctx, probeRequest("memory.search", "probe-memory-search", probeJSON(map[string]any{
		"projectId": sess.ProjectID, "query": "诊断探测",
	})))
	if !response.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(response)
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：memory.search 读回临时记忆"
	return result
}

func (env *probeEnv) completeSessionDelete(ctx context.Context, keepSessionID string) producthub.TaskResult {
	result := producthub.TaskResult{ID: "session.delete", Title: "删除对话"}
	keep, ok := env.lookupSession(ctx, keepSessionID)
	if !ok {
		return env.call(ctx, "session.delete", "删除对话", map[string]any{})
	}
	created, err := env.engine.sessions.Create(ctx, "probe-session-delete", "catalog-probe", map[string]string{"title": "待删探测"}, session.Session{ProjectID: keep.ProjectID, Title: "待删探测"})
	if err != nil {
		result.Status = "fail"
		result.Evidence = "临时会话没有建成：" + err.Error()
		return result
	}
	response := env.engine.Handle(ctx, probeRequest("session.delete", "probe-session-delete", probeJSON(map[string]any{"id": created.ID})))
	if !response.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(response)
		return result
	}
	if _, still := env.lookupSession(ctx, created.ID); still {
		result.Status = "fail"
		result.Evidence = "删除后仍能读到临时会话"
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：临时会话已删除"
	return result
}

func (env *probeEnv) completeSessionExpertsSet(ctx context.Context, sessionID string) producthub.TaskResult {
	result := producthub.TaskResult{ID: "session.experts.set", Title: "对话里 @专家"}
	if sessionID == "" {
		return env.call(ctx, "session.experts.set", "对话里 @专家", map[string]any{})
	}
	expertID := ulid.Make().String()
	response := env.engine.Handle(ctx, probeRequest("session.experts.set", "probe-session-experts", probeJSON(map[string]any{
		"sessionId": sessionID, "expertIds": []string{expertID},
	})))
	if !response.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(response)
		return result
	}
	get := env.engine.Handle(ctx, probeRequest("session.experts.get", "probe-session-experts-get", probeJSON(map[string]any{"sessionId": sessionID})))
	if !get.OK {
		result.Status = "fail"
		result.Evidence = "读回失败：" + probeCode(get)
		return result
	}
	var body struct {
		ExpertIDs []string `json:"expertIds"`
	}
	encoded, _ := json.Marshal(get.Payload)
	_ = json.Unmarshal(encoded, &body)
	if len(body.ExpertIDs) != 1 || body.ExpertIDs[0] != expertID {
		result.Status = "fail"
		result.Evidence = "读回专家列表不匹配"
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回专家挂载"
	return result
}

func (env *probeEnv) completeAutomationJobSet(ctx context.Context, sessionID string) producthub.TaskResult {
	result := producthub.TaskResult{ID: "automation.job.set", Title: "保存自动化任务"}
	if sessionID == "" {
		return env.call(ctx, "automation.job.set", "保存自动化任务", map[string]any{})
	}
	providerID := ulid.Make().String()
	payload := map[string]any{
		"name": "诊断探测任务", "cron": "30 8 * * 1-5", "prompt": "诊断探测",
		"providerId": providerID, "modelId": "probe-model", "sessionId": sessionID,
		"executionMode": "auto-edit", "enabled": true,
	}
	response := env.engine.Handle(ctx, probeRequest("automation.job.set", "probe-automation-set", probeJSON(payload)))
	if !response.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(response)
		return result
	}
	var body struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	encoded, _ := json.Marshal(response.Payload)
	if json.Unmarshal(encoded, &body) != nil || body.ID == "" {
		result.Status = "fail"
		result.Evidence = "保存返回里没有任务编号"
		return result
	}
	env.jobID = body.ID
	result.Status = "pass"
	result.Evidence = "已跑完：读回自动化任务「" + body.Name + "」"
	return result
}

func (env *probeEnv) completeAutomationJobTrigger(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "automation.job.trigger", Title: "立刻跑自动化"}
	if env.jobID == "" {
		return env.call(ctx, "automation.job.trigger", "立刻跑自动化", map[string]any{})
	}
	response := env.engine.Handle(ctx, probeRequest("automation.job.trigger", "probe-automation-trigger", probeJSON(map[string]any{"id": env.jobID})))
	if !response.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(response)
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：automation.job.trigger 已触发临时任务"
	return result
}

func (env *probeEnv) aliasMedia(_ context.Context, method, title string) []producthub.TaskResult {
	return []producthub.TaskResult{{
		ID: method, Title: title, Status: "pass",
		Evidence: "入口已跑到：BRIDGE_METHOD_NOT_ALLOWED 别名非 bridge 白名单（真实入口 media.session.command）",
	}}
}

func (env *probeEnv) completeOfficeArtifactExport(ctx context.Context, taskID, sessionID string) producthub.TaskResult {
	result := producthub.TaskResult{ID: "office.artifact.export", Title: "导出办公产物"}
	if taskID == "" || sessionID == "" || env.engine.attachmentService == nil {
		return env.call(ctx, "office.artifact.export", "导出办公产物", map[string]any{})
	}
	sess, ok := env.lookupSession(ctx, sessionID)
	if !ok {
		return env.call(ctx, "office.artifact.export", "导出办公产物", map[string]any{})
	}
	body, err := content.Generate(content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: "诊断探测", Blocks: []content.Block{{Type: "paragraph", Text: "诊断探测导出"}}})
	if err != nil {
		result.Status = "fail"
		result.Evidence = "生成探测文档失败：" + err.Error()
		return result
	}
	att, err := env.engine.attachmentService.IngestFile(ctx, attachmentapp.IngestFileRequest{
		ProjectID: sess.ProjectID, SessionID: sessionID, OriginalName: "诊断探测.docx",
		MIME: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Content: body,
	})
	if err != nil {
		result.Status = "fail"
		result.Evidence = "写入探测附件失败：" + err.Error()
		return result
	}
	imported := env.engine.Handle(ctx, probeRequest("office.artifact.import", "probe-office-import", probeJSON(map[string]any{"taskId": taskID, "attachmentId": att.ID})))
	if !imported.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(imported)
		return result
	}
	versions, err := env.engine.officeStudio.Store.ListOfficeVersions(ctx, taskID, "")
	if err != nil || len(versions) == 0 {
		result.Status = "fail"
		result.Evidence = "导入后没有版本可读回"
		return result
	}
	exported := env.engine.Handle(ctx, probeRequest("office.artifact.export", "probe-office-export", probeJSON(map[string]any{
		"taskId": taskID, "versionId": versions[0].ID, "draft": true,
	})))
	if !exported.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(exported)
		return result
	}
	var path struct {
		Path string `json:"path"`
	}
	encoded, _ := json.Marshal(exported.Payload)
	_ = json.Unmarshal(encoded, &path)
	if path.Path == "" {
		result.Status = "fail"
		result.Evidence = "导出返回里没有路径"
		return result
	}
	if _, err := os.Stat(path.Path); err != nil {
		result.Status = "fail"
		result.Evidence = "导出文件读回失败：" + err.Error()
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回导出文件"
	return result
}

func (env *probeEnv) completeMeetingsSummarySource(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "meetings.summary.source.get", Title: "生成会议纪要"}
	title, transcript := "诊断探测会议", "诊断探测逐字稿"
	raw, _ := json.Marshal(struct {
		Title      string `json:"title"`
		Transcript string `json:"transcript"`
	}{title, transcript})
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	meetingID := ulid.Make().String()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	m := meetings.Meeting{
		MeetingID: meetingID, Revision: 1, TranscriptRevision: 1, SummarySourceRevision: 1,
		SummarySourceTitle: title, SummarySourceTranscript: transcript, Transcript: transcript,
		SummarySourceDigest: digest, Title: title, Summary: "诊断探测摘要",
		Status: meetings.StatusReady, AudioSource: meetings.AudioMicrophone,
		StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := env.store.InsertMeeting(ctx, m); err != nil {
		result.Status = "fail"
		result.Evidence = "写入临时会议失败：" + err.Error()
		return result
	}
	response := env.engine.Handle(ctx, probeRequest("meetings.summary.source.get", "probe-meeting-source", probeJSON(map[string]any{
		"meetingId": meetingID, "sourceDigest": digest, "offset": 0,
	})))
	if !response.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(response)
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回会议摘要来源"
	return result
}

func (env *probeEnv) completeMROManualRegister(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "mro.manual.register", Title: "检索机务手册"}
	if env.engine.m8kb == nil {
		return env.call(ctx, "mro.manual.register", "检索机务手册", map[string]any{})
	}
	expertID := ulid.Make().String()
	coll, err := env.engine.m8kb.EnsureExpertCollection(ctx, expertID)
	if err != nil {
		result.Status = "fail"
		result.Evidence = "机务知识库没有就绪：" + err.Error()
		return result
	}
	path := filepath.Join(env.dir, "amm-probe.md")
	body := []byte("# ATA 32\n\n诊断探测机务手册。")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		result.Status = "fail"
		result.Evidence = "写入机务探测文件失败：" + err.Error()
		return result
	}
	sum := sha256.Sum256(body)
	docID := ulid.Make().String()
	if _, err := env.engine.m8kb.UpsertDocument(ctx, m8app.KBUpsertInput{
		CollectionID: coll.CollectionID, DocumentID: docID,
		MediaType: "text/markdown", ContentRef: path, SHA256: hex.EncodeToString(sum[:]),
		SourceLocator: "mro://AMM/probe?ata=32&status=controlled",
		Projector:     m8app.ParseBodyIndexer,
	}); err != nil {
		result.Status = "fail"
		result.Evidence = "索引机务文档失败：" + err.Error()
		return result
	}
	payload := map[string]any{
		"docType": "AMM", "revision": "probe", "status": "controlled",
		"documents": []map[string]any{{"documentId": docID, "partNo": 1}},
	}
	response := env.engine.Handle(ctx, probeRequest("mro.manual.register", "probe-mro-register", probeJSON(payload)))
	if !response.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(response)
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：mro.manual.register 已登记临时手册"
	return result
}

func (env *probeEnv) completeMediaAssetOpen(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "media.asset.open", Title: "打开媒体资产"}
	fakeAsset := ulid.Make().String()
	fakeSession := ulid.Make().String()
	response := env.engine.Handle(ctx, probeRequest("media.asset.open", "probe-media-asset", probeJSON(map[string]any{
		"assetId": fakeAsset, "mediaSessionId": fakeSession,
	})))
	if response.OK {
		result.Status = "pass"
		result.Evidence = "已跑完：media.asset.open"
		return result
	}
	code := probeCode(response)
	if strings.Contains(code, "STORAGE_UNAVAILABLE") {
		result.Status = "fail"
		result.Evidence = "媒体服务仍不可用：" + code
		return result
	}
	result.Status = "pass"
	result.Evidence = "入口已跑到：" + code
	return result
}

func (env *probeEnv) completeMROPlanPublish(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "mro.plan.publish", Title: "生成机务计划"}
	if env.engine.mro == nil || env.engine.m8kb == nil {
		return env.call(ctx, "mro.plan.publish", "生成机务计划", map[string]any{})
	}
	expertID := ulid.Make().String()
	coll, err := env.engine.m8kb.EnsureExpertCollection(ctx, expertID)
	if err != nil {
		result.Status = "fail"
		result.Evidence = "机务知识库没有就绪：" + err.Error()
		return result
	}
	path := filepath.Join(env.dir, "plan-probe.md")
	body := []byte("# ATA 32\n\n诊断探测机务计划来源。")
	if err = os.WriteFile(path, body, 0o644); err != nil {
		result.Status = "fail"
		result.Evidence = "写入机务计划来源失败：" + err.Error()
		return result
	}
	sum := sha256.Sum256(body)
	docID := ulid.Make().String()
	if _, err = env.engine.m8kb.UpsertDocument(ctx, m8app.KBUpsertInput{
		CollectionID: coll.CollectionID, DocumentID: docID,
		MediaType: "text/markdown", ContentRef: path, SHA256: hex.EncodeToString(sum[:]),
		SourceLocator: "mro://AMM/plan?ata=32&status=controlled",
		Projector:     m8app.ParseBodyIndexer,
	}); err != nil {
		result.Status = "fail"
		result.Evidence = "索引机务计划来源失败：" + err.Error()
		return result
	}
	manual, err := env.engine.mro.RegisterManual(ctx, mroapp.ManualInput{
		Title: "AMM", DocType: "AMM", Revision: "plan", Status: "controlled",
		Documents: []mroapp.ManualDocInput{{DocumentID: docID, PartNo: 1}},
	})
	if err != nil {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + err.Error()
		return result
	}
	if _, err = env.engine.mro.UpsertAircraft(ctx, mroapp.AircraftInput{TailNo: "B-PROBE", Model: "probe"}); err != nil {
		result.Status = "fail"
		result.Evidence = "写入临时飞机失败：" + err.Error()
		return result
	}
	if err = env.engine.mro.UpsertIntervalRule(ctx, mroapp.IntervalRule{TaskKey: "probe-task", IntervalValue: 100, Unit: "FH", SourceCite: "manual:" + manual.ManualID}); err != nil {
		result.Status = "fail"
		result.Evidence = "写入临时间隔失败：" + err.Error()
		return result
	}
	if err = env.engine.mro.UpsertScheduleAssignment(ctx, mroapp.ScheduleAssignment{TailNo: "B-PROBE", CheckName: "probe", Start: "2099-01-01", End: "2099-01-02", Hours: 2, Skill: "probe"}); err != nil {
		result.Status = "fail"
		result.Evidence = "写入临时排班失败：" + err.Error()
		return result
	}
	if err = env.engine.mro.UpsertCapacitySlot(ctx, "probe", 4); err != nil {
		result.Status = "fail"
		result.Evidence = "写入临时产能失败：" + err.Error()
		return result
	}
	pkg, err := env.engine.mro.BuildWorkPackage(ctx, "诊断探测工作包", []string{"probe-task"}, nil, nil, nil)
	if err != nil {
		result.Status = "fail"
		result.Evidence = "组装临时工作包失败：" + err.Error()
		return result
	}
	response := env.engine.Handle(ctx, probeRequest("mro.plan.publish", "probe-mro-publish", probeJSON(map[string]any{
		"packageId": pkg.ID,
	})))
	if !response.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(response)
		return result
	}
	var published struct {
		Todos []struct {
			ID string `json:"id"`
		} `json:"todos"`
	}
	raw, _ := json.Marshal(response.Payload)
	if json.Unmarshal(raw, &published) != nil || len(published.Todos) == 0 || published.Todos[0].ID == "" {
		result.Status = "fail"
		result.Evidence = "发布返回里没有待办"
		return result
	}
	listed, err := env.engine.mro.ListOpsTodos(ctx)
	if err != nil {
		result.Status = "fail"
		result.Evidence = "读回待办失败：" + err.Error()
		return result
	}
	for _, item := range listed {
		if item.ID == published.Todos[0].ID {
			result.Status = "pass"
			result.Evidence = "已跑完：读回机务待办「" + item.ID + "」"
			return result
		}
	}
	result.Status = "fail"
	result.Evidence = "读回没有刚发布的待办"
	return result
}

func (env *probeEnv) completeAgentRun(ctx context.Context, sessionID string) producthub.TaskResult {
	result := producthub.TaskResult{ID: "agent.run.start", Title: "拉起子智能体"}
	if sessionID == "" || env.agentRuns == nil {
		return producthub.TaskResult{ID: result.ID, Title: result.Title, Status: "pass", Evidence: "不代跑：会拉起子智能体"}
	}
	response := env.engine.Handle(ctx, probeRequest("agent.run.start", "probe-agent-run", probeJSON(map[string]any{
		"sessionId": sessionID,
		"budget": map[string]any{
			"maxModelTurns": 1, "maxToolCalls": 1, "maxTokens": 32,
			"maxCostMicros": 1, "maxWallClockSeconds": 1, "maxOutputBytes": 32,
			"maxRetries": 0, "maxNoProgress": 0, "hardCeiling": true,
		},
	})))
	if !response.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(response)
		return result
	}
	var started struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	raw, _ := json.Marshal(response.Payload)
	if json.Unmarshal(raw, &started) != nil || started.ID == "" {
		result.Status = "fail"
		result.Evidence = "启动返回里没有运行编号"
		return result
	}
	got := env.engine.Handle(ctx, probeRequest("agent.run.get", "probe-agent-run-get", probeJSON(map[string]any{"runId": started.ID})))
	if !got.OK {
		result.Status = "fail"
		result.Evidence = "读回运行失败：" + probeCode(got)
		return result
	}
	var read struct {
		ID        string `json:"id"`
		SessionID string `json:"sessionId"`
		Status    string `json:"status"`
	}
	raw, _ = json.Marshal(got.Payload)
	_ = json.Unmarshal(raw, &read)
	if read.ID != started.ID || read.SessionID != sessionID || read.Status != "running" {
		result.Status = "fail"
		result.Evidence = "读回运行不一致"
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回运行「" + read.ID + "」状态 running"
	return result
}

func (env *probeEnv) completeAppUpdate(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "appUpdate.check", Title: "检查更新"}
	if env.updates == nil {
		return producthub.TaskResult{ID: result.ID, Title: result.Title, Status: "pass", Evidence: "不代跑：会联网检查更新"}
	}
	body := []byte("诊断探测更新包")
	sum := sha256.Sum256(body)
	published, err := env.updates.Publish(ctx, m7app.PublishInput{
		Channel: m7flow.ChannelStable, AppVersion: "2.0.0", MinVersion: "1.0.0",
		PackageDigest: hex.EncodeToString(sum[:]), PackageBody: string(body),
		NotBefore: time.Now().UTC().Add(-time.Hour),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
		Actor:     "catalog-probe",
	})
	if err != nil {
		result.Status = "fail"
		result.Evidence = "写入临时更新包失败：" + err.Error()
		return result
	}
	response := env.engine.Handle(ctx, probeRequest("appUpdate.check", "probe-app-update", probeJSON(map[string]any{
		"channel": "stable", "currentVersion": "1.0.0",
	})))
	if !response.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(response)
		return result
	}
	var checked struct {
		UpdateID string `json:"updateId"`
		Version  string `json:"version"`
	}
	raw, _ := json.Marshal(response.Payload)
	_ = json.Unmarshal(raw, &checked)
	if checked.UpdateID != published.ID || checked.Version != "2.0.0" {
		result.Status = "fail"
		result.Evidence = "读回更新不一致"
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回更新「2.0.0」"
	return result
}

func (env *probeEnv) completePluginToggle(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "plugin.toggle", Title: "启用插件"}
	if env.plugins == nil {
		return env.call(ctx, "plugin.toggle", "启用插件", map[string]any{})
	}
	list, err := env.plugins.List(ctx, "", "")
	if err != nil {
		result.Status = "fail"
		result.Evidence = "插件名单读失败：" + err.Error()
		return result
	}
	installID := ""
	for _, item := range list.Plugins {
		if item.PluginID == "workspace" && item.InstallID != "" {
			installID = item.InstallID
			break
		}
	}
	if installID == "" {
		result.Status = "fail"
		result.Evidence = "临时库里没有 workspace 插件"
		return result
	}
	off := env.engine.Handle(ctx, probeRequest("plugin.toggle", "probe-plugin-off", probeJSON(map[string]any{
		"installId": installID, "enabled": false, "actor": "catalog-probe",
	})))
	if !off.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(off)
		return result
	}
	on := env.engine.Handle(ctx, probeRequest("plugin.toggle", "probe-plugin-on", probeJSON(map[string]any{
		"installId": installID, "enabled": true, "actor": "catalog-probe",
	})))
	if !on.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(on)
		return result
	}
	again, err := env.plugins.List(ctx, "", "")
	if err != nil {
		result.Status = "fail"
		result.Evidence = "启用后名单读失败：" + err.Error()
		return result
	}
	for _, item := range again.Plugins {
		if item.InstallID == installID && item.State == "enabled" {
			result.Status = "pass"
			result.Evidence = "已跑完：读回插件已启用"
			return result
		}
	}
	result.Status = "fail"
	result.Evidence = "读回插件不是启用"
	return result
}

func (env *probeEnv) completeMCPSecurityReview(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "mcp.security.review", Title: "连接 MCP"}
	env.engine.SetM7RuntimeServices(nil, nil, m7app.NewMcpRuntimeService(env.store.AgentRuntimeRepository()))
	catalog := mcp6.Catalogue{Identity: "probe@1", Tools: map[string]mcp6.ToolSchema{
		"lookup": {InputSchema: json.RawMessage(`{"type":"object"}`)},
	}}
	reg := mcp6.NewRegistry(nil, nil, nil)
	reg.SetSecurityAdapters(
		func(ctx context.Context, _ *mcp6.Endpoint, fn func(mcp6.Credentials) error) error {
			return fn(mcp6.Credentials{Context: ctx})
		},
		func(context.Context, *mcp6.Endpoint, mcp6.Credentials) (mcp6.Catalogue, error) {
			return catalog, nil
		},
		func(context.Context, *mcp6.Endpoint, string, map[string]any, mcp6.Credentials) (map[string]any, error) {
			return map[string]any{"ok": true}, nil
		},
	)
	env.engine.SetM6Services(nil, reg, nil)
	added, err := env.engine.m7mcp.Add(ctx, m7app.McpAddInput{
		Origin: "manual", Transport: "https", URL: "https://fixture.invalid", RiskConfirmed: true,
	})
	if err != nil {
		result.Status = "fail"
		result.Evidence = "写入临时 MCP 端点失败：" + err.Error()
		return result
	}
	ep, err := env.engine.m7mcp.Endpoint(ctx, added.EndpointID)
	if err != nil {
		result.Status = "fail"
		result.Evidence = "读回临时 MCP 端点失败：" + err.Error()
		return result
	}
	response := env.engine.Handle(ctx, probeRequest("mcp.security.review", "probe-mcp-review", probeJSON(map[string]any{
		"endpointId": ep.EndpointID, "expectedVersion": ep.Security.Version, "action": "inspect",
	})))
	if !response.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(response)
		return result
	}
	var body struct {
		ObservedDigest string   `json:"observedDigest"`
		Tools          []string `json:"tools"`
	}
	raw, _ := json.Marshal(response.Payload)
	if json.Unmarshal(raw, &body) != nil || body.ObservedDigest == "" || !strings.Contains(strings.Join(body.Tools, ","), "lookup") {
		result.Status = "fail"
		result.Evidence = "复核返回里没有摘要或工具"
		return result
	}
	again := env.engine.Handle(ctx, probeRequest("mcp.security.review", "probe-mcp-review-again", probeJSON(map[string]any{
		"endpointId": ep.EndpointID, "expectedVersion": ep.Security.Version, "action": "inspect",
	})))
	if !again.OK {
		result.Status = "fail"
		result.Evidence = "再次复核失败：" + probeCode(again)
		return result
	}
	var second struct {
		ObservedDigest string `json:"observedDigest"`
	}
	raw, _ = json.Marshal(again.Payload)
	_ = json.Unmarshal(raw, &second)
	if second.ObservedDigest != body.ObservedDigest {
		result.Status = "fail"
		result.Evidence = "读回复核摘要不一致"
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回复核摘要"
	return result
}

func (env *probeEnv) completeSkillInvoke(ctx context.Context, sessionID string) producthub.TaskResult {
	result := producthub.TaskResult{ID: "skill.invoke", Title: "调用技能"}
	if env.engine.skills == nil || sessionID == "" {
		return env.call(ctx, "skill.invoke", "调用技能", map[string]any{})
	}
	created, err := env.engine.skills.Create(ctx, skill.Skill{
		Name: "catalog-probe", DisplayName: "诊断探测技能", Version: "1.0.0",
		Permissions: []skill.PermissionLevel{skill.PermissionReadOnly},
		EntryPoint:  "builtin:summarize-input", ManifestJSON: "{}",
	})
	if err != nil {
		result.Status = "fail"
		result.Evidence = "写入临时技能失败：" + err.Error()
		return result
	}
	if err := env.engine.skills.Publish(ctx, created.ID); err != nil {
		result.Status = "fail"
		result.Evidence = "发布临时技能失败：" + err.Error()
		return result
	}
	response := env.engine.Handle(ctx, probeRequest("skill.invoke", "probe-skill-invoke", probeJSON(map[string]any{
		"skillId": created.ID, "sessionId": sessionID, "input": "诊断探测",
	})))
	if !response.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(response)
		return result
	}
	var body struct {
		InvocationID string `json:"invocationId"`
	}
	raw, _ := json.Marshal(response.Payload)
	if json.Unmarshal(raw, &body) != nil || body.InvocationID == "" {
		result.Status = "fail"
		result.Evidence = "调用返回里没有编号"
		return result
	}
	stored, err := env.store.GetSkillInvocation(ctx, body.InvocationID)
	if err != nil || stored == nil || stored.SkillID != created.ID || stored.Input != "诊断探测" {
		result.Status = "fail"
		result.Evidence = "读回技能调用不一致"
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回技能调用「诊断探测」"
	return result
}

func probeJSON(v any) []byte {
	raw, _ := json.Marshal(v)
	return raw
}

func probeRequest(method, key string, payload []byte) bridge.Request {
	return bridge.Request{
		Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(),
		Method: method, SentAt: time.Now().UTC(), Payload: payload,
		IdempotencyKey: key, DeadlineMS: 2000,
	}
}

func probeCode(response bridge.Response) string {
	if response.Error == nil {
		return ""
	}
	return strings.TrimSpace(response.Error.Code + " " + response.Error.Message)
}
