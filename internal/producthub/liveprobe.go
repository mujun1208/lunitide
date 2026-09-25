package producthub

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// TaskResult is one real check started by 「重新检测」.
// Status is pass, fail, or untested. Untested stays in the total and is not a pass.
type TaskResult struct {
	ID       string
	Title    string
	Status   string
	Evidence string
}

// LandscapeNote is one axis already saved on the landscape page.
// The diagnostic quotes it. It does not invent a rank.
type LandscapeNote struct {
	Name   string
	Axis   string
	Score  string
	Note   string
	Source string
	Date   string
}

var (
	sizeMismatch = regexp.MustCompile(`expected (\d+) bytes, server offered (\d+)`)
	taskCodes    = map[string]string{
		"dictate":  "PH_L01",
		"play":     "PH_L02",
		"download": "PH_L03",
		"ocr":      "PH_L04",
	}
)

// ScoreLive is the ring: task passes over tasks plus log faults.
// Landscape notes are not part of this score.
func ScoreLive(tasks []TaskResult, faults []Finding) (ProbeScore, int) {
	passed := 0
	for _, task := range tasks {
		if task.Status == "pass" {
			passed++
		}
	}
	total := len(tasks) + len(faults)
	if total < 1 {
		total = 1
	}
	return ProbeScore{Passed: passed, Total: total}, 100 * passed / total
}

// TaskFindings turns each real task into a report row.
func TaskFindings(tasks []TaskResult) []Finding {
	out := make([]Finding, 0, len(tasks))
	for _, task := range tasks {
		code := taskCodes[task.ID]
		if code == "" {
			code = "PH_L00"
		}
		sev, status := "info", "pass"
		switch task.Status {
		case "fail":
			sev, status = "error", "open"
		case "untested":
			sev, status = "warn", "open"
		}
		verify := "执行净化会再跑这一项。复查通过才改为 fixed；仍失败则保持 open，方案和证据换成这次的原文。"
		if status == "pass" {
			verify = "已经通过。执行净化不会把它改成待修复。"
		}
		out = append(out, finding(sev, code, "probe."+task.ID, task.Title, task.Evidence,
			taskCause(task), taskFix(task), verify, status))
	}
	return out
}

func taskCause(task TaskResult) string {
	switch task.Status {
	case "fail":
		return "这一次真实跑动没有完成"
	case "untested":
		return "这一项没有跑起来，不能算通过"
	default:
		return "探测通过。这不是一条待修缺陷。"
	}
}

func taskFix(task TaskResult) string {
	if task.Status == "pass" {
		return "这项已经通过，不用再处理。"
	}
	switch task.ID {
	case "dictate":
		if strings.Contains(task.Evidence, "runtime") {
			return "证据是精识别运行时没装上，不是流式听写已经坏了。净化会再跑听写。只有识别跑完、证据里不再是 runtime 未安装，本条才改为 fixed。"
		}
		return "净化会再跑听写。证据里的失败原因消失并且识别跑完，本条才改为 fixed。仍失败就保持 open，方案换成新的证据。"
	case "play":
		return "净化会再播 0.2 秒探测音。证据变成已播放，本条改为 fixed。仍失败就保持 open，方案换成新的报错。"
	case "download":
		return "净化会再对一次记录字节、服务器字节和本机模型目录。两边一致且目录已核对，本条改为 fixed。这一步不重新下载。"
	case "ocr":
		return "净化会再生成探测图并识图。读出 OCR，本条改为 fixed。读不出就保持 open，方案换成这次读到的文字。"
	default:
		return "净化会再跑这一项。通过才改为 fixed。"
	}
}

// logClock is the clock for 「今天的日志」. Tests pin it.
var logClock = time.Now

func logLineCurrent(line string, now time.Time) bool {
	fields := strings.SplitN(strings.TrimSpace(line), " ", 3)
	if len(fields) < 2 || strings.Count(fields[0], "/") != 2 || strings.Count(fields[1], ":") != 2 {
		return true
	}
	t, err := time.ParseInLocation("2006/01/02 15:04:05", fields[0]+" "+fields[1], now.Location())
	if err != nil {
		return true
	}
	return t.Year() == now.Year() && t.YearDay() == now.YearDay()
}

// ClassifyLog keeps only lines that match a fault class and quotes them.
func ClassifyLog(text string) []Finding {
	var out []Finding
	now := logClock()
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !logLineCurrent(line, now) {
			continue
		}
		lines = append(lines, line)
	}
	counts := map[string]int{}
	sample := map[string]string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key := logKey(line)
		counts[key]++
		if _, ok := sample[key]; !ok {
			sample[key] = line
		}
	}
	add := func(code, title, evidence, cause, fix string) {
		for _, f := range out {
			if f.ErrorCode == code {
				return
			}
		}
		out = append(out, finding("error", code, "log."+code, title, evidence, cause, fix,
			"执行净化会再读今天的引擎日志。这句不在了，本条改为 fixed。还在就保持 open，证据换成最新那一行。", "open"))
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if m := sizeMismatch.FindStringSubmatch(line); len(m) == 3 {
			add("PH_L10", "下载校验失败", line, "服务器给出的文件大小和安装包记下的大小不一致，下载在收到内容前就停了。",
				"把安装包里的大小和摘要改成服务器现在这份，或等下一版。不要把这条当成网络中断。")
		}
		if strings.Contains(line, "panic:") || strings.Contains(line, "host crash") {
			add("PH_L11", "宕机", line, "进程打出了 panic 或宿主崩溃。", "保留这份日志，按这句定位崩溃点。诊断不重启产品。")
		}
		if strings.Contains(line, "context deadline exceeded") || strings.Contains(line, "超时") {
			add("PH_L13", "卡壳", line, "有一步超过了自己的时限。", "看这句前面的方法名，确认是哪一条链路停住。")
		}
		if strings.Contains(line, "IRandomAccessStream") || strings.Contains(line, "IAsyncOperation") {
			add("PH_L14", "图片识别链路错误", line, "打开图片时接口转换失败，识别文字被丢掉。", "识图脚本要按带内容类型的流来等。正在运行的旧安装包仍是旧脚本。")
		}
		if strings.Contains(line, "无法执行") {
			add("PH_L15", "流程不通", line, "这一步停在无法执行，后面的链路没有走完。", "按这句里的步骤看是入口缺失还是上一步没有结果。")
		}
		if strings.Contains(line, "input rejected by filter") {
			add("PH_L16", "点击被拦住", line, "屏幕点击没有通过输入校验。坐标、按钮或过期画面不能当成已经点开。", "打开第一条网页时直接打开搜索结果里的链接，不再对过期坐标点击。")
		}
		if strings.Contains(line, "模型请求结果尚无法确认") || strings.Contains(line, "OUTCOME_UNKNOWN") {
			add("PH_L17", "模型没有返回", line, "模型连接没有返回正文。这是连接阶段的失败，不是播放器坏了。", "播放、打开这种本地就能决定的动作，模型失败时仍然执行。只统计今天的日志，昨天的这句不再占着当前故障。")
		}
		if strings.Contains(line, "SUMMARY_FAILED") {
			add("PH_L18", "周摘要失败", line, "周归档的摘要模型调用失败。这是记忆归档，不是播放或听写。", "证据里冒号后是失败码和模型原因。同一周失败会暂停数小时；进程重启后会再试。诊断不改归档代码。")
		}
	}
	bestKey, bestN := "", 0
	for _, line := range lines {
		key := logKey(strings.TrimSpace(line))
		n := counts[key]
		if n < 3 || n <= bestN || len(key) < 24 {
			continue
		}
		if !strings.Contains(key, "failure") && !strings.Contains(key, "failed") && !strings.Contains(key, "失败") {
			continue
		}
		bestKey, bestN = key, n
	}
	if bestKey != "" {
		add("PH_L12", "反复调用", fmt.Sprintf("%s ×%d", sample[bestKey], bestN), "同一句失败在今天的引擎日志里出现了多次。冒号后面是状态。", "按证据那一行处理，不要把它当成播放故障。已经过去的日期不再计入。")
	}
	return out
}

func logKey(line string) string {
	fields := strings.SplitN(line, " ", 3)
	if len(fields) == 3 && strings.Count(fields[0], "/") == 2 && strings.Count(fields[1], ":") == 2 {
		return fields[2]
	}
	return line
}

// LandscapeFindings quotes products already picked on the landscape page.
func LandscapeFindings(notes []LandscapeNote) []Finding {
	grouped := map[string][]LandscapeNote{}
	var order []string
	for _, note := range notes {
		name := strings.TrimSpace(note.Name)
		if name == "" || isSelfName(name) {
			continue
		}
		if _, ok := grouped[name]; !ok {
			order = append(order, name)
		}
		grouped[name] = append(grouped[name], note)
	}
	if len(order) == 0 {
		return []Finding{finding("info", "PH_L90", "landscape.none", "对照",
			"尚未在图景页选择产品", "对照名单是空的",
			"先到图景页加入产品并执行对照，再回到诊断点重新检测。",
			"图景页有名单后，本条换成那些产品的已保存结论。", "note")}
	}
	out := make([]Finding, 0, len(order))
	for _, name := range order {
		var parts []string
		for _, note := range grouped[name] {
			parts = append(parts, fmt.Sprintf("%s=%s %s（%s %s）", note.Axis, note.Score, note.Note, note.Source, note.Date))
		}
		out = append(out, finding("info", "PH_L91", "landscape."+name, "对照 "+name,
			name+"："+strings.Join(parts, "；"), "结论来自图景页已保存的对照，不是这次新编的排名。",
			"若某一维标成待核验，补上带来源和日期的说明后再检测。",
			"图景页改名单后重新检测，本条跟着变。", "note"))
	}
	return out
}

type liveTasksFn func(context.Context) ([]TaskResult, string, []LandscapeNote)

type liveCtxKey struct{}
type landscapeCtxKey struct{}
type engineLogCtxKey struct{}

// WithEngineLog replaces the engine log for one purify recheck. Tests use it.
func WithEngineLog(ctx context.Context, text string) context.Context {
	return context.WithValue(ctx, engineLogCtxKey{}, text)
}

func engineLog(ctx context.Context) string {
	if text, ok := ctx.Value(engineLogCtxKey{}).(string); ok {
		return text
	}
	return readEngineLog()
}

// recheckLive runs the same check that produced the finding.
// A pass or a log line that is gone today becomes fixed. A failure stays open
// and the plan is the new evidence, not a tag that pretends it was handled.
func recheckLive(ctx context.Context, f Finding) (status string, applied bool, evidence, fix string) {
	if f.ErrorCode == "PH_L90" || f.ErrorCode == "PH_L91" {
		return f.Status, false, f.Evidence, "对照来自图景页。净化不改名单，也不把它算进实测分。"
	}
	if f.Status == "pass" {
		return "fixed", true, f.Evidence, "复查时这一项已经是通过。"
	}
	if id := strings.TrimPrefix(f.StableKey, "probe."); id != f.StableKey {
		task := rerunProbe(ctx, id)
		if task.Status == "pass" {
			return "fixed", true, task.Evidence, "复查通过。" + task.Evidence
		}
		if task.ID == "" {
			task = TaskResult{ID: id, Status: "untested", Evidence: "复查没有这项"}
		}
		return "open", false, task.Evidence, taskFix(task)
	}
	if strings.HasPrefix(f.StableKey, "log.") {
		for _, next := range ClassifyLog(engineLog(ctx)) {
			if next.ErrorCode == f.ErrorCode {
				return "open", false, next.Evidence, next.Fix
			}
		}
		return "fixed", true, "今天的日志里已经没有这句", "复查通过。这句不再占当前故障。"
	}
	return "open", false, f.Evidence, f.Fix
}

func rerunProbe(ctx context.Context, id string) TaskResult {
	if fn, ok := ctx.Value(liveCtxKey{}).(liveTasksFn); ok && fn != nil {
		tasks, _, _ := fn(ctx)
		for _, task := range tasks {
			if task.ID == id {
				return task
			}
		}
		return TaskResult{}
	}
	switch id {
	case "dictate":
		return probeDictate(ctx)
	case "play":
		return probePlay(ctx)
	case "download":
		return probeDownload(ctx)
	case "ocr":
		return probeOCR(ctx)
	default:
		return TaskResult{}
	}
}

// WithLiveTasks replaces the real checks. Tests use it so a manual refresh
// does not open a microphone, a model, or the network.
func WithLiveTasks(ctx context.Context, fn liveTasksFn) context.Context {
	return context.WithValue(ctx, liveCtxKey{}, fn)
}

// WithLandscape attaches the products already picked on the landscape page.
func WithLandscape(ctx context.Context, notes []LandscapeNote) context.Context {
	return context.WithValue(ctx, landscapeCtxKey{}, notes)
}

func invokeLive(ctx context.Context) ([]TaskResult, string, []LandscapeNote) {
	if fn, ok := ctx.Value(liveCtxKey{}).(liveTasksFn); ok && fn != nil {
		return fn(ctx)
	}
	tasks, logText := DefaultLiveRun(ctx)
	notes, _ := ctx.Value(landscapeCtxKey{}).([]LandscapeNote)
	return tasks, logText, notes
}

func liveHealth(probe ProbeScore) int {
	total := probe.Total
	if total < 1 {
		total = 1
	}
	return 100 * probe.Passed / total
}

func displayedScore(ed Edition, findings []Finding, catalog ProbeScore) (int, ProbeScore, bool) {
	if ed.LiveProbe.Total > 0 {
		return liveHealth(ed.LiveProbe), ed.LiveProbe, true
	}
	if probe, ok := probeFromFindings(findings); ok {
		return liveHealth(probe), probe, true
	}
	return healthScore(findings, catalog), catalog, false
}

// probeFromFindings rebuilds the measured ring after the snapshot is loaded.
// The snapshot stores findings, not LiveProbe, so a later export used to
// print the catalog coverage number next to the real probe rows.
func probeFromFindings(findings []Finding) (ProbeScore, bool) {
	passed, total := 0, 0
	for _, f := range findings {
		if !strings.HasPrefix(f.ErrorCode, "PH_L") {
			continue
		}
		if f.ErrorCode == "PH_L90" || f.ErrorCode == "PH_L91" {
			continue
		}
		total++
		if f.Status == "pass" || f.Status == "fixed" {
			passed++
		}
	}
	if total == 0 {
		return ProbeScore{}, false
	}
	return ProbeScore{Passed: passed, Total: total}, true
}

func liveFindings(in []Finding) []Finding {
	var out []Finding
	for _, f := range in {
		if strings.HasPrefix(f.ErrorCode, "PH_L") {
			out = append(out, f)
		}
	}
	return out
}

func isSelfName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "lunitide", "月汐", "lunitide / 月汐":
		return true
	default:
		return false
	}
}
