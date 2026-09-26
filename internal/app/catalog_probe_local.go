package app

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/lunitide/lunitide/internal/agenthub"
	"github.com/lunitide/lunitide/internal/brapp"
	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/identity"
	"github.com/lunitide/lunitide/internal/meetings"
	"github.com/lunitide/lunitide/internal/people"
	"github.com/lunitide/lunitide/internal/producthub"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/oklog/ulid/v2"
)

// probeBrHost records navigation without opening a browser window.
type probeBrHost struct {
	navigates []string
}

func (h *probeBrHost) Detect(context.Context, brapp.Settings) (brapp.DetectReport, error) {
	return brapp.DetectReport{Builtin: true}, nil
}

func (h *probeBrHost) Connect(context.Context, string, string, brapp.Settings) (string, error) {
	return "ws://127.0.0.1:9/devtools/browser/probe", nil
}

func (h *probeBrHost) Disconnect(context.Context, string, string) error { return nil }

func (h *probeBrHost) Navigate(_ context.Context, _ brapp.Session, rawURL string) error {
	h.navigates = append(h.navigates, rawURL)
	return nil
}

func (h *probeBrHost) SnapshotUsage(context.Context, string) (int64, int64, int64) { return 0, 0, 0 }

func (h *probeBrHost) ClearData(context.Context, string, time.Time) (int64, error) { return 0, nil }

func (env *probeEnv) completeBrowserNavigate(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "br.navigate", Title: "浏览器打开网址"}
	host := &probeBrHost{}
	svc := brapp.New(env.store.AgentRuntimeRepository(), env.dir)
	svc.SetHost(host)
	svc.SetResolver(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	})
	env.engine.SetBrMultiModeService(svc)
	const sessionID = "br-probe-1"
	const rawURL = "https://example.com/docs"
	connected := env.engine.Handle(ctx, probeRequest("br.session.connect", "probe-br-connect", probeJSON(map[string]any{
		"sessionId": sessionID, "mode": brapp.ModeBuiltin, "actor": "catalog-probe",
	})))
	if !connected.OK {
		result.Status = "fail"
		result.Evidence = "临时浏览器没有连上：" + probeCode(connected)
		return result
	}
	response := env.engine.Handle(ctx, probeRequest("br.navigate", "probe-br-navigate", probeJSON(map[string]any{
		"sessionId": sessionID, "url": rawURL, "actor": "catalog-probe",
	})))
	if !response.OK {
		result.Status = "fail"
		result.Evidence = "导航没有完成：" + probeCode(response)
		return result
	}
	var body struct {
		URL string `json:"url"`
	}
	raw, _ := json.Marshal(response.Payload)
	_ = json.Unmarshal(raw, &body)
	seen := false
	for _, got := range host.navigates {
		if got == rawURL {
			seen = true
			break
		}
	}
	if body.URL != rawURL || !seen {
		result.Status = "fail"
		result.Evidence = "读回网址不一致"
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回网址「" + rawURL + "」，没有打开本机浏览器"
	return result
}

func (env *probeEnv) ensurePeople(ctx context.Context) error {
	if env.people != nil {
		return nil
	}
	ident := identity.New(env.store)
	if err := ident.Ensure(ctx); err != nil {
		return err
	}
	receive := filepath.Join(env.dir, "people-receive")
	staging := filepath.Join(env.dir, "people-staging")
	if err := os.MkdirAll(receive, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return err
	}
	roster := people.New(env.store, ident, receive, staging)
	env.engine.SetIdentityPeopleServices(ident, roster)
	env.people = roster
	return nil
}

func (env *probeEnv) completePeopleFileOpen(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "people.file.open", Title: "打开同事发来的文件"}
	if err := env.ensurePeople(ctx); err != nil {
		result.Status = "fail"
		result.Evidence = "同事目录没有建成：" + err.Error()
		return result
	}
	const text = "诊断探测文件"
	path := filepath.Join(env.dir, "people-receive", "probe-note.txt")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		result.Status = "fail"
		result.Evidence = "临时文件没有写成：" + err.Error()
		return result
	}
	var opened string
	restore := people.ReplaceOpenPathForTest(func(p string) error {
		opened = p
		return nil
	})
	defer restore()
	response := env.engine.Handle(ctx, probeRequest("people.file.open", "probe-people-file", probeJSON(map[string]any{
		"destPath": path, "fileName": "probe-note.txt",
	})))
	if !response.OK {
		result.Status = "fail"
		result.Evidence = "打开没有完成：" + probeCode(response)
		return result
	}
	var body struct {
		Opened string `json:"opened"`
	}
	raw, _ := json.Marshal(response.Payload)
	_ = json.Unmarshal(raw, &body)
	got, err := os.ReadFile(body.Opened)
	if err != nil || body.Opened == "" || body.Opened != opened || string(got) != text {
		result.Status = "fail"
		result.Evidence = "读回同事文件不一致"
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回同事文件「" + text + "」，没有用系统打开"
	return result
}

func (env *probeEnv) completePeopleThreadSend(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "people.thread.send", Title: "给同事发文件"}
	if err := env.ensurePeople(ctx); err != nil {
		result.Status = "fail"
		result.Evidence = "同事目录没有建成：" + err.Error()
		return result
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	peerID := ulid.Make().String()
	if err := env.store.UpsertContact(ctx, people.Contact{
		SubjectID: peerID, Nickname: "诊断同事", Status: "online", OrgName: "诊断",
		TrustState: "trusted", CreatedAt: now, UpdatedAt: now, LastSeenAt: now,
	}); err != nil {
		result.Status = "fail"
		result.Evidence = "临时同事没有写入：" + err.Error()
		return result
	}
	thread, _, err := env.people.OpenDirect(ctx, peerID)
	if err != nil {
		result.Status = "fail"
		result.Evidence = "临时会话没有打开：" + err.Error()
		return result
	}
	const text = "诊断探测"
	response := env.engine.Handle(ctx, probeRequest("people.thread.send", "probe-people-send", probeJSON(map[string]any{
		"threadId": thread.ThreadID, "kind": "text", "body": text,
	})))
	if !response.OK {
		result.Status = "fail"
		result.Evidence = "发送没有完成：" + probeCode(response)
		return result
	}
	msgs, err := env.people.ListMessages(ctx, thread.ThreadID, 20)
	if err != nil {
		result.Status = "fail"
		result.Evidence = "读回消息失败：" + err.Error()
		return result
	}
	for _, msg := range msgs {
		if msg.Kind == "text" && msg.Body == text {
			result.Status = "pass"
			result.Evidence = "已跑完：读回同事消息「" + text + "」，没有发给真实同事"
			return result
		}
	}
	result.Status = "fail"
	result.Evidence = "读回同事消息不一致"
	return result
}

func (env *probeEnv) completePeopleFilePick(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "people.file.pick", Title: "选择本地文件"}
	if err := env.ensurePeople(ctx); err != nil {
		result.Status = "fail"
		result.Evidence = "同事目录没有建成：" + err.Error()
		return result
	}
	const text = "诊断探测所选"
	path := filepath.Join(env.dir, "picked-probe.txt")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		result.Status = "fail"
		result.Evidence = "临时文件没有写成：" + err.Error()
		return result
	}
	restore := people.ReplacePickPathForTest(func(bool) (string, error) { return path, nil })
	defer restore()
	response := env.engine.Handle(ctx, probeRequest("people.file.pick", "probe-people-pick", probeJSON(map[string]any{"folder": false})))
	if !response.OK {
		result.Status = "fail"
		result.Evidence = "选择没有完成：" + probeCode(response)
		return result
	}
	var body struct {
		Path     string `json:"path"`
		FileName string `json:"fileName"`
		Size     int64  `json:"size"`
	}
	raw, _ := json.Marshal(response.Payload)
	_ = json.Unmarshal(raw, &body)
	got, err := os.ReadFile(body.Path)
	if err != nil || body.Path != path || body.FileName != "picked-probe.txt" || body.Size != int64(len(text)) || string(got) != text {
		result.Status = "fail"
		result.Evidence = "读回所选文件不一致"
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回所选文件「" + text + "」，没有弹出选择框"
	return result
}

func (env *probeEnv) completeMeetingStart(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "meetings.start", Title: "开始会议听写"}
	if env.engine.meetings == nil {
		result.Status = "fail"
		result.Evidence = "临时会议服务没有装上"
		return result
	}
	restore := meetings.RefusePlatformLoopbackForTest()
	defer restore()
	const title = "诊断探测会议"
	response := env.engine.Handle(ctx, probeRequest("meetings.start", "probe-meeting-start", probeJSON(map[string]any{"title": title})))
	if !response.OK {
		result.Status = "fail"
		result.Evidence = "会议没有开始：" + probeCode(response)
		return result
	}
	var started struct {
		MeetingID string `json:"meetingId"`
		Title     string `json:"title"`
		Status    string `json:"status"`
	}
	raw, _ := json.Marshal(response.Payload)
	_ = json.Unmarshal(raw, &started)
	got := env.engine.Handle(ctx, probeRequest("meetings.get", "probe-meeting-get", probeJSON(map[string]any{"meetingId": started.MeetingID})))
	var detail struct {
		MeetingID string `json:"meetingId"`
		Title     string `json:"title"`
		Status    string `json:"status"`
	}
	raw, _ = json.Marshal(got.Payload)
	_ = json.Unmarshal(raw, &detail)
	if !got.OK || detail.MeetingID != started.MeetingID || detail.Title != title || detail.Status != string(meetings.StatusRecording) {
		result.Status = "fail"
		result.Evidence = "读回会议不一致"
		_, _ = env.engine.meetings.Stop(ctx, started.MeetingID)
		return result
	}
	if _, err := env.engine.meetings.Stop(ctx, started.MeetingID); err != nil {
		result.Status = "fail"
		result.Evidence = "会议没有停下：" + err.Error()
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回会议「" + title + "」，没有打开麦克风"
	return result
}

func (env *probeEnv) completeAgentHubTask(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "agentHub.task.start", Title: "Agent Hub 开工"}
	restore := agenthub.SilenceExternalProbesForTest()
	defer restore()
	hub := agenthub.New(env.store, env.dir, nil)
	hub.Look = func(name string) (string, error) {
		if name == "codex" {
			return filepath.Join(env.dir, "probe-codex"), nil
		}
		return "", os.ErrNotExist
	}
	hub.Version = func(string, time.Duration) (string, error) { return "probe", nil }
	hub.Start = func(context.Context, agenthub.ProcSpec, func(string)) (int64, bool, error) {
		return 0, false, nil
	}
	env.engine.SetAgentHub(hub)
	const prompt = "诊断探测任务"
	response := env.engine.Handle(ctx, probeRequest("agentHub.task.start", "probe-agent-task", probeJSON(map[string]any{
		"agent": "codex", "prompt": prompt, "idempotencyKey": "probe-agent-task", "workDir": env.dir,
	})))
	if !response.OK {
		result.Status = "fail"
		result.Evidence = "任务没有开始：" + probeCode(response)
		return result
	}
	var started struct {
		Task struct {
			ID     string `json:"taskId"`
			Prompt string `json:"prompt"`
		} `json:"task"`
	}
	raw, _ := json.Marshal(response.Payload)
	_ = json.Unmarshal(raw, &started)
	if started.Task.ID == "" {
		result.Status = "fail"
		result.Evidence = "开始返回里没有任务编号"
		return result
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := env.engine.Handle(ctx, probeRequest("agentHub.task.get", "probe-agent-get", probeJSON(map[string]any{"taskId": started.Task.ID})))
		var detail struct {
			Task struct {
				ID      string `json:"taskId"`
				Prompt  string `json:"prompt"`
				Status  string `json:"status"`
				WorkDir string `json:"workDir"`
			} `json:"task"`
		}
		raw, _ = json.Marshal(got.Payload)
		_ = json.Unmarshal(raw, &detail)
		if got.OK && detail.Task.ID == started.Task.ID && detail.Task.Prompt == prompt && detail.Task.Status == "success" && detail.Task.WorkDir != "" {
			env.agentTaskID = detail.Task.ID
			env.agentWorkDir = detail.Task.WorkDir
			result.Status = "pass"
			result.Evidence = "已跑完：读回任务「" + prompt + "」，没有拉起真实进程"
			return result
		}
		if time.Now().After(deadline) {
			result.Status = "fail"
			result.Evidence = "读回任务不一致"
			return result
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (env *probeEnv) completeAgentHubFileOpen(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "agentHub.file.open", Title: "Agent Hub 打开文件"}
	if env.agentTaskID == "" || env.agentWorkDir == "" {
		result.Status = "fail"
		result.Evidence = "没有可打开的临时任务"
		return result
	}
	const text = "诊断探测产物"
	path := filepath.Join(env.agentWorkDir, "probe-artifact.txt")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		result.Status = "fail"
		result.Evidence = "临时产物没有写成：" + err.Error()
		return result
	}
	var opened string
	prev := openArtifactTarget
	openArtifactTarget = func(target string, _, _ bool) error {
		opened = target
		return nil
	}
	defer func() { openArtifactTarget = prev }()
	response := env.engine.Handle(ctx, probeRequest("agentHub.file.open", "probe-agent-open", probeJSON(map[string]any{
		"taskId": env.agentTaskID, "path": "probe-artifact.txt",
	})))
	if !response.OK {
		result.Status = "fail"
		result.Evidence = "打开没有完成：" + probeCode(response)
		return result
	}
	got, err := os.ReadFile(opened)
	if err != nil || opened != path || string(got) != text {
		result.Status = "fail"
		result.Evidence = "读回产物不一致"
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回产物「" + text + "」，没有用系统打开"
	return result
}

// probeCcHost records computer-control calls without touching this machine.
type probeCcHost struct {
	clip string
}

func (h *probeCcHost) Available() bool                                     { return true }
func (h *probeCcHost) ScreenSize() (int, int)                               { return 800, 600 }
func (h *probeCcHost) ScreenOrigin() (int, int)                             { return 0, 0 }
func (h *probeCcHost) CursorPosition() (int, int, error)                    { return 0, 0, nil }
func (h *probeCcHost) MouseMove(int, int) error                             { return nil }
func (h *probeCcHost) MouseClick(string, int) error                         { return nil }
func (h *probeCcHost) MouseDrag(int, int, int, int) error                   { return nil }
func (h *probeCcHost) KeyboardType(string) error                            { return nil }
func (h *probeCcHost) KeyboardShortcut([]string) error                      { return nil }
func (h *probeCcHost) HoldKey(string, bool) error                           { return nil }
func (h *probeCcHost) MouseScroll(int) error                                { return nil }
func (h *probeCcHost) MouseScrollH(int) error                               { return nil }
func (h *probeCcHost) EnsureForeground() error                              { return nil }
func (h *probeCcHost) ScreenCapture() ([]byte, error)                       { return nil, nil }
func (h *probeCcHost) WindowCapture(string) ([]byte, int, int, error)       { return nil, 0, 0, nil }
func (h *probeCcHost) ActiveWindow() (string, string, error)                { return "probe", "probe", nil }
func (h *probeCcHost) ListWindows() ([]ccapp.WindowInfo, error)             { return nil, nil }
func (h *probeCcHost) FocusWindow(string) (ccapp.WindowInfo, error)         { return ccapp.WindowInfo{}, nil }
func (h *probeCcHost) ObserveDialogs() ([]ccapp.DialogSnapshot, error)      { return nil, nil }
func (h *probeCcHost) ConfirmDialog(string) (ccapp.DialogSnapshot, error)   { return ccapp.DialogSnapshot{}, nil }
func (h *probeCcHost) ObserveUI(int) ([]ccapp.UINode, error)                 { return nil, nil }
func (h *probeCcHost) ClipboardGet() (string, error)                        { return h.clip, nil }
func (h *probeCcHost) ClipboardSet(text string) error                       { h.clip = text; return nil }
func (h *probeCcHost) WindowAction(string, string, int, int, int, int) (ccapp.WindowInfo, error) {
	return ccapp.WindowInfo{}, nil
}
func (h *probeCcHost) QuitApp(string) (int, ccapp.WindowInfo, error) { return 0, ccapp.WindowInfo{}, nil }
func (h *probeCcHost) MenuClick(string) error                        { return nil }
func (h *probeCcHost) SetValue(string, string) error                 { return nil }
func (h *probeCcHost) InvokeUI(string) error                         { return nil }

func (env *probeEnv) completeComputerControl(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "computer.control", Title: "电脑控制"}
	host := &probeCcHost{}
	svc := ccapp.New(env.store.AgentRuntimeRepository())
	svc.SetHost(host)
	env.engine.SetCcControlService(svc)
	cfg, err := svc.GetConfig(ctx)
	if err != nil {
		result.Status = "fail"
		result.Evidence = "电脑控制配置没有读到：" + err.Error()
		return result
	}
	enabled := true
	if _, err = svc.UpdateConfig(ctx, ccapp.SettingsPatch{ExpectedRevision: cfg.Revision, Enabled: &enabled, Actor: "catalog-probe"}); err != nil {
		result.Status = "fail"
		result.Evidence = "电脑控制没有打开：" + err.Error()
		return result
	}
	const text = "诊断探测控制"
	out, err := svc.ExecuteTool(ctx, "cc-probe", ccapp.ToolClipboard, []byte(`{"op":"set","text":"`+text+`"}`), true)
	if err != nil {
		result.Status = "fail"
		result.Evidence = "电脑控制没有做完：" + err.Error()
		return result
	}
	entries, err := svc.GetAuditLog(ctx, 20, "", "cc-probe")
	if err != nil {
		result.Status = "fail"
		result.Evidence = "读回电脑控制失败：" + err.Error()
		return result
	}
	executed := false
	for _, entry := range entries {
		if entry.Tool == ccapp.ToolClipboard && entry.Status == ccapp.StatusExecuted {
			executed = true
			break
		}
	}
	if host.clip != text || !executed || out.Summary == "" {
		result.Status = "fail"
		result.Evidence = "读回电脑控制不一致"
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回临时剪贴板「" + text + "」，没有操控本机键鼠"
	return result
}

type probeLease struct{}

func (probeLease) WithLease(_ context.Context, _ secretlease.Request, fn func([]byte) error) error {
	return fn([]byte("probe-key"))
}

func (env *probeEnv) completeProviderTest(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "provider.test", Title: "测试供应商连通"}
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	base := server.URL + "/v1"
	origin, err := provider.NormalizeOrigin(base)
	if err != nil {
		result.Status = "fail"
		result.Evidence = "临时供应商地址无效：" + err.Error()
		return result
	}
	providerID := ulid.Make().String()
	credentialRef := ulid.Make().String()
	isDefault := true
	raw := probeJSON(map[string]any{
		"providerId": providerID, "credentialRef": credentialRef, "origin": origin, "protocol": string(provider.ProtocolOpenAICompatible),
		"create": map[string]any{
			"name": "诊断探测供应商连通", "protocol": string(provider.ProtocolOpenAICompatible), "baseUrl": base,
			"models": []map[string]any{{"modelId": "probe-m", "displayName": "Probe", "isDefault": isDefault}},
		},
	})
	created := handleProviderCreateWithCredential(env.engine, ctx, probeRequest("provider.create", "probe-provider-test-create", raw))
	if !created.OK {
		result.Status = "fail"
		result.Evidence = "临时供应商没有建成：" + probeCode(created)
		return result
	}
	env.engine.leases = probeLease{}
	response := env.engine.Handle(ctx, probeRequest("provider.test", "probe-provider-test", probeJSON(map[string]any{
		"providerId": providerID, "modelId": "probe-m",
	})))
	if !response.OK {
		result.Status = "fail"
		result.Evidence = "连通测试没有完成：" + probeCode(response)
		return result
	}
	var body struct {
		Status string `json:"status"`
	}
	encoded, _ := json.Marshal(response.Payload)
	_ = json.Unmarshal(encoded, &body)
	if body.Status != "passed" || hits < 1 {
		result.Status = "fail"
		result.Evidence = "读回供应商连通不一致"
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回本机测试服务状态 passed，没有连接真实供应商"
	return result
}
