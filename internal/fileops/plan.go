package fileops

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/oklog/ulid/v2"
)

var (
	ErrInvalidPlan   = errors.New("fileops: invalid plan")
	ErrOutsideRoot   = errors.New("fileops: path escapes workspace")
	ErrCollision     = errors.New("fileops: destination already exists")
	ErrPlanNotFound  = errors.New("fileops: plan not found")
	ErrNothingToUndo = errors.New("fileops: nothing to undo")
	ErrApplyPartial  = errors.New("fileops: apply stopped after a failed step")
	ErrInputChanged  = errors.New("fileops: source changed since plan")
	ErrUndoCollision = errors.New("fileops: undo destination already exists")
	ErrCrossVolume   = errors.New("fileops: cross-volume move is not supported")
	ErrAlreadyUndone = errors.New("fileops: undone plan cannot be re-applied")
	ErrSourceMissing = errors.New("fileops: source missing")
	ErrUnavailable   = errors.New("fileops: unavailable")
	ErrInvalidRoot   = errors.New("fileops: root must be absolute")
)

// UserMessage returns the Chinese fail-closed text for known sentinels.
// Unknown errors keep their original text; do not invent a copy pipeline.
func UserMessage(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrInputChanged):
		return "源文件在计划后已被修改，已停止执行，未覆盖原文件"
	case errors.Is(err, ErrCollision):
		return "目标路径已存在，已停止，未覆盖"
	case errors.Is(err, ErrUndoCollision):
		return "撤销目标已被修改，不能覆盖"
	case errors.Is(err, ErrNothingToUndo):
		return "没有可撤销的步骤"
	case errors.Is(err, ErrCrossVolume):
		return "不支持跨卷移动，已停止，未复制"
	case errors.Is(err, ErrOutsideRoot):
		return "路径超出工作区"
	case errors.Is(err, ErrPlanNotFound):
		return "文件计划不存在"
	case errors.Is(err, ErrInvalidPlan):
		return "文件计划无效"
	case errors.Is(err, ErrApplyPartial):
		return "批次已停止，已完成的步骤保留"
	case errors.Is(err, ErrAlreadyUndone):
		return "已撤销的计划不能再次执行"
	case errors.Is(err, ErrSourceMissing):
		return "源文件不存在，已停止执行"
	case errors.Is(err, os.ErrPermission):
		return "没有权限，已停止执行"
	case errors.Is(err, ErrUnavailable):
		return "工作区尚未就绪"
	case errors.Is(err, ErrInvalidRoot):
		return "工作区路径无效"
	default:
		return "文件操作失败，已停止执行"
	}
}

type Action string

const (
	ActionMkdir  Action = "mkdir"
	ActionRename Action = "rename"
	ActionMove   Action = "move"
	ActionWrite  Action = "write"
)

type Item struct {
	ID           string `json:"id"`
	Action       Action `json:"action"`
	From         string `json:"from,omitempty"`
	To           string `json:"to,omitempty"`
	Body         string `json:"body,omitempty"`
	SourceDigest string `json:"sourceDigest,omitempty"`
}

type Plan struct {
	ID        string `json:"id"`
	Recipe    string `json:"recipe,omitempty"`
	Root      string `json:"root"`
	Items     []Item `json:"items"`
	CreatedAt string `json:"createdAt"`
	Digest    string `json:"digest"`
}

type ItemStatus struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Error  string `json:"error,omitempty"`
	UndoTo string `json:"undoTo,omitempty"`
}

type Status struct {
	PlanID    string       `json:"planId"`
	State     string       `json:"state"`
	Items     []ItemStatus `json:"items"`
	AppliedAt string       `json:"appliedAt,omitempty"`
	UndoneAt  string       `json:"undoneAt,omitempty"`
}

type Service struct {
	root string
	dir  string
}

func New(root string) (*Service, error) {
	if !filepath.IsAbs(root) {
		return nil, ErrInvalidRoot
	}
	dir := filepath.Join(root, ".fileops")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Service{root: root, dir: dir}, nil
}

func (s *Service) Create(recipe string, items []Item) (Plan, error) {
	if s == nil {
		return Plan{}, ErrUnavailable
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	plan := Plan{ID: ulid.Make().String(), Recipe: recipe, Root: s.root, CreatedAt: now}
	for _, item := range items {
		if item.ID == "" {
			item.ID = ulid.Make().String()
		}
		if err := s.validateItem(item); err != nil {
			return Plan{}, err
		}
		plan.Items = append(plan.Items, s.stampSource(item))
	}
	if len(plan.Items) == 0 {
		return Plan{}, ErrInvalidPlan
	}
	plan.Digest = planDigest(plan)
	if err := s.writeJSON(s.planPath(plan.ID), plan); err != nil {
		return Plan{}, err
	}
	st := Status{PlanID: plan.ID, State: "planned"}
	for _, item := range plan.Items {
		st.Items = append(st.Items, ItemStatus{ID: item.ID, State: "planned"})
	}
	if err := s.writeJSON(s.statusPath(plan.ID), st); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (s *Service) Get(id string) (Plan, error) {
	var plan Plan
	if err := s.readJSON(s.planPath(id), &plan); err != nil {
		if os.IsNotExist(err) {
			return Plan{}, ErrPlanNotFound
		}
		return Plan{}, err
	}
	return plan, nil
}

func (s *Service) Status(id string) (Status, error) {
	var st Status
	if err := s.readJSON(s.statusPath(id), &st); err != nil {
		if os.IsNotExist(err) {
			return Status{}, ErrPlanNotFound
		}
		return Status{}, err
	}
	localizeStatusErrors(&st)
	return st, nil
}

func localizeStatusErrors(st *Status) {
	if st == nil {
		return
	}
	for i := range st.Items {
		st.Items[i].Error = localizeStoredError(st.Items[i].Error)
	}
}

func localizeStoredError(stored string) string {
	if stored == "" {
		return ""
	}
	for _, sentinel := range []error{
		ErrInputChanged, ErrCollision, ErrUndoCollision, ErrNothingToUndo,
		ErrCrossVolume, ErrOutsideRoot, ErrPlanNotFound, ErrInvalidPlan,
		ErrApplyPartial, ErrAlreadyUndone, ErrSourceMissing, ErrUnavailable, ErrInvalidRoot, os.ErrPermission,
	} {
		if stored == sentinel.Error() {
			return UserMessage(sentinel)
		}
	}
	for _, r := range stored {
		if unicode.Is(unicode.Han, r) {
			return stored
		}
	}
	return UserMessage(errors.New(stored))
}

func (s *Service) validateItem(item Item) error {
	switch item.Action {
	case ActionMkdir:
		return s.inside(item.To)
	case ActionRename, ActionMove:
		if err := s.inside(item.From); err != nil {
			return err
		}
		if err := s.inside(item.To); err != nil {
			return err
		}
		if err := rejectCrossVolumePaths(s.abs(item.From), s.abs(item.To)); err != nil {
			return err
		}
		return s.rejectExisting(item.To)
	case ActionWrite:
		if err := s.inside(item.To); err != nil {
			return err
		}
		return s.rejectExisting(item.To)
	default:
		return ErrInvalidPlan
	}
}

func (s *Service) stampSource(item Item) Item {
	if item.From == "" {
		return item
	}
	sum, err := hashFile(s.abs(item.From))
	if err != nil {
		return item
	}
	item.SourceDigest = sum
	return item
}

func (s *Service) confineItem(item Item) error {
	if item.From != "" {
		if err := s.inside(item.From); err != nil {
			return err
		}
	}
	if item.To != "" {
		if err := s.inside(item.To); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) verifySource(item Item) error {
	if item.From == "" || item.SourceDigest == "" {
		return nil
	}
	sum, err := hashFile(s.abs(item.From))
	if err != nil || sum != item.SourceDigest {
		return ErrInputChanged
	}
	return nil
}

func hashFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Service) rejectExisting(rel string) error {
	if _, err := os.Lstat(s.abs(rel)); err == nil {
		return ErrCollision
	}
	return nil
}

func (s *Service) inside(rel string) error {
	if rel == "" || strings.ContainsAny(rel, ":\x00") || !filepath.IsLocal(rel) {
		return ErrOutsideRoot
	}
	full := filepath.Join(s.root, rel)
	if !staysInside(s.root, full) {
		return ErrOutsideRoot
	}
	resolvedRoot, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		resolvedRoot = s.root
	}
	resolved, err := evalExistingPath(full)
	if err != nil {
		return nil
	}
	if !staysInside(resolvedRoot, resolved) {
		return ErrOutsideRoot
	}
	return nil
}

func staysInside(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func evalExistingPath(path string) (string, error) {
	cur := path
	for {
		if resolved, err := resolveExisting(cur); err == nil {
			if sameFilepath(cur, path) {
				return resolved, nil
			}
			rest, err := filepath.Rel(cur, path)
			if err != nil {
				return resolved, nil
			}
			return filepath.Join(resolved, rest), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", os.ErrNotExist
		}
		cur = parent
	}
}

func resolveExisting(path string) (string, error) {
	if target, err := os.Readlink(path); err == nil {
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		return filepath.Clean(target), nil
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}
	return "", os.ErrNotExist
}

func sameFilepath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func (s *Service) abs(rel string) string { return filepath.Join(s.root, rel) }

const defaultBoundedTextBytes = 16 << 10

func (s *Service) ReadBoundedText(rel string, maxBytes int) (string, error) {
	if s == nil {
		return "", ErrUnavailable
	}
	if maxBytes <= 0 {
		maxBytes = defaultBoundedTextBytes
	}
	if err := s.inside(rel); err != nil {
		return "", err
	}
	f, err := os.Open(s.abs(rel))
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrSourceMissing
		}
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxBytes {
		data = data[:maxBytes]
	}
	return string(data), nil
}

func (s *Service) planPath(id string) string   { return filepath.Join(s.dir, id+".plan.json") }
func (s *Service) statusPath(id string) string { return filepath.Join(s.dir, id+".status.json") }

func (s *Service) writeJSON(path string, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Service) readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

func planDigest(p Plan) string {
	raw, _ := json.Marshal(p.Items)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
