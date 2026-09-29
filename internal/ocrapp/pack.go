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

var packExecutables = []string{
	"RapidOCR-json.exe",
	"RapidOCR_json.exe",
	"ppocr.exe",
	"paddleocr.exe",
}

func packMarkerReady(root string) bool {
	root = strings.TrimSpace(root)
	if root == "" {
		return false
	}
	for _, name := range packExecutables {
		info, err := os.Stat(filepath.Join(root, name))
		if err == nil && !info.IsDir() {
			return true
		}
	}
	for _, name := range []string{
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
	return detectPPOcrPackDepth(root, 0)
}

const packSearchDepth = 4

func detectPPOcrPackDepth(root string, depth int) PackStatus {
	root = strings.TrimSpace(root)
	if root == "" {
		return PackStatus{Status: "missing_dependency", Backend: "ppocr-pack"}
	}
	if packMarkerReady(root) {
		return PackStatus{Available: true, Status: "ready", Backend: "ppocr-pack"}
	}
	if depth >= packSearchDepth {
		return PackStatus{Status: "missing_dependency", Backend: "ppocr-pack"}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return PackStatus{Status: "missing_dependency", Backend: "ppocr-pack"}
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if got := detectPPOcrPackDepth(filepath.Join(root, entry.Name()), depth+1); got.Available {
			return got
		}
	}
	return PackStatus{Status: "missing_dependency", Backend: "ppocr-pack"}
}

func FindPackExecutable(root string) string {
	return findPackExecutableDepth(root, 0)
}

func findPackExecutableDepth(root string, depth int) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	if exe := findPackExecutableHere(root); exe != "" {
		return exe
	}
	if depth >= packSearchDepth {
		return ""
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if exe := findPackExecutableDepth(filepath.Join(root, entry.Name()), depth+1); exe != "" {
			return exe
		}
	}
	return ""
}

func findPackExecutableHere(root string) string {
	for _, name := range packExecutables {
		path := filepath.Join(root, name)
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func ResolvePPOcrRoot(stored string) string {
	if root := strings.TrimSpace(stored); root != "" {
		return root
	}
	return strings.TrimSpace(os.Getenv("LUNITIDE_PPOCR_ROOT"))
}

func EffectiveLocalEngine(localEngine string, pack PackStatus) string {
	switch strings.TrimSpace(localEngine) {
	case "windows-ocr":
		return "windows-ocr"
	case "ppocr":
		if pack.Available {
			return "ppocr"
		}
		return "windows-ocr"
	default:
		if pack.Available {
			return "ppocr"
		}
		return "windows-ocr"
	}
}
