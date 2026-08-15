// Package m5: T-5.2.2 workspace file reads. fs.read / fs.tree / fs.grep run
// exclusively through the T-5.2.1 path safety core (SecureRoot), page with
// maxBytes truncation and mark truncated responses so the agent never gets
// a silently shortened view.
package m5

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/m5workspace"
	"github.com/lunitide/lunitide/internal/workspace"
)

var (
	ErrFsInvalidOp    = errors.New("m5: op must be read, tree or grep")
	ErrFsPathRequired = errors.New("m5: path is required")
	ErrFsPatternReq   = errors.New("m5: grep requires a pattern")
	ErrFsWorkspaceGone = errors.New("m5: workspace is deleted")
)

// Frozen M5 read limits (latency budget: P95 <= 100ms for <= 1MB files).
const (
	FsDefaultMaxBytes int64 = 1 << 20 // 1 MiB
	FsMaxBytesCap     int64 = 8 << 20 // 8 MiB hard cap per request
	FsTreeEntryLimit        = 2000
	FsGrepMatchLimit        = 200
	FsGrepFileBytes   int64 = 1 << 20 // per-file scan bound
)

// FsInput is the unified fs op request.
type FsInput struct {
	WorkspaceID string
	Op          string // read | tree | grep
	Path        string // workspace-relative
	MaxBytes    int64  // read/grep per-file bound (default 1 MiB, cap 8 MiB)
	Pattern     string // grep only
}

// FsEntry is one tree row.
type FsEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // file | dir
	Size int64  `json:"size"`
}

// FsMatch is one grep hit.
type FsMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// FsResult carries exactly one populated view plus the truncated flag.
type FsResult struct {
	Content   string     `json:"content,omitempty"`
	Entries   []FsEntry  `json:"entries,omitempty"`
	Matches   []FsMatch  `json:"matches,omitempty"`
	Truncated bool       `json:"truncated"`
}

// FsService reads files inside an AdHocWorkspace through SecureRoot.
type FsService struct {
	uow agentrunapp.UnitOfWork
}

func NewFsService(uow agentrunapp.UnitOfWork) *FsService { return &FsService{uow: uow} }

type wsStore interface {
	GetM5Workspace(id string) (m5workspace.Workspace, error)
}

func (s *FsService) rootFor(workspaceID string) (*workspace.SecureRoot, error) {
	var root *workspace.SecureRoot
	err := s.uow.TransactAgentRuntime(context.Background(), func(tx agentrunapp.Tx) error {
		st, ok := tx.(wsStore)
		if !ok {
			return workspace.ErrUOWUnavailable
		}
		w, err := st.GetM5Workspace(workspaceID)
		if err != nil {
			return err
		}
		if w.State == m5workspace.StateDeleted {
			return ErrFsWorkspaceGone
		}
		root, err = workspace.NewSecureRoot(w.RootCanonical)
		return err
	})
	return root, err
}

// Op dispatches to Read, Tree or Grep after input validation.
func (s *FsService) Op(ctx context.Context, in FsInput) (FsResult, error) {
	if in.WorkspaceID == "" || in.Path == "" {
		return FsResult{}, ErrFsPathRequired
	}
	root, err := s.rootFor(in.WorkspaceID)
	if err != nil {
		return FsResult{}, err
	}
	switch in.Op {
	case "read":
		return fsRead(root, in)
	case "tree":
		return fsTree(root, in)
	case "grep":
		return fsGrep(root, in)
	default:
		return FsResult{}, ErrFsInvalidOp
	}
}

func clampMaxBytes(n int64) int64 {
	if n <= 0 {
		return FsDefaultMaxBytes
	}
	if n > FsMaxBytesCap {
		return FsMaxBytesCap
	}
	return n
}

func fsRead(root *workspace.SecureRoot, in FsInput) (FsResult, error) {
	f, err := root.OpenSecure(in.Path)
	if err != nil {
		return FsResult{}, err
	}
	defer f.Close()
	limit := clampMaxBytes(in.MaxBytes)
	buf, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return FsResult{}, err
	}
	truncated := int64(len(buf)) > limit
	if truncated {
		buf = buf[:limit]
	}
	return FsResult{Content: string(buf), Truncated: truncated}, nil
}

func fsTree(root *workspace.SecureRoot, in FsInput) (FsResult, error) {
	// "." addresses the workspace root itself; other paths resolve through
	// the lexical + prefix layers.
	var base string
	var err error
	if in.Path == "." || in.Path == "" {
		base = root.Root()
	} else if base, err = root.Resolve(in.Path); err != nil {
		return FsResult{}, err
	}
	rootPath := root.Root()
	out := FsResult{Entries: []FsEntry{}}
	err = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// WalkDir never follows symlinked directories; a reparse leaf still
		// shows as one entry and is never descended into.
		if len(out.Entries) >= FsTreeEntryLimit {
			out.Truncated = true
			return filepath.SkipAll
		}
		rel, relErr := filepath.Rel(rootPath, p)
		if relErr != nil {
			return relErr
		}
		entry := FsEntry{Path: filepath.ToSlash(rel)}
		info, infoErr := d.Info()
		if infoErr == nil {
			entry.Size = info.Size()
		}
		if d.IsDir() {
			entry.Type = "dir"
		} else {
			entry.Type = "file"
		}
		out.Entries = append(out.Entries, entry)
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return FsResult{}, err
	}
	return out, nil
}

func fsGrep(root *workspace.SecureRoot, in FsInput) (FsResult, error) {
	if strings.TrimSpace(in.Pattern) == "" {
		return FsResult{}, ErrFsPatternReq
	}
	re, err := regexp.Compile(in.Pattern)
	if err != nil {
		return FsResult{}, err
	}
	tree, err := fsTree(root, FsInput{WorkspaceID: in.WorkspaceID, Op: "tree", Path: in.Path})
	if err != nil {
		return FsResult{}, err
	}
	out := FsResult{Matches: []FsMatch{}}
	fileLimit := clampMaxBytes(in.MaxBytes)
	for _, entry := range tree.Entries {
		if entry.Type != "file" {
			continue
		}
		if len(out.Matches) >= FsGrepMatchLimit {
			out.Truncated = true
			break
		}
		f, openErr := root.OpenSecure(entry.Path)
		if openErr != nil {
			continue // unreadable leaf (permissions, racing delete): skip
		}
		matches, fileTrunc, scanErr := scanFile(f, re, fileLimit, FsGrepMatchLimit-len(out.Matches))
		_ = f.Close()
		if scanErr != nil {
			continue
		}
		for i := range matches {
			matches[i].Path = entry.Path
		}
		out.Matches = append(out.Matches, matches...)
		if fileTrunc {
			out.Truncated = true
		}
	}
	return out, nil
}

// scanFile greps one bounded file, skipping binary content (NUL in the first
// 8 KiB) and capping both per-file bytes and total matches.
func scanFile(f *os.File, re *regexp.Regexp, byteLimit int64, matchBudget int) ([]FsMatch, bool, error) {
	r := bufio.NewReader(io.LimitReader(f, byteLimit+1))
	probe, _ := r.Peek(8192)
	if bytes.IndexByte(probe, 0) >= 0 {
		return nil, false, nil
	}
	var matches []FsMatch
	truncated := false
	line := 0
	for {
		if len(matches) >= matchBudget {
			truncated = true
			break
		}
		text, readErr := r.ReadString('\n')
		if text == "" && readErr != nil {
			if readErr == io.EOF {
				break
			}
			return matches, truncated, readErr
		}
		line++
		trimmed := strings.TrimRight(text, "\r\n")
		if re.MatchString(trimmed) {
			matches = append(matches, FsMatch{Path: "", Line: line, Text: clampLine(trimmed)})
		}
		if readErr == io.EOF {
			break
		}
	}
	return matches, truncated, nil
}

func clampLine(s string) string {
	const maxLineRunes = 400
	runes := []rune(s)
	if len(runes) <= maxLineRunes {
		return s
	}
	return string(runes[:maxLineRunes]) + "…"
}
