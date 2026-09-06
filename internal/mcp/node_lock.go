package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type LaunchLock struct {
	Args       []string `json:"args"`
	Digest     string   `json:"digest"`
	SourceArgs string   `json:"sourceArgs"`
}

// NodeScriptDigest binds a local script entry point and its arguments before
// launching it. Package dependencies remain the Node project's lockfile concern;
// they are not claimed to be covered by this entry-point digest.
func NodeScriptDigest(ctx context.Context, args []string, workDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", ErrLaunchLock
	}
	path := args[0]
	if !filepath.IsAbs(path) {
		path = filepath.Join(workDir, path)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return "", ErrLaunchLock
	}
	data, err := io.ReadAll(io.LimitReader(file, (8<<20)+1))
	if err != nil {
		return "", err
	}
	if len(data) > 8<<20 {
		return "", ErrLaunchLock
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	canonical, _ := json.Marshal(struct {
		Path string
		Args []string
		Body []byte
	}{filepath.Clean(path), args, data})
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
