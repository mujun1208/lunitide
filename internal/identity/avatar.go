package identity

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/jpeg"
	_ "image/gif"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const avatarPixels = 96

var ErrAvatarUnreadable = errors.New("图片无法读取")

func normalizeAvatar(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if strings.HasPrefix(raw, "data:image/") {
		if len(raw) <= maxAvatar && strings.Contains(raw, ";base64,") {
			if _, err := decodeDataURL(raw); err != nil {
				return "", err
			}
			return raw, nil
		}
		data, err := decodeDataURL(raw)
		if err != nil {
			return "", err
		}
		return shrinkAvatarBytes(data)
	}
	if looksLikeAvatarPath(raw) {
		data, err := os.ReadFile(raw)
		if err != nil {
			return "", ErrAvatarUnreadable
		}
		return shrinkAvatarBytes(data)
	}
	if len(raw) <= maxAvatar {
		return raw, nil
	}
	return "", ErrInvalidProfile
}

func looksLikeAvatarPath(raw string) bool {
	if utf8.RuneCountInString(raw) > 1024 {
		return false
	}
	if strings.ContainsAny(raw, "\n\r") {
		return false
	}
	if filepath.IsAbs(raw) {
		return true
	}
	if len(raw) >= 3 && raw[1] == ':' && (raw[2] == '\\' || raw[2] == '/') {
		return true
	}
	return strings.ContainsAny(raw, `/\`)
}

func decodeDataURL(raw string) ([]byte, error) {
	i := strings.Index(raw, ",")
	if i < 0 {
		return nil, ErrAvatarUnreadable
	}
	payload := strings.TrimSpace(raw[i+1:])
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(payload)
	}
	if err != nil || len(data) == 0 {
		return nil, ErrAvatarUnreadable
	}
	return data, nil
}

func shrinkAvatarBytes(data []byte) (string, error) {
	if len(data) == 0 {
		return "", ErrAvatarUnreadable
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", ErrAvatarUnreadable
	}
	dst := scaleAvatar(img)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 82}); err != nil {
		return "", ErrAvatarUnreadable
	}
	out := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	if len(out) > maxAvatar {
		buf.Reset()
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 55}); err != nil {
			return "", ErrInvalidProfile
		}
		out = "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	}
	if len(out) > maxAvatar {
		return "", ErrInvalidProfile
	}
	return out, nil
}

func scaleAvatar(src image.Image) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, avatarPixels, avatarPixels))
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw < 1 {
		sw = 1
	}
	if sh < 1 {
		sh = 1
	}
	for y := 0; y < avatarPixels; y++ {
		for x := 0; x < avatarPixels; x++ {
			sx := b.Min.X + x*sw/avatarPixels
			sy := b.Min.Y + y*sh/avatarPixels
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}
