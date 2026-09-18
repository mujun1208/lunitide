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

var rapidOCRExecutables = []string{
	"RapidOCR-json.exe",
	"RapidOCR_json.exe",
}

func packRapidOCRReady(root string) bool {
	root = strings.TrimSpace(root)
	if root == "" {
		return false
	}
	for _, name := range rapidOCRExecutables {
		info, err := os.Stat(filepath.Join(root, name))
		if err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func packUnwiredMarker(root string) bool {
	root = strings.TrimSpace(root)
	if root == "" {
		return false
	}
	for _, name := range []string{"ppocr.exe", "paddleocr.exe"} {
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
	if packRapidOCRReady(root) {
		return PackStatus{Available: true, Status: "ready", Backend: "ppocr-pack"}
	}
	unwired := packUnwiredMarker(root)
	if unwired {
		return PackStatus{Status: "registered_unwired", Backend: "ppocr-pack"}
	}
	if depth >= packSearchDepth {
		if unwired {
			return PackStatus{Status: "registered_unwired", Backend: "ppocr-pack"}
		}
		return PackStatus{Status: "missing_dependency", Backend: "ppocr-pack"}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if unwired {
			return PackStatus{Status: "registered_unwired", Backend: "ppocr-pack"}
		}
		return PackStatus{Status: "missing_dependency", Backend: "ppocr-pack"}
	}
	foundUnwired := unwired
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		got := detectPPOcrPackDepth(filepath.Join(root, entry.Name()), depth+1)
		if got.Available {
			return got
		}
		if got.Status == "registered_unwired" {
			foundUnwired = true
		}
	}
	if foundUnwired {
		return PackStatus{Status: "registered_unwired", Backend: "ppocr-pack"}
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
	for _, name := range rapidOCRExecutables {
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
	if strings.TrimSpace(localEngine) == "windows-ocr" {
		return "windows-ocr"
	}
	if pack.Available {
		return "ppocr"
	}
	return "windows-ocr"
}
