package producthub

import (
	"context"
	"fmt"
	"regexp"
	"strings"
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
		out = append(out, finding(sev, code, "probe."+task.ID, task.Title, task.Evidence,
			taskCause(task), taskFix(task), "再点一次「重新检测」，本条证据应变化或消失", status))
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
		return "这一项已经在本机跑完"
	}
}

func taskFix(task TaskResult) string {
	switch task.ID {
	case "dictate":
		return "在语音设置确认默认识别模型已安装，再点重新检测。诊断不安装模型，也不改听写代码。"
	case "play":
		return "确认本机音频设备可用。诊断只播放一段短探测音，不改播放器。"
	case "download":
		return "对照证据里的字节数和本机模型目录。诊断只核对长度和已安装文件，不重新下载。"
	case "ocr":
		return "确认 Windows 图片识别语言可用。诊断只跑一张探测图，不改对话代码。"
	default:
		return "按证据复核。诊断不改产品代码。"
	}
}

// ClassifyLog keeps only lines that match a fault class and quotes them.
func ClassifyLog(text string) []Finding {
	var out []Finding
	lines := strings.Split(text, "\n")
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
			"再点「重新检测」。日志里这句还在，本条就还在。", "open"))
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
		add("PH_L12", "反复调用", fmt.Sprintf("%s ×%d", sample[bestKey], bestN), "同一条失败在最近的引擎日志里反复出现。", "先停掉会触发它的那一轮，再看是重试没有上限还是同一请求被重复发出。")
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
	return healthScore(findings, catalog), catalog, false
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
