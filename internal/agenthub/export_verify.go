package agenthub

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	content "github.com/lunitide/lunitide/internal/officestudio"
)

func verifyThreadExports(store *ThreadStore, thread ThreadRecord) {
	_ = filepath.WalkDir(thread.WorkspaceRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if path != thread.WorkspaceRoot && (skipScanDir(d.Name()) || d.Type()&os.ModeSymlink != 0) {
				return fs.SkipDir
			}
			return nil
		}
		reason := exportArtifactReason(thread.WorkspaceRoot, thread.ExportDir, path, d)
		if reason == "" {
			return nil
		}
		rel := slashRel(thread.WorkspaceRoot, path)
		_ = insertThreadEvent(store, thread.ID, AgentEvent{
			Type:   "artifact_unverified",
			Title:  reason,
			Detail: reason + ": " + rel,
		})
		return nil
	})
}

func exportArtifactReason(workspace, exportDir, path string, d fs.DirEntry) string {
	if d.Type()&os.ModeSymlink != 0 {
		return ""
	}
	info, err := d.Info()
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	ext := strings.ToLower(filepath.Ext(d.Name()))
	kind := officeKindForExport(ext)
	if kind == "" {
		return ""
	}
	if !PathAllowed(workspace, exportDir, path) {
		return "path_escape"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "missing_file"
		}
		return "missing_file"
	}
	if _, err = content.Inspect(kind, data); err != nil {
		return "inspect_failed"
	}
	return "usage_unknown"
}

func officeKindForExport(ext string) content.Kind {
	switch ext {
	case ".docx":
		return content.DOCX
	case ".pptx":
		return content.PPTX
	case ".xlsx":
		return content.XLSX
	case ".pdf":
		return content.PDF
	default:
		return ""
	}
}
