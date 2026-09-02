package toolruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// todoItem is one checklist entry persisted per session.
type todoItem struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

const (
	todoMax        = 50
	todoMaxContent = 500
)

// writeTodos validates and atomically persists one full checklist for
// the session (stored outside the session workspace so it never disturbs
// the approval workspace digest) and answers the rendered checklist.
func (r *Runtime) writeTodos(session string, todos []struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}) (string, error) {
	if len(todos) > todoMax {
		return "", errors.New("too many todos (max 50)")
	}
	items := make([]todoItem, 0, len(todos))
	inProgress := 0
	for _, t := range todos {
		content := strings.TrimSpace(t.Content)
		if content == "" || len(content) > todoMaxContent {
			return "", errors.New("todo content must be 1-500 chars")
		}
		status := t.Status
		if status == "" {
			status = "pending"
		}
		if status != "pending" && status != "in_progress" && status != "completed" {
			return "", errors.New("todo status must be pending|in_progress|completed")
		}
		if status == "in_progress" {
			inProgress++
		}
		priority := t.Priority
		if priority == "" {
			priority = "medium"
		}
		if priority != "high" && priority != "medium" && priority != "low" {
			return "", errors.New("todo priority must be high|medium|low")
		}
		items = append(items, todoItem{Content: content, Status: status, Priority: priority})
	}
	if inProgress > 1 {
		return "", errors.New("only one todo may be in_progress at a time")
	}
	dir := filepath.Join(r.root, ".todos")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	target := filepath.Join(dir, session+".json")
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return "", err
	}
	_ = os.Remove(target)
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d todo(s) stored", len(items))
	for i, t := range items {
		mark := " "
		if t.Status == "completed" {
			mark = "x"
		}
		fmt.Fprintf(&b, "\n%d. [%s] (%s|%s) %s", i+1, mark, t.Status, t.Priority, t.Content)
	}
	return b.String(), nil
}

type editHunk struct {
	OldText    string
	NewText    string
	ReplaceAll bool
}

type workspaceEditFile struct {
	Path  string
	Hunks []editHunk
}

type workspaceEditHunkJSON struct {
	OldText    string `json:"oldText"`
	NewText    string `json:"newText"`
	ReplaceAll bool   `json:"replaceAll"`
}

func hunksFromJSON(oldText, newText string, replaceAll bool, edits []workspaceEditHunkJSON) ([]editHunk, error) {
	if len(edits) > 0 {
		if len(edits) > 20 {
			return nil, errors.New("invalid arguments")
		}
		hunks := make([]editHunk, 0, len(edits))
		for _, h := range edits {
			hunks = append(hunks, editHunk(h))
		}
		return hunks, nil
	}
	return []editHunk{{OldText: oldText, NewText: newText, ReplaceAll: replaceAll}}, nil
}

func parseWorkspaceEditArgs(args json.RawMessage) ([]workspaceEditFile, error) {
	var a struct {
		Path       string                  `json:"path"`
		OldText    string                  `json:"oldText"`
		NewText    string                  `json:"newText"`
		ReplaceAll bool                    `json:"replaceAll"`
		Edits      []workspaceEditHunkJSON `json:"edits"`
		Files      []struct {
			Path       string                  `json:"path"`
			OldText    string                  `json:"oldText"`
			NewText    string                  `json:"newText"`
			ReplaceAll bool                    `json:"replaceAll"`
			Edits      []workspaceEditHunkJSON `json:"edits"`
		} `json:"files"`
	}
	if strict(args, &a) != nil {
		return nil, errors.New("invalid arguments")
	}
	if len(a.Files) > 0 {
		if len(a.Files) > 8 {
			return nil, errors.New("invalid arguments")
		}
		out := make([]workspaceEditFile, 0, len(a.Files))
		seen := map[string]bool{}
		for _, f := range a.Files {
			if strings.TrimSpace(f.Path) == "" {
				return nil, errors.New("invalid arguments")
			}
			if seen[f.Path] {
				return nil, errors.New("invalid arguments")
			}
			seen[f.Path] = true
			hunks, err := hunksFromJSON(f.OldText, f.NewText, f.ReplaceAll, f.Edits)
			if err != nil {
				return nil, err
			}
			out = append(out, workspaceEditFile{Path: f.Path, Hunks: hunks})
		}
		return out, nil
	}
	if strings.TrimSpace(a.Path) == "" {
		return nil, errors.New("invalid arguments")
	}
	hunks, err := hunksFromJSON(a.OldText, a.NewText, a.ReplaceAll, a.Edits)
	if err != nil {
		return nil, err
	}
	return []workspaceEditFile{{Path: a.Path, Hunks: hunks}}, nil
}

func writeFileReplace(path, content string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".edit-*")
	if err != nil {
		return err
	}
	tn := tmp.Name()
	defer os.Remove(tn)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.WriteString(content)
	}
	if err == nil {
		err = tmp.Sync()
	}
	ce := tmp.Close()
	if err == nil {
		err = ce
	}
	if err == nil {
		_ = os.Remove(path)
		err = os.Rename(tn, path)
	}
	return err
}

func applyWorkspaceHunks(content string, hunks []editHunk) (string, int, error) {
	if len(hunks) == 0 {
		return "", 0, errors.New("invalid arguments")
	}
	updated := content
	total := 0
	for i, h := range hunks {
		if h.OldText == "" || len(h.OldText) > maxFile || len(h.NewText) > maxFile {
			return "", 0, errors.New("invalid arguments")
		}
		count := strings.Count(updated, h.OldText)
		if count == 0 {
			return "", 0, fmt.Errorf("oldText not found in file (hunk %d)", i+1)
		}
		if count > 1 && !h.ReplaceAll {
			return "", 0, fmt.Errorf("oldText found %d times; set replaceAll=true or narrow the anchor", count)
		}
		if h.ReplaceAll {
			updated = strings.ReplaceAll(updated, h.OldText, h.NewText)
			total += count
		} else {
			updated = strings.Replace(updated, h.OldText, h.NewText, 1)
			total++
		}
	}
	return updated, total, nil
}
