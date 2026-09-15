package ocrapp

import (
	"os"
	"path/filepath"
	"strings"
)

type PackStatus struct {
	Available bool
	Status    string
	Backend   string
}

func packMarkerReady(root string) bool {
	root = strings.TrimSpace(root)
	if root == "" {
		return false
	}
	for _, name := range []string{
		filepath.Join(root, "ppocr.exe"),
		filepath.Join(root, "paddleocr.exe"),
		filepath.Join(root, "onnx", "ppocr.onnx"),
		filepath.Join(root, "ppocr.onnx"),
	} {
		info, err := os.Stat(name)
		if err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func DetectPPOcrPack(root string) PackStatus {
	root = strings.TrimSpace(root)
	if root == "" {
		return PackStatus{Status: "missing_dependency", Backend: "ppocr-pack"}
	}
	if packMarkerReady(root) {
		return PackStatus{Available: true, Status: "ready", Backend: "ppocr-pack"}
	}
	for _, name := range []string{
		filepath.Join(root, "ppocr"),
		filepath.Join(root, "paddleocr"),
	} {
		if packMarkerReady(name) {
			return PackStatus{Available: true, Status: "ready", Backend: "ppocr-pack"}
		}
	}
	return PackStatus{Status: "missing_dependency", Backend: "ppocr-pack"}
}

func ResolvePPOcrRoot(stored string) string {
	if root := strings.TrimSpace(stored); root != "" {
		return root
	}
	return strings.TrimSpace(os.Getenv("LUNITIDE_PPOCR_ROOT"))
}
