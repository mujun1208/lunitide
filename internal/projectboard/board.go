package projectboard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/projecttask"
)

type Kind string

const (
	KindInterface    Kind = "interface"
	KindDev          Kind = "dev"
	KindTest         Kind = "test"
	KindIntegration  Kind = "integration"
)

type Stats struct {
	Total          int `json:"total"`
	Added          int `json:"added"`
	Modified       int `json:"modified"`
	Unchanged      int `json:"unchanged"`
	Removed        int `json:"removed"`
	Done           int `json:"done"`
	Pending        int `json:"pending"`
	InProgress     int `json:"inProgress"`
	Returned       int `json:"returned"`
	NeedsReprocess int `json:"needsReprocess"`
}

func Fingerprint(item projecttask.Item) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{item.Title, item.Method, item.Path, item.OperationID, item.Acceptance}, "\n")))
	return hex.EncodeToString(sum[:])
}

func Sync(board, source projecttask.Doc) projecttask.Doc {
	if board.Version == 0 {
		board.Version = 1
	}
	index := map[string]int{}
	for i, item := range board.Items {
		index[item.ID] = i
	}
	seen := map[string]bool{}
	for _, src := range source.Items {
		seen[src.ID] = true
		fp := Fingerprint(src)
		if i, ok := index[src.ID]; ok {
			cur := board.Items[i]
			if cur.SourceFingerprint != "" && cur.SourceFingerprint != fp {
				cur.ChangeKind = "modified"
				cur.NeedsReprocess = true
			} else if cur.ChangeKind == "" {
				cur.ChangeKind = "unchanged"
			}
			cur.Title = src.Title
			cur.Method = src.Method
			cur.Path = src.Path
			cur.OperationID = src.OperationID
			if src.Acceptance != "" {
				cur.Acceptance = src.Acceptance
			}
			cur.SourceFingerprint = fp
			board.Items[i] = cur
			continue
		}
		src.Status = "pending"
		src.ChangeKind = "added"
		src.NeedsReprocess = true
		src.SourceFingerprint = fp
		if src.SourceID == "" {
			src.SourceID = src.ID
		}
		board.Items = append(board.Items, src)
		index[src.ID] = len(board.Items) - 1
	}
	for i, item := range board.Items {
		if !seen[item.ID] {
			item.ChangeKind = "removed"
			item.NeedsReprocess = true
			board.Items[i] = item
		}
	}
	return board
}

func Summarize(doc projecttask.Doc) Stats {
	var s Stats
	for _, item := range doc.Items {
		switch item.ChangeKind {
		case "added":
			s.Added++
		case "modified":
			s.Modified++
		case "removed":
			s.Removed++
		default:
			if item.ChangeKind == "unchanged" {
				s.Unchanged++
			}
		}
		if item.ChangeKind == "removed" {
			continue
		}
		s.Total++
		switch item.Status {
		case "dev_done", "test_pass":
			s.Done++
		case "pending":
			s.Pending++
		case "in_progress":
			s.InProgress++
		}
		if item.NeedsReprocess {
			s.NeedsReprocess++
		}
		if item.TestReturn != nil && item.Status != "dev_done" && item.Status != "test_pass" {
			s.Returned++
		}
	}
	if s.Unchanged == 0 {
		for _, item := range doc.Items {
			if item.ChangeKind == "unchanged" {
				s.Unchanged++
			}
		}
	}
	return s
}

func FormatStats(s Stats) string {
	return "共 " + itoa(s.Total) + " 条 · 新增 " + itoa(s.Added) + " · 变更 " + itoa(s.Modified) +
		" · 未改 " + itoa(s.Unchanged) + " · 已做 " + itoa(s.Done) + " · 未做 " + itoa(s.Total-s.Done) +
		" · 待再处理 " + itoa(s.NeedsReprocess)
}

func Dirty(doc projecttask.Doc) bool {
	for _, item := range doc.Items {
		if item.NeedsReprocess {
			return true
		}
	}
	return false
}

func RequireSelfTest(item projecttask.Item) error {
	if !item.SelfTestPass {
		return projectapp.ErrSelfTestRequired
	}
	return nil
}

func IntegrationReady(scene projecttask.Item, tests projecttask.Doc) error {
	if len(scene.MemberIDs) == 0 {
		return projectapp.ErrIntegrationNotReady
	}
	for _, id := range scene.MemberIDs {
		_, item, ok := projecttask.Find(tests, id)
		if !ok || item.Status != "test_pass" {
			return projectapp.ErrIntegrationNotReady
		}
	}
	return nil
}

func BuildTestBoard(iface, dev projecttask.Doc) projecttask.Doc {
	out := projecttask.Doc{Version: 1, BoardKind: string(KindTest)}
	for _, item := range iface.Items {
		if item.Status != "dev_done" || item.ChangeKind == "removed" {
			continue
		}
		out.Items = append(out.Items, projecttask.Item{
			ID: "T-" + item.ID, Title: "测试接口：" + item.Title, Status: "pending",
			SourceID: item.ID, SourceKind: "interface", RequiredKinds: []string{"unit"},
		})
	}
	for _, item := range dev.Items {
		if item.Status != "dev_done" || item.ChangeKind == "removed" {
			continue
		}
		out.Items = append(out.Items, projecttask.Item{
			ID: "T-" + item.ID, Title: "测试：" + item.Title, Status: "pending",
			SourceID: item.ID, SourceKind: "dev", RequiredKinds: []string{"unit"},
		})
	}
	return out
}

func ResetScenesForTest(scenes *projecttask.Doc, testItemID string) {
	for i := range scenes.Items {
		for _, id := range scenes.Items[i].MemberIDs {
			if id == testItemID && scenes.Items[i].Status == "test_pass" {
				scenes.Items[i].Status = "pending"
			}
		}
	}
}

func ParseOpenAPI(spec []byte) (projecttask.Doc, error) {
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
			Summary     string `json:"summary"`
		} `json:"paths"`
	}
	if json.Unmarshal(spec, &doc) != nil || doc.Paths == nil {
		return projecttask.Doc{}, projectapp.ErrBoardSourceInvalid
	}
	out := projecttask.Doc{Version: 1, BoardKind: string(KindInterface)}
	n := 0
	methods := []string{"get", "post", "put", "patch", "delete", "head", "options"}
	paths := make([]string, 0, len(doc.Paths))
	for path := range doc.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		ops := doc.Paths[path]
		for _, m := range methods {
			op, ok := ops[m]
			if !ok {
				continue
			}
			n++
			title := strings.ToUpper(m) + " " + path
			if op.Summary != "" {
				title = op.Summary
			}
			out.Items = append(out.Items, projecttask.Item{
				ID: "I" + pad3(n), Title: title, Status: "pending",
				Method: strings.ToUpper(m), Path: path, OperationID: op.OperationID,
			})
		}
	}
	return out, nil
}

func pad3(n int) string {
	s := itoa(n)
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}

func itoa(n int) string {
	if n < 0 {
		n = 0
	}
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
