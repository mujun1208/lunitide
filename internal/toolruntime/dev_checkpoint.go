package toolruntime

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type devCPFile struct {
	rel     string
	body    []byte
	missing bool
}

// boundDevRoot is the project directory or the full-access workspace.
// The per-session sandbox is not a bound repo.
func (r *Runtime) SetSessionCodeRoot(session, root string) error {
	pinned, ok := pinExistingDir(root)
	if !ok {
		return errors.New("code root is not a directory")
	}
	r.codeRootMu.Lock()
	defer r.codeRootMu.Unlock()
	if r.codeRoots == nil {
		r.codeRoots = map[string]string{}
	}
	r.codeRoots[session] = pinned
	return nil
}

func (r *Runtime) sessionCodeRoot(session string) (string, bool) {
	r.codeRootMu.Lock()
	root := r.codeRoots[session]
	r.codeRootMu.Unlock()
	if root == "" {
		return "", false
	}
	if pinned, ok := pinExistingDir(root); ok {
		return pinned, true
	}
	return "", false
}

func (r *Runtime) boundDevRoot(mode Mode, session string) (string, bool) {
	if root, ok := r.sessionCodeRoot(session); ok {
		return root, true
	}
	if r.projectRoot != nil {
		if root, err := r.projectRoot(session); err == nil && root != "" {
			if pinned, ok := pinExistingDir(root); ok {
				return pinned, true
			}
		}
	}
	if mode == FullAccess && r.fullAccessRoot != nil {
		if root, err := r.fullAccessRoot(); err == nil && root != "" {
			if pinned, ok := pinExistingDir(root); ok {
				return pinned, true
			}
		}
	}
	return "", false
}

type editDiff struct {
	rel     string
	old     string
	updated string
	count   int
}

func formatPendingEdit(total int, files []editDiff) string {
	names := make([]string, 0, len(files))
	var diff strings.Builder
	for _, file := range files {
		names = append(names, file.rel)
		diff.WriteString(unifiedFileDiff(file.rel, file.old, file.updated))
	}
	head := fmt.Sprintf("edited %d files (%d replacement(s)): %s", len(files), total, strings.Join(names, ", "))
	if len(files) == 1 {
		head = fmt.Sprintf("edited %s (%d replacement(s))", files[0].rel, files[0].count)
	}
	if diff.Len() == 0 {
		return head
	}
	return head + "\n" + diff.String()
}

func unifiedFileDiff(name, before, after string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", name, name)
	for _, line := range strings.Split(before, "\n") {
		fmt.Fprintf(&b, "-%s\n", line)
	}
	for _, line := range strings.Split(after, "\n") {
		fmt.Fprintf(&b, "+%s\n", line)
	}
	return b.String()
}

func devFilePreimage(rel, abs string) devCPFile {
	body, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return devCPFile{rel: rel, missing: true}
		}
		return devCPFile{rel: rel}
	}
	if len(body) > maxFile {
		return devCPFile{rel: rel, body: nil, missing: false}
	}
	return devCPFile{rel: rel, body: append([]byte(nil), body...)}
}

// rememberDevCheckpoint replaces the session checkpoint with the pre-images
// of the files this call is about to change.
func (r *Runtime) rememberDevCheckpoint(session string, files map[string]devCPFile) {
	if r == nil || len(files) == 0 {
		return
	}
	r.devCPMu.Lock()
	defer r.devCPMu.Unlock()
	if r.devCP == nil {
		r.devCP = map[string]map[string]devCPFile{}
	}
	r.devCP[session] = files
}

func (r *Runtime) acceptDevCheckpoint(session string) int {
	r.devCPMu.Lock()
	n := len(r.devCP[session])
	delete(r.devCP, session)
	r.devCPMu.Unlock()
	return n
}

func (r *Runtime) restoreDevCheckpoint(session string) (int, error) {
	r.devCPMu.Lock()
	files := r.devCP[session]
	delete(r.devCP, session)
	r.devCPMu.Unlock()
	if len(files) == 0 {
		return 0, errors.New("nothing to restore")
	}
	n := 0
	for abs, f := range files {
		if f.missing || f.body == nil && f.missing {
			if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
				return n, err
			}
			n++
			continue
		}
		if f.body == nil {
			return n, errors.New("checkpoint missing bytes for " + f.rel)
		}
		if err := writeFileReplace(abs, string(f.body)); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
