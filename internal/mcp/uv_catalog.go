package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// UvVersion is the pinned astral-sh/uv release used by in-product install.
const UvVersion = "0.8.22"

// UvBundle is one digest-pinned uv zip (or a test fixture served locally).
type UvBundle struct {
	Version string
	File    string
	URLs    []string
	SHA256  string
	Bytes   int64
}

func uvBinaryNames() (uv string, uvx string) {
	if runtime.GOOS == "windows" {
		return "uv.exe", "uvx.exe"
	}
	return "uv", "uvx"
}

// ProductUvDir is where the in-product installer places uv / uvx.
func ProductUvDir() string {
	if home := strings.TrimSpace(os.Getenv("LUNITIDE_UV_HOME")); home != "" {
		return home
	}
	if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
		return filepath.Join(local, "Lunitide", "runtime", "uv")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share", "lunitide", "runtime", "uv")
}

// Runtime returns the pinned uv archive for this OS/arch. Tests override URLs
// with httptest; production verifies SHA-256 (baked or sidecar .sha256).
func Runtime() (UvBundle, error) {
	asset := ""
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "windows/amd64":
		asset = "uv-x86_64-pc-windows-msvc.zip"
	case "windows/arm64":
		asset = "uv-aarch64-pc-windows-msvc.zip"
	default:
		return UvBundle{}, fmt.Errorf("当前系统请从 https://docs.astral.sh/uv 安装 uv")
	}
	zipURL := "https://github.com/astral-sh/uv/releases/download/" + UvVersion + "/" + asset
	return UvBundle{
		Version: UvVersion,
		File:    asset,
		URLs: []string{
			zipURL,
			"https://ghfast.top/" + zipURL,
		},
	}, nil
}
