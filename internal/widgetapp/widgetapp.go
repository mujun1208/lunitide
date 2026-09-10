package widgetapp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidSpec = errors.New("widget spec rejected")
	ErrForbidden   = errors.New("widget owner or binding rejected")
	ErrUnknown     = errors.New("widget action unknown")
	ErrConflict    = errors.New("widget revision conflict")
)

type WidgetState struct {
	Seconds   int    `json:"seconds,omitempty"`
	Running   bool   `json:"running,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	Value     string `json:"value,omitempty"`
	Unit      string `json:"unit,omitempty"`
	Percent   int    `json:"percent,omitempty"`
	Rows      string `json:"rows,omitempty"`
	Checked   string `json:"checked,omitempty"`
	Items     string `json:"items,omitempty"`
}

type Widget struct {
	ID    string      `json:"id"`
	Kind  string      `json:"kind"`
	State WidgetState `json:"state,omitempty"`
}

type Action struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

type Spec struct {
	ID       string   `json:"id"`
	Revision int      `json:"revision"`
	Owner    string   `json:"owner"`
	Title    string   `json:"title"`
	Layout   string   `json:"layout,omitempty"`
	Status   string   `json:"status"`
	Widgets  []Widget `json:"widgets,omitempty"`
	Actions  []Action `json:"actions,omitempty"`
}

type Item struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Title      string `json:"title"`
	SourceRef  string `json:"sourceRef,omitempty"`
	DueAt      string `json:"dueAt,omitempty"`
	Timezone   string `json:"timezone,omitempty"`
	RepeatRule string `json:"repeatRule,omitempty"`
	Status     string `json:"status"`
	Amount     string `json:"amount,omitempty"`
	Currency   string `json:"currency,omitempty"`
	UpdatedAt  string `json:"updatedAt"`
}

type snapshot struct {
	Specs      map[string]Spec `json:"specs"`
	Items      map[string]Item `json:"items"`
	Dispatches map[string]int  `json:"dispatches"`
}

type FileStore struct {
	path string
	mu   sync.Mutex
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func allowedWidget(kind string) bool {
	switch kind {
	case "timer", "checklist", "metric", "table", "bar", "progress", "todo", "checkin":
		return true
	default:
		return false
	}
}

func validWidgetState(st WidgetState) bool {
	if st.Seconds < 0 || st.Seconds > 86400 || st.Percent < 0 || st.Percent > 100 {
		return false
	}
	return len(st.UpdatedAt) <= 40 && len(st.Value) <= 64 && len(st.Unit) <= 16 && len(st.Rows) <= 500 && len(st.Checked) <= 500 && len(st.Items) <= 500
}

func allowedAction(kind string) bool {
	switch kind {
	case "local.toggle", "local.complete", "item.archive":
		return true
	default:
		return false
	}
}

func allowedItemType(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "subscription", "bill", "id", "checkin", "action":
		return true
	default:
		return false
	}
}

func Validate(spec Spec) error {
	if strings.TrimSpace(spec.ID) == "" || strings.TrimSpace(spec.Owner) == "" || strings.TrimSpace(spec.Title) == "" {
		return ErrInvalidSpec
	}
	for _, w := range spec.Widgets {
		if !allowedWidget(w.Kind) || !validWidgetState(w.State) {
			return ErrInvalidSpec
		}
	}
	for _, a := range spec.Actions {
		if !allowedAction(a.Kind) {
			return ErrInvalidSpec
		}
	}
	return nil
}

func (s *FileStore) load() snapshot {
	out := snapshot{Specs: map[string]Spec{}, Items: map[string]Item{}, Dispatches: map[string]int{}}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	if out.Specs == nil {
		out.Specs = map[string]Spec{}
	}
	if out.Items == nil {
		out.Items = map[string]Item{}
	}
	if out.Dispatches == nil {
		out.Dispatches = map[string]int{}
	}
	return out
}

func (s *FileStore) save(snap snapshot) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0600)
}

func (s *FileStore) Create(spec Spec) (Spec, error) {
	if err := Validate(spec); err != nil {
		return Spec{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := s.load()
	if existing, ok := snap.Specs[spec.ID]; ok && existing.Owner != spec.Owner {
		return Spec{}, ErrForbidden
	}
	spec.Revision = 1
	spec.Status = "active"
	snap.Specs[spec.ID] = spec
	return spec, s.save(snap)
}

func (s *FileStore) Update(owner string, spec Spec) (Spec, error) {
	if err := Validate(spec); err != nil {
		return Spec{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := s.load()
	cur, ok := snap.Specs[spec.ID]
	if !ok || cur.Owner != owner {
		return Spec{}, ErrForbidden
	}
	if spec.Revision != 0 && spec.Revision != cur.Revision {
		return Spec{}, ErrConflict
	}
	spec.Owner = owner
	spec.Revision = cur.Revision + 1
	if spec.Status == "" {
		spec.Status = cur.Status
	}
	snap.Specs[spec.ID] = spec
	return spec, s.save(snap)
}

func (s *FileStore) Query(owner, id string) ([]Spec, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := s.load()
	var out []Spec
	for _, spec := range snap.Specs {
		if spec.Owner != owner {
			continue
		}
		if id != "" && spec.ID != id {
			continue
		}
		out = append(out, spec)
	}
	return out, nil
}

func (s *FileStore) Archive(owner, id string, revision int) (Spec, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := s.load()
	cur, ok := snap.Specs[id]
	if !ok || cur.Owner != owner {
		return Spec{}, ErrForbidden
	}
	if revision != 0 && revision != cur.Revision {
		return Spec{}, ErrConflict
	}
	cur.Status = "archived"
	cur.Revision++
	snap.Specs[id] = cur
	return cur, s.save(snap)
}

func (s *FileStore) Dispatch(owner, widgetID, actionID string, revision int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := s.load()
	spec, ok := snap.Specs[widgetID]
	if !ok || spec.Owner != owner {
		return 0, ErrForbidden
	}
	if revision != 0 && revision != spec.Revision {
		return 0, ErrConflict
	}
	found := false
	for _, a := range spec.Actions {
		if a.ID == actionID {
			found = true
			break
		}
	}
	if !found {
		return 0, ErrUnknown
	}
	key := owner + "/" + widgetID + "/" + actionID + "/" + itoa(spec.Revision)
	if n := snap.Dispatches[key]; n > 0 {
		return n, nil
	}
	snap.Dispatches[key] = 1
	return 1, s.save(snap)
}

func (s *FileStore) UpsertItem(item Item) (Item, error) {
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Title) == "" || !allowedItemType(item.Type) {
		return Item{}, ErrInvalidSpec
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := s.load()
	if item.Status == "" {
		item.Status = "open"
	}
	item.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	snap.Items[item.ID] = item
	return item, s.save(snap)
}

func (s *FileStore) ArchiveItem(id string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := s.load()
	item, ok := snap.Items[id]
	if !ok {
		return Item{}, ErrUnknown
	}
	item.Status = "stopped"
	item.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	snap.Items[id] = item
	return item, s.save(snap)
}

func (s *FileStore) ListItems() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := s.load()
	out := make([]Item, 0, len(snap.Items))
	for _, item := range snap.Items {
		out = append(out, item)
	}
	return out
}

func (s *FileStore) HasItem(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.load().Items[id]
	return ok
}

func (s *FileStore) ItemDue(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.load().Items[id]
	if !ok || item.Status != "open" {
		return false
	}
	due := strings.TrimSpace(item.DueAt)
	if due == "" {
		return true
	}
	loc := time.UTC
	if tz := strings.TrimSpace(item.Timezone); tz != "" {
		loaded, err := time.LoadLocation(tz)
		if err != nil {
			return false
		}
		loc = loaded
	}
	at, err := time.Parse(time.RFC3339, due)
	if err != nil {
		at, err = time.Parse(time.RFC3339Nano, due)
	}
	if err != nil {
		at, err = time.ParseInLocation("2006-01-02 15:04:05", due, loc)
	}
	if err != nil {
		at, err = time.ParseInLocation("2006-01-02", due, loc)
	}
	if err != nil {
		return false
	}
	return !at.After(time.Now())
}

func itoa(n int) string {
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
