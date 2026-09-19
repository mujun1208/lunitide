package desktopfiles

import "testing"

func TestMediaFilterIncludesAudioAndVideo(t *testing.T) {
	for _, ext := range []string{".mp3", ".wav", ".flac", ".m4a", ".mp4", ".webm", ".mkv", ".mov"} {
		if !containsExt(MediaFilterPS, ext) {
			t.Fatalf("PowerShell media filter missing %s", ext)
		}
		if !containsExt(MediaFilterNative, ext) {
			t.Fatalf("native media filter missing %s", ext)
		}
	}
}

func containsExt(filter, ext string) bool {
	return len(filter) > 0 && (len(ext) == 0 || (len(filter) >= len(ext) && (func() bool {
		needle := "*" + ext
		return indexOf(filter, needle) >= 0
	})()))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
