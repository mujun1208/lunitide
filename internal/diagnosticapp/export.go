package diagnosticapp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/buildinfo"
)

// ExportDiagnostics collects engine version, platform info, and optional logs
// into a JSON file in the system temp directory. Paths are redacted by default.
// Returns the absolute path to the exported diagnostics file.
func ExportDiagnostics(ctx context.Context, includeLogs bool, redactPaths bool) (bridge.Response, error) {
	diag := map[string]any{
		"version":     buildinfo.Version,
		"platform":    runtime.GOOS + "/" + runtime.GOARCH,
		"exportedAt":  time.Now().UTC().Format(time.RFC3339Nano),
		"redacted":    redactPaths,
	}
	if includeLogs {
		diag["logs"] = redactLogPaths(redactPaths)
	}

	body, err := json.MarshalIndent(diag, "", "  ")
	if err != nil {
		return bridge.Failure("", "", "DIAGNOSTICS_EXPORT_FAILED", "诊断包序列化失败", false), err
	}

	dir := os.TempDir()
	filename := fmt.Sprintf("lunitide-diagnostics-%s.json", time.Now().UTC().Format("20060102-150405"))
	fullPath := filepath.Join(dir, filename)

	if err := os.WriteFile(fullPath, body, 0600); err != nil {
		return bridge.Failure("", "", "DIAGNOSTICS_EXPORT_FAILED", "诊断包写入失败", false), err
	}

	result := struct {
		Path      string `json:"path"`
		CreatedAt string `json:"createdAt"`
		Redacted  bool   `json:"redacted"`
	}{
		Path:      fullPath,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Redacted:  true,
	}
	return bridge.Success("", result), nil
}

// redactLogPaths returns log location info with paths optionally redacted.
func redactLogPaths(redact bool) []map[string]string {
	logs := []map[string]string{
		{"source": "engine", "level": "info"},
		{"source": "host", "level": "info"},
	}
	if !redact {
		logs[0]["path"] = filepath.Join(os.TempDir(), "lunitide-engine.log")
		logs[1]["path"] = filepath.Join(os.TempDir(), "lunitide-host.log")
	}
	return logs
}
