package mediaapp

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
)

var (
	ErrProtectedRemoteMedia   = errors.New("MEDIA_PROTECTED_CONTENT")
	ErrMediaAssetUnauthorized = errors.New("MEDIA_ASSET_UNAUTHORIZED")
)

// AuthorizeLocalAsset rejects remote URLs, UNC, ADS, relative paths, and
// unknown source kinds. Owned playback only registers explicit local files.
func AuthorizeLocalAsset(sourceKind, path, kind string) error {
	switch sourceKind {
	case "user_selected", "artifact", "workspace":
	default:
		return ErrMediaAssetUnauthorized
	}
	if kind != "audio" && kind != "video" {
		return ErrMediaAssetUnauthorized
	}
	raw := strings.TrimSpace(path)
	if raw == "" {
		return ErrMediaAssetUnauthorized
	}
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "://") || strings.HasPrefix(lower, "file:") || strings.HasPrefix(lower, "javascript:") {
		return ErrProtectedRemoteMedia
	}
	if strings.ContainsAny(raw, "<>|\n\r") || strings.Contains(raw, "..") {
		return ErrMediaAssetUnauthorized
	}
	if strings.HasPrefix(raw, `\\`) || strings.HasPrefix(raw, "//") {
		return ErrMediaAssetUnauthorized
	}
	if runtime.GOOS == "windows" {
		colon := strings.Index(raw, ":")
		if colon >= 0 && strings.Contains(raw[colon+1:], ":") {
			return ErrMediaAssetUnauthorized
		}
	}
	if !filepath.IsAbs(raw) {
		return ErrMediaAssetUnauthorized
	}
	for _, r := range raw {
		if r == 0 || unicode.IsControl(r) {
			return ErrMediaAssetUnauthorized
		}
	}
	return nil
}
