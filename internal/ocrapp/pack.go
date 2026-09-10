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

func DetectPPOcrPack(root string) PackStatus {
	if strings.TrimSpace(root) == "" {
		return PackStatus{Status: "missing_dependency", Backend: "ppocr-pack"}
	}
	for _, name := range []string{
		filepath.Join(root, "ppocr"),
		filepath.Join(root, "paddleocr"),
		filepath.Join(root, "onnx", "ppocr.onnx"),
	} {
		if _, err := os.Stat(name); err == nil {
			return PackStatus{Available: true, Status: "ready", Backend: "ppocr-pack"}
		}
	}
	return PackStatus{Status: "missing_dependency", Backend: "ppocr-pack"}
}
