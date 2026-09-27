package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type openPageFileKey struct{}

// WithOpenPageFile marks the local file the user is looking at. Read and
// edit of that exact file are allowed for this turn.
func WithOpenPageFile(ctx context.Context, path string) context.Context {
	path = strings.TrimSpace(path)
	if ctx == nil || path == "" {
		return ctx
	}
	return context.WithValue(ctx, openPageFileKey{}, path)
}

func openPageFile(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	path, _ := ctx.Value(openPageFileKey{}).(string)
	return path
}

func sameOpenPage(requested, allow string) bool {
	requested = strings.TrimSpace(requested)
	allow = strings.TrimSpace(allow)
	if requested == "" || allow == "" || strings.Contains(requested, "..") || strings.Contains(allow, "..") {
		return false
	}
	return strings.EqualFold(filepath.Clean(requested), filepath.Clean(allow))
}

func openPageTarget(ctx context.Context, requested string) (string, bool) {
	allow := openPageFile(ctx)
	if !sameOpenPage(requested, allow) {
		return "", false
	}
	clean := filepath.Clean(allow)
	info, err := os.Stat(clean)
	if err != nil || info.IsDir() {
		return "", false
	}
	return clean, true
}

func (r *Runtime) resolveWorkspacePath(ctx context.Context, mode Mode, session, rel string, write, unconfined bool) (string, error) {
	if p, ok := openPageTarget(ctx, rel); ok {
		return p, nil
	}
	return r.path(mode, session, rel, write, unconfined)
}

func toolEditsOpenPage(ctx context.Context, name string, args json.RawMessage) bool {
	if name != "workspace.write" && name != "workspace.edit" {
		return false
	}
	allow := openPageFile(ctx)
	if allow == "" {
		return false
	}
	var body struct {
		Path  string `json:"path"`
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if json.Unmarshal(args, &body) != nil {
		return false
	}
	paths := make([]string, 0, 1+len(body.Files))
	if strings.TrimSpace(body.Path) != "" {
		paths = append(paths, body.Path)
	}
	for _, file := range body.Files {
		if strings.TrimSpace(file.Path) != "" {
			paths = append(paths, file.Path)
		}
	}
	if len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		if !sameOpenPage(path, allow) {
			return false
		}
	}
	return true
}
