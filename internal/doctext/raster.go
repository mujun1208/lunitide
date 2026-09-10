package doctext

import (
	"bytes"
	"path/filepath"
	"strings"
)

// LooksLikeRasterImage reports whether the input is a still image that may
// need OCR. It does not claim the bytes are a well-formed picture.
func LooksLikeRasterImage(name, media string, raw []byte) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff":
		return true
	}
	if strings.HasPrefix(strings.ToLower(media), "image/") && !strings.Contains(strings.ToLower(media), "svg") {
		return true
	}
	return bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n")) ||
		bytes.HasPrefix(raw, []byte("\xff\xd8\xff")) ||
		bytes.HasPrefix(raw, []byte("GIF8")) ||
		bytes.HasPrefix(raw, []byte("BM")) ||
		(len(raw) >= 12 && bytes.HasPrefix(raw, []byte("RIFF")) && bytes.Equal(raw[8:12], []byte("WEBP")))
}
