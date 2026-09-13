package agenthub

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

var (
	ErrNotAvailable = errors.New("AGENT_NOT_AVAILABLE")
	ErrNotFound     = errors.New("not found")
	ErrPathOutside  = errors.New("path outside work dir")
	ErrPickCanceled = errors.New("pick canceled")
)

type StartFunc func(ctx context.Context, spec ProcSpec, onLine func(string)) (exit int64, timedOut bool, err error)

type Service struct {
	Store      TaskStore
	Threads    *ThreadStore
	Root       string
	Look       LookPath
	Version    VersionRunner
	Start      StartFunc
	Notify     func(title, body string) error
	Now        func() time.Time
	Pick       func() (string, error)
	PickFiles  func() ([]string, error)
	PickFolder func() (string, error)

	mu       sync.Mutex
	cancels  map[string]context.CancelFunc
	running  map[string]string
	timeouts map[string]time.Duration
}

func New(store TaskStore, root string, notify func(title, body string) error) *Service {
	return &Service{
		Store:    store,
		Root:     filepath.Clean(root),
		Look:     lookWithCommonPaths,
		Start:    StartProcess,
		Notify:   notify,
		Now:      time.Now,
		cancels:  map[string]context.CancelFunc{},
		running:  map[string]string{},
		timeouts: map[string]time.Duration{},
	}
}

func (s *Service) Recover() {
	if s.Store != nil {
		items, _, err := s.Store.ListTasks(ListFilter{})
		if err == nil {
			queued := map[string]bool{}
			now := s.now().Format(time.RFC3339)
			for _, task := range items {
				if task.Status == "running" {
					task.Status = "failed"
					task.ErrorMsg = "应用重启后未能继续该任务"
					task.FinishedAt = now
					_ = s.Store.UpdateTask(task)
					for _, art := range ScanWorkDir(task.WorkDir, nil, parseRFC3339(task.StartedAt)) {
						art.Source = persistableSource(art.Source)
						_ = s.Store.UpsertArtifact(task.ID, art)
					}
				}
				if task.Status == "queued" {
					queued[task.Agent] = true
				}
			}
			for agent := range queued {
				s.kickAgentQueue(agent)
			}
		}
	}
	s.recoverLiveThreads()
}

func (s *Service) recoverLiveThreads() {
	if s.Threads == nil {
		return
	}
	items, err := s.Threads.List(ThreadFilter{})
	if err != nil {
		return
	}
	for _, thread := range items {
		if thread.Status != "running" && thread.Status != "waiting_user" {
			continue
		}
		if adapter, err := s.threadAdapter(thread.HarnessID); err == nil {
			_ = adapter.Close(thread.ID)
		}
		_ = s.Threads.CancelOpenPrompts(thread.ID)
		_ = setThreadStatus(s.Threads, thread.ID, "faulted")
		_ = insertThreadMessage(s.Threads, thread.ID, "notice", "应用重启后未能继续")
		_ = insertThreadEvent(s.Threads, thread.ID, AgentEvent{
			Type: "error", Title: "应用重启后未能继续", Detail: "应用重启后未能继续",
		})
	}
}

func (s *Service) Detect() []AgentStatus {
	return DetectAll(s.Look, s.Version)
}

func (s *Service) statusOf(name string) AgentStatus {
	for _, item := range s.Detect() {
		if item.Name == name {
			return item
		}
	}
	return AgentStatus{Name: name, State: "unknown", Hint: "未知适配器"}
}

func (s *Service) StartTask(req TaskRequest) (TaskDetail, error) {
	req.Agent = normalizeAgent(req.Agent)
	if req.Prompt == "" || req.IdempotencyKey == "" {
		return TaskDetail{}, fmt.Errorf("参数无效")
	}
	if existing, err := s.Store.GetTaskByKey(req.IdempotencyKey); err == nil {
		return s.GetTask(existing.ID)
	}
	st := s.statusOf(req.Agent)
	if st.State != "available" || !st.NonInteractive {
		return TaskDetail{}, fmt.Errorf("%w: %s", ErrNotAvailable, st.Hint)
	}
	adapter, err := Adapter(req.Agent)
	if err != nil {
		return TaskDetail{}, err
	}
	workDir, err := s.resolveWorkDir(req.WorkDir)
	if err != nil {
		return TaskDetail{}, err
	}
	req.WorkDir = workDir
	id := req.TaskID
	if id == "" {
		id = ulid.Make().String()
	}
	now := s.now().Format(time.RFC3339)
	task := TaskRecord{
		ID: id, Agent: req.Agent, Prompt: req.Prompt, WorkDir: workDir, Sandbox: req.Sandbox,
		Status: "queued", CreatedAt: now, IdempotencyKey: req.IdempotencyKey,
	}
	if err = s.Store.InsertTask(task); err != nil {
		if existing, getErr := s.Store.GetTaskByKey(req.IdempotencyKey); getErr == nil {
			return s.GetTask(existing.ID)
		}
		return TaskDetail{}, err
	}
	timeout := clampTimeout(time.Duration(req.TimeoutMin) * time.Minute)
	s.mu.Lock()
	s.timeouts[id] = timeout
	s.mu.Unlock()
	s.maybeRun(adapter, req, task, timeout)
	return s.GetTask(id)
}

func (s *Service) maybeRun(adapter AgentAdapter, req TaskRequest, task TaskRecord, timeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.running[task.Agent]; ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancels[task.ID] = cancel
	s.running[task.Agent] = task.ID
	go s.execute(ctx, adapter, req, task, timeout)
}

func (s *Service) execute(ctx context.Context, adapter AgentAdapter, req TaskRequest, task TaskRecord, timeout time.Duration) {
	defer s.finishQueue(task.Agent, task.ID)
	task.Status = "running"
	task.StartedAt = s.now().Format(time.RFC3339)
	_ = s.Store.UpdateTask(task)
	_ = s.appendEvent(task.ID, AgentEvent{Type: "started", Title: "开始", Detail: req.Agent, TS: task.StartedAt})
	exe, args, stdin, err := adapter.BuildCommand(req)
	if err != nil {
		s.fail(task, 0, false, err)
		return
	}
	if req.Agent == "kimi" {
		hasSkillsDir := false
		for _, arg := range args {
			if arg == "--skills-dir" {
				hasSkillsDir = true
				break
			}
		}
		if !hasSkillsDir {
			_ = s.appendEvent(task.ID, AgentEvent{Type: "message", Title: "未找到 kimi-slides 技能目录", TS: s.now().Format(time.RFC3339)})
		}
	}
	look := s.Look
	if look == nil {
		look = defaultLookPath
	}
	if looked, lookErr := look(exe); lookErr == nil && looked != "" {
		exe = looked
	}
	start := s.Start
	if start == nil {
		start = StartProcess
	}
	var eventPaths []string
	exit, timedOut, runErr := start(ctx, ProcSpec{Exe: exe, Dir: req.WorkDir, Args: args, Stdin: stdin, Timeout: timeout}, func(line string) {
		ev, ok := adapter.ParseLine(line)
		if !ok {
			return
		}
		ev.TS = s.now().Format(time.RFC3339)
		if ev.Path != "" {
			eventPaths = append(eventPaths, ev.Path)
		}
		if ev.Tokens > 0 {
			task.TokensUsed += ev.Tokens
			_ = s.Store.UpdateTask(task)
		}
		_ = s.appendEvent(task.ID, ev)
	})
	latest, _ := s.Store.GetTask(task.ID)
	if latest.ID != "" {
		task = latest
	}
	for _, art := range ScanWorkDir(req.WorkDir, eventPaths, parseRFC3339(task.StartedAt)) {
		art.Source = persistableSource(art.Source)
		_ = s.Store.UpsertArtifact(task.ID, art)
	}
	if errors.Is(runErr, context.Canceled) {
		task.Status = "cancelled"
		task.ErrorMsg = "已取消"
	} else if timedOut {
		task.Status = "timeout"
		task.ErrorMsg = "任务超时"
		task.ExitCode = int64Ptr(exit)
	} else if runErr != nil || exit != 0 {
		task.Status = "failed"
		events, _ := s.Store.ListEvents(task.ID)
		text := eventText(runErr, events)
		if looksRateLimited(runErr, events) {
			task.ErrorMsg = "订阅限流，请稍后重试"
		} else if looksLoggedOut(text) {
			task.ErrorMsg = "已安装但未登录。请先在该 CLI 自己的终端完成登录。"
		} else if looksUnsupportedModel(text) {
			task.ErrorMsg = "当前 Codex CLI 不支持配置里的模型。请升级 CLI，或改用该 CLI 可用的模型。"
		} else if strings.Contains(text, "Not inside a trusted directory") {
			task.ErrorMsg = "工作目录未被 Codex 信任。请换一个目录后再试。"
		} else if runErr != nil {
			task.ErrorMsg = chineseProcError(runErr)
		} else {
			task.ErrorMsg = fmt.Sprintf("退出码 %d", exit)
		}
		task.ExitCode = int64Ptr(exit)
	} else {
		task.Status = "success"
		task.ExitCode = int64Ptr(exit)
	}
	task.FinishedAt = s.now().Format(time.RFC3339)
	_ = s.Store.UpdateTask(task)
	_ = s.appendEvent(task.ID, AgentEvent{Type: "finished", Title: "结束", Detail: task.Status, TS: task.FinishedAt})
	s.notify(task)
}

func (s *Service) fail(task TaskRecord, exit int64, timedOut bool, err error) {
	task.Status = "failed"
	if timedOut {
		task.Status = "timeout"
	}
	task.ErrorMsg = chineseProcError(err)
	task.ExitCode = int64Ptr(exit)
	task.FinishedAt = s.now().Format(time.RFC3339)
	_ = s.Store.UpdateTask(task)
	s.notify(task)
}

func (s *Service) finishQueue(agent, taskID string) {
	s.mu.Lock()
	delete(s.cancels, taskID)
	delete(s.timeouts, taskID)
	if s.running[agent] == taskID {
		delete(s.running, agent)
	}
	s.mu.Unlock()
	s.kickAgentQueue(agent)
}

func (s *Service) kickAgentQueue(agent string) {
	items, _, err := s.Store.ListTasks(ListFilter{Agent: agent, Status: "queued"})
	if err != nil || len(items) == 0 {
		return
	}
	next := items[0]
	for _, item := range items {
		if item.CreatedAt < next.CreatedAt {
			next = item
		}
	}
	adapter, err := Adapter(agent)
	if err != nil {
		return
	}
	s.mu.Lock()
	timeout := 30 * time.Minute
	if d, ok := s.timeouts[next.ID]; ok {
		timeout = d
	}
	s.mu.Unlock()
	s.maybeRun(adapter, TaskRequest{Agent: next.Agent, Prompt: next.Prompt, WorkDir: next.WorkDir, Sandbox: next.Sandbox, TimeoutMin: int(timeout / time.Minute)}, next, timeout)
}

func (s *Service) Cancel(id string) (TaskDetail, error) {
	s.mu.Lock()
	cancel := s.cancels[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	task, err := s.Store.GetTask(id)
	if err != nil {
		return TaskDetail{}, ErrNotFound
	}
	if task.Status == "queued" {
		task.Status = "cancelled"
		task.ErrorMsg = "已取消"
		task.FinishedAt = s.now().Format(time.RFC3339)
		_ = s.Store.UpdateTask(task)
	}
	return s.GetTask(id)
}

func (s *Service) GetTask(id string) (TaskDetail, error) {
	task, err := s.Store.GetTask(id)
	if err != nil {
		return TaskDetail{}, ErrNotFound
	}
	events, _ := s.Store.ListEvents(id)
	arts, _ := s.Store.ListArtifacts(id)
	if events == nil {
		events = []AgentEvent{}
	}
	if arts == nil {
		arts = []Artifact{}
	}
	if task.Status == "running" || task.Status == "queued" {
		arts = mergeLiveArtifacts(arts, peekWorkDir(task.WorkDir))
	}
	arts = decorateArtifacts(task.WorkDir, task.StartedAt, arts)
	return TaskDetail{Task: task, Events: events, Artifacts: arts}, nil
}

func (s *Service) List(filter ListFilter) (TaskList, error) {
	items, counts, err := s.Store.ListTasks(filter)
	if err != nil {
		return TaskList{}, err
	}
	if items == nil {
		items = []TaskRecord{}
	}
	return TaskList{Items: items, Counts: counts}, nil
}

func (s *Service) Artifacts(filter ListFilter) ([]Artifact, error) {
	items, err := s.Store.ListAllArtifacts(filter)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return []Artifact{}, nil
	}
	for i := range items {
		items[i].Source = rematerializeSource("", items[i].Path, items[i].Source, time.Time{})
	}
	return items, nil
}

func (s *Service) ResolveFile(taskID, rel string) (string, error) {
	return s.safePath(taskID, rel)
}

func (s *Service) ChooseDir() (string, error) {
	pick := s.Pick
	if pick == nil {
		pick = pickWorkDirOS
	}
	path, err := pick()
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "" || !filepath.IsAbs(clean) || forbiddenWorkDir(clean) {
		return "", fmt.Errorf("工作目录不受支持")
	}
	info, statErr := os.Stat(clean)
	if statErr != nil || !info.IsDir() {
		return "", fmt.Errorf("工作目录不受支持")
	}
	return clean, nil
}

func (s *Service) OpenPath(taskID, rel string) (string, error) {
	if rel == "" {
		task, err := s.Store.GetTask(taskID)
		if err != nil {
			return "", ErrNotFound
		}
		return task.WorkDir, nil
	}
	return s.safePath(taskID, rel)
}

func (s *Service) safePath(taskID, rel string) (string, error) {
	task, err := s.Store.GetTask(taskID)
	if err != nil {
		return "", ErrNotFound
	}
	clean := filepath.Clean(strings.ReplaceAll(rel, "/", string(filepath.Separator)))
	abs := clean
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(task.WorkDir, clean)
	}
	if !insideDir(task.WorkDir, abs) {
		return "", ErrPathOutside
	}
	return abs, nil
}

func (s *Service) resolveWorkDir(requested string) (string, error) {
	if strings.TrimSpace(requested) == "" {
		return s.allocateDir()
	}
	clean := filepath.Clean(requested)
	if !filepath.IsAbs(clean) {
		clean = filepath.Join(s.Root, clean)
	}
	if forbiddenWorkDir(clean) {
		return "", fmt.Errorf("工作目录不受支持")
	}
	if err := os.MkdirAll(clean, 0o755); err != nil {
		return "", err
	}
	return clean, nil
}

func (s *Service) allocateDir() (string, error) {
	day := s.now().Format("20060102")
	for n := 1; n < 1000; n++ {
		dir := filepath.Join(s.Root, fmt.Sprintf("%s-%02d", day, n))
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			return dir, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("无法分配工作目录")
}

func (s *Service) appendEvent(taskID string, ev AgentEvent) error {
	seq, err := s.Store.NextEventSeq(taskID)
	if err != nil {
		return err
	}
	ev.Seq = seq
	if ev.TS == "" {
		ev.TS = s.now().Format(time.RFC3339)
	}
	return s.Store.InsertEvent(taskID, ev)
}

func (s *Service) notify(task TaskRecord) {
	if s.Notify == nil {
		return
	}
	body := fmt.Sprintf("任务%s：%s", statusCN(task.Status), clip(task.Prompt, 80))
	if err := s.Notify("Lunitide", clip(body, 500)); err != nil {
		log.Printf("agent hub toast: %v", err)
	}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func forbiddenWorkDir(path string) bool {
	clean := filepath.Clean(path)
	vol := filepath.VolumeName(clean)
	rest := strings.TrimPrefix(clean, vol)
	if rest == `\` || rest == `/` || rest == "" {
		return true
	}
	low := strings.ToLower(clean)
	for _, name := range []string{"Windows", "Program Files", "Program Files (x86)", "ProgramData"} {
		banned := strings.ToLower(filepath.Clean(vol + string(os.PathSeparator) + name))
		if low == banned || strings.HasPrefix(low, banned+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func peekWorkDir(workDir string) []Artifact {
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return nil
	}
	out := make([]Artifact, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == promptFileName {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			continue
		}
		art := fileArtifact(filepath.Join(workDir, entry.Name()))
		art.Source = "scan"
		out = append(out, art)
	}
	inboxRoot := filepath.Join(workDir, inboxDirName)
	if _, statErr := os.Stat(inboxRoot); statErr != nil {
		return out
	}
	_ = filepath.WalkDir(inboxRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if path != inboxRoot {
				if skipScanDir(d.Name()) {
					return fs.SkipDir
				}
				if d.Type()&os.ModeSymlink != 0 {
					return fs.SkipDir
				}
			}
			return nil
		}
		if d.Name() == promptFileName {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			return nil
		}
		art := fileArtifact(filepath.Clean(path))
		art.Size = info.Size()
		art.Source = "inbox"
		out = append(out, art)
		return nil
	})
	return out
}

func mergeLiveArtifacts(stored, live []Artifact) []Artifact {
	seen := map[string]Artifact{}
	for _, art := range stored {
		seen[filepath.Clean(art.Path)] = art
	}
	for _, art := range live {
		key := filepath.Clean(art.Path)
		if _, ok := seen[key]; !ok {
			seen[key] = art
		}
	}
	out := make([]Artifact, 0, len(seen))
	for _, art := range seen {
		out = append(out, art)
	}
	return out
}

func looksRateLimited(err error, events []AgentEvent) bool {
	low := strings.ToLower(eventText(err, events))
	return strings.Contains(low, "rate limit") || strings.Contains(low, "429") || strings.Contains(low, "too many requests")
}

func looksUnsupportedModel(text string) bool {
	low := strings.ToLower(text)
	return strings.Contains(low, "requires a newer version") || strings.Contains(low, "model metadata") && strings.Contains(low, "not found")
}

func eventText(err error, events []AgentEvent) string {
	var b strings.Builder
	if err != nil {
		b.WriteString(err.Error())
	}
	for _, ev := range events {
		b.WriteString(ev.Title)
		b.WriteString(ev.Detail)
	}
	return b.String()
}

func statusCN(status string) string {
	switch status {
	case "success":
		return "已完成"
	case "failed":
		return "失败"
	case "timeout":
		return "超时"
	case "cancelled":
		return "已取消"
	default:
		return status
	}
}

func clip(s string, max int) string {
	if s == "" {
		return "任务已结束"
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}

func int64Ptr(v int64) *int64 { return &v }

func chineseProcError(err error) string {
	if err == nil {
		return "执行失败"
	}
	msg := err.Error()
	if strings.Contains(msg, "未检测到") || strings.Contains(msg, "进程路径") {
		return msg
	}
	return "CLI 执行失败，请查看时间线"
}
