package projectgen

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

const InterviewFile = "phase-interview.json"

var ErrInterviewInvalid = errors.New("project interview is invalid")

type Answer struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
	Value  string `json:"value"`
}

type PhaseInterview struct {
	Mode        string   `json:"mode"`
	CompletedAt string   `json:"completedAt,omitempty"`
	Answers     []Answer `json:"answers"`
}

type InterviewDoc struct {
	Version   int                       `json:"version"`
	ProjectID string                    `json:"projectId"`
	Phases    map[string]PhaseInterview `json:"phases"`
}

type Question struct {
	ID      string
	Prompt  string
	Options []string
}

func Phase1Questions() []Question {
	return []Question{
		{ID: "core_problem", Prompt: "本项目首先要解决什么？", Options: []string{"核心业务闭环", "内部提效", "对外交付", "其他"}},
		{ID: "system_shape", Prompt: "系统形态？", Options: []string{"桌面工具", "Web 应用", "桌面+本地服务", "其他"}},
		{ID: "stack", Prompt: "主要技术栈？", Options: []string{"沿用本仓库习惯", "Go+React", "其他"}},
		{ID: "data_store", Prompt: "数据存在哪？", Options: []string{"项目内 SQLite", "稍后再定", "其他"}},
		{ID: "tree_choice", Prompt: "目录树？", Options: []string{"用默认树", "我要改树", "其他"}},
		{ID: "rule_strictness", Prompt: "开发规范？", Options: []string{"按生成的开发/技术规范严格执行", "先出草稿我再改", "其他"}},
	}
}

func Phase2Questions() []Question {
	return []Question{
		{ID: "main_flows", Prompt: "有几条必须先设计的主业务流？", Options: []string{"1 条", "2–3 条", "4 条以上", "其他"}},
		{ID: "api_style", Prompt: "接口怎么给？", Options: []string{"OpenAPI REST", "仅内部模块调用", "两者都要", "其他"}},
		{ID: "module_cut", Prompt: "功能怎么切？", Options: []string{"按业务对象", "按页面", "按角色", "其他"}},
		{ID: "ui_depth", Prompt: "UI 详细设计要做到哪？", Options: []string{"线框+状态", "完整页面说明", "本阶段先清单", "其他"}},
		{ID: "integration_cut", Prompt: "集成测试怎么切场景？", Options: []string{"按主业务流", "按角色任务", "本阶段只出清单", "其他"}},
	}
}

func QuestionsForPhase(phase int) []Question {
	if phase == 1 {
		return Phase1Questions()
	}
	if phase == 2 {
		return Phase2Questions()
	}
	return nil
}

func ValidMode(mode string) bool {
	switch mode {
	case "guide", "council", "mixed":
		return true
	default:
		return false
	}
}

func NormalizeAnswer(a Answer) (Answer, error) {
	a.ID = strings.TrimSpace(a.ID)
	a.Prompt = strings.TrimSpace(a.Prompt)
	a.Value = strings.TrimSpace(a.Value)
	if a.ID == "" || utf8.RuneCountInString(a.Value) < 1 || utf8.RuneCountInString(a.Value) > 2000 {
		return Answer{}, ErrInterviewInvalid
	}
	if utf8.RuneCountInString(a.Prompt) > 200 {
		return Answer{}, ErrInterviewInvalid
	}
	return a, nil
}

func SavePhase(root, projectID string, phase int, mode, completedAt string, answers []Answer) (InterviewDoc, error) {
	if !ValidMode(mode) {
		return InterviewDoc{}, ErrInterviewInvalid
	}
	normalized := make([]Answer, 0, len(answers))
	for _, item := range answers {
		a, err := NormalizeAnswer(item)
		if err != nil {
			return InterviewDoc{}, err
		}
		normalized = append(normalized, a)
	}
	doc, err := Load(root)
	if err != nil {
		doc = InterviewDoc{Version: 1, ProjectID: projectID, Phases: map[string]PhaseInterview{}}
	}
	if doc.Phases == nil {
		doc.Phases = map[string]PhaseInterview{}
	}
	doc.Version = 1
	if projectID != "" {
		doc.ProjectID = projectID
	}
	key := strconv.Itoa(phase)
	doc.Phases[key] = PhaseInterview{Mode: mode, CompletedAt: completedAt, Answers: normalized}
	return doc, writeInterview(root, doc)
}

func Load(root string) (InterviewDoc, error) {
	raw, err := os.ReadFile(interviewPath(root))
	if err != nil {
		return InterviewDoc{}, err
	}
	var doc InterviewDoc
	if json.Unmarshal(raw, &doc) != nil || doc.Version != 1 {
		return InterviewDoc{}, ErrInterviewInvalid
	}
	if doc.Phases == nil {
		doc.Phases = map[string]PhaseInterview{}
	}
	return doc, nil
}

func AnswerMap(phase PhaseInterview) map[string]string {
	out := map[string]string{}
	for _, a := range phase.Answers {
		out[a.ID] = a.Value
	}
	return out
}

func interviewPath(root string) string {
	return filepath.Join(root, ".lunitide", InterviewFile)
}

func writeInterview(root string, doc InterviewDoc) error {
	if err := os.MkdirAll(filepath.Join(root, ".lunitide"), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(interviewPath(root), body, 0o644)
}
