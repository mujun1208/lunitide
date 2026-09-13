package projecttask

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/projectapp"
)

type Item struct {
	ID                string                `json:"id"`
	Title             string                `json:"title"`
	Module            string                `json:"module,omitempty"`
	Priority          string                `json:"priority,omitempty"`
	Status            string                `json:"status"`
	SourceID          string                `json:"sourceId,omitempty"`
	Notes             string                `json:"notes,omitempty"`
	Acceptance        string                `json:"acceptance,omitempty"`
	TargetRelPath     string                `json:"targetRelPath,omitempty"`
	Executor          project.Executor      `json:"executor,omitempty"`
	WorkSessionID     string                `json:"workSessionId,omitempty"`
	HubThreadID       string                `json:"hubThreadId,omitempty"`
	LastRunKind       string                `json:"lastRunKind,omitempty"`
	LastResultSummary string                `json:"lastResultSummary,omitempty"`
	LastResultAt      string                `json:"lastResultAt,omitempty"`
	TestReturnID      string                `json:"testReturnId,omitempty"`
	TestReturn        *TestReturn           `json:"testReturn,omitempty"`
	OpenedAt          string                `json:"openedAt,omitempty"`
	SourceKind        string                `json:"sourceKind,omitempty"`
	ChangeKind        string                `json:"changeKind,omitempty"`
	NeedsReprocess    bool                  `json:"needsReprocess,omitempty"`
	SourceFingerprint string                `json:"sourceFingerprint,omitempty"`
	DesignRef         *DesignRef            `json:"designRef,omitempty"`
	SelfTestPass      bool                  `json:"selfTestPass,omitempty"`
	Method            string                `json:"method,omitempty"`
	Path              string                `json:"path,omitempty"`
	OperationID       string                `json:"operationId,omitempty"`
	RequiredKinds     []string              `json:"requiredKinds,omitempty"`
	KindResults       map[string]KindResult `json:"kindResults,omitempty"`
	MemberIDs         []string              `json:"memberIds,omitempty"`
	ReturnTargetKind  string                `json:"returnTargetKind,omitempty"`
}

type DesignRef struct {
	DocumentType string `json:"documentType"`
	Hint         string `json:"hint,omitempty"`
}

type KindResult struct {
	Status         string `json:"status"`
	At             string `json:"at,omitempty"`
	Summary        string `json:"summary,omitempty"`
	EvidenceDigest string `json:"evidenceDigest,omitempty"`
}

type TestReturn struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
	At     string `json:"at"`
}

type Doc struct {
	Version            int    `json:"version"`
	BoardKind          string `json:"boardKind,omitempty"`
	SourceDocumentType string `json:"sourceDocumentType,omitempty"`
	SourceDigest       string `json:"sourceDigest,omitempty"`
	Items              []Item `json:"items"`
}

type Brief struct {
	ItemID        string      `json:"itemId"`
	Title         string      `json:"title"`
	Acceptance    string      `json:"acceptance"`
	TargetRelPath string      `json:"targetRelPath"`
	RootPath      string      `json:"rootPath"`
	TestReturn    *TestReturn `json:"testReturn,omitempty"`
	Text          string      `json:"text"`
}

func Parse(raw []byte) (Doc, error) {
	var doc Doc
	if len(raw) == 0 {
		return Doc{Version: 1}, nil
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Version != 1 {
		return Doc{Version: 1}, nil
	}
	return doc, nil
}

func ParseStrict(raw []byte) (Doc, error) {
	if len(raw) == 0 {
		return Doc{}, projectapp.ErrBoardSourceInvalid
	}
	var doc Doc
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Version != 1 {
		return Doc{}, projectapp.ErrBoardSourceInvalid
	}
	return doc, nil
}

func Find(doc Doc, id string) (int, Item, bool) {
	for i, item := range doc.Items {
		if item.ID == id {
			return i, item, true
		}
	}
	return -1, Item{}, false
}

func ImportMissing(dst, src Doc) Doc {
	seen := map[string]bool{}
	for _, item := range dst.Items {
		seen[item.ID] = true
		if item.SourceID != "" {
			seen[item.SourceID] = true
		}
	}
	for _, item := range src.Items {
		if seen[item.ID] || (item.SourceID != "" && seen[item.SourceID]) {
			continue
		}
		item.Status = "pending"
		item.SourceID = item.ID
		dst.Items = append(dst.Items, item)
		seen[item.ID] = true
	}
	dst.Version = 1
	return dst
}

func BuildTestItems(dev Doc) Doc {
	out := Doc{Version: 1}
	for _, item := range dev.Items {
		if item.Status != "dev_done" {
			continue
		}
		out.Items = append(out.Items, Item{
			ID:       "T-" + item.ID,
			Title:    "测试：" + item.Title,
			Status:   "pending",
			SourceID: item.ID,
		})
	}
	return out
}

func Open(dev *Doc, itemID, root, codeRoot string, executor project.Executor) (Brief, error) {
	i, item, ok := Find(*dev, itemID)
	if !ok {
		return Brief{}, projectapp.ErrTaskNotFound
	}
	if item.Status == "pending" || (item.TestReturnID != "" && item.Status != "in_progress") {
		item.Status = "in_progress"
	}
	if item.Status == "dev_done" && item.TestReturn != nil {
		item.Status = "in_progress"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	item.OpenedAt = now
	item.Executor = executor
	item.LastRunKind = string(executor)
	if item.TargetRelPath == "" {
		item.TargetRelPath = codeRoot
		if item.TargetRelPath == "" {
			item.TargetRelPath = "src"
		}
	}
	dev.Items[i] = item
	brief := Brief{
		ItemID: item.ID, Title: item.Title, Acceptance: item.Acceptance,
		TargetRelPath: item.TargetRelPath, RootPath: root, TestReturn: item.TestReturn,
	}
	brief.Text = formatBrief(brief)
	return brief, nil
}

func formatBrief(b Brief) string {
	var bld strings.Builder
	bld.WriteString("任务 ")
	bld.WriteString(b.ItemID)
	bld.WriteString("：")
	bld.WriteString(b.Title)
	bld.WriteString("\n验收：")
	if b.Acceptance == "" {
		bld.WriteString("（无）")
	} else {
		bld.WriteString(b.Acceptance)
	}
	bld.WriteString("\n目标路径：")
	bld.WriteString(b.TargetRelPath)
	bld.WriteString("\n项目根：")
	bld.WriteString(b.RootPath)
	if b.TestReturn != nil {
		bld.WriteString("\n测试退回 ")
		bld.WriteString(b.TestReturn.ID)
		bld.WriteString(" @ ")
		bld.WriteString(b.TestReturn.At)
		bld.WriteString("：")
		bld.WriteString(b.TestReturn.Reason)
	}
	bld.WriteString("\n约束：只改本任务范围；不要 git commit/push；不要把文件挪出项目根。")
	return bld.String()
}

func Report(dev *Doc, itemID, summary string, executor project.Executor, hubThreadID string) error {
	summary = strings.TrimSpace(summary)
	if n := utf8.RuneCountInString(summary); n < 1 || n > 2000 {
		return projectapp.ErrInvalidTransition
	}
	i, item, ok := Find(*dev, itemID)
	if !ok {
		return projectapp.ErrTaskNotFound
	}
	item.LastResultSummary = summary
	item.LastResultAt = time.Now().UTC().Format(time.RFC3339)
	item.LastRunKind = string(executor)
	item.Executor = executor
	if hubThreadID != "" {
		item.HubThreadID = hubThreadID
	}
	dev.Items[i] = item
	return nil
}

func Complete(dev, test *Doc, itemID string) error {
	i, item, ok := Find(*dev, itemID)
	if !ok {
		return projectapp.ErrTaskNotFound
	}
	if strings.TrimSpace(item.LastResultSummary) == "" {
		return projectapp.ErrDevIncomplete
	}
	if !item.SelfTestPass {
		return projectapp.ErrSelfTestRequired
	}
	item.Status = "dev_done"
	item.NeedsReprocess = false
	dev.Items[i] = item
	if test == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for j := range test.Items {
		if test.Items[j].SourceID == itemID && test.Items[j].Status == "test_fail" {
			test.Items[j].Status = "pending"
			note := "开发已再交付 @ " + now
			if test.Items[j].Notes != "" {
				test.Items[j].Notes += "; " + note
			} else {
				test.Items[j].Notes = note
			}
		}
	}
	return nil
}

func ReturnFromTest(dev, test *Doc, testItemID, reason string) error {
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n < 1 || n > 2000 {
		return projectapp.ErrTestReasonRequired
	}
	ti, testItem, ok := Find(*test, testItemID)
	if !ok {
		return projectapp.ErrTaskNotFound
	}
	if testItem.SourceID == "" {
		return projectapp.ErrTestNoSource
	}
	di, devItem, ok := Find(*dev, testItem.SourceID)
	if !ok {
		return projectapp.ErrTaskNotFound
	}
	now := time.Now().UTC().Format(time.RFC3339)
	testItem.Status = "test_fail"
	if testItem.Notes != "" {
		testItem.Notes += "; " + reason
	} else {
		testItem.Notes = reason
	}
	test.Items[ti] = testItem
	devItem.Status = "in_progress"
	devItem.SelfTestPass = false
	devItem.NeedsReprocess = true
	devItem.TestReturnID = testItemID
	devItem.TestReturn = &TestReturn{ID: testItemID, Reason: reason, At: now}
	if testItem.SourceKind != "" {
		devItem.ReturnTargetKind = testItem.SourceKind
	} else {
		devItem.ReturnTargetKind = "dev"
	}
	dev.Items[di] = devItem
	return nil
}

func DevComplete(doc Doc) error {
	if len(doc.Items) == 0 {
		return nil
	}
	for _, item := range doc.Items {
		if item.Status != "dev_done" {
			return projectapp.ErrDevIncomplete
		}
	}
	return nil
}

func TestClosed(doc Doc) error {
	for _, item := range doc.Items {
		if item.Status == "test_fail" || item.Status == "pending" || item.Status == "in_progress" {
			return projectapp.ErrTestOpen
		}
	}
	return nil
}
