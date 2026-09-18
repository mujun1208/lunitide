package mediaapp

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"
)

func TestProtectedRemoteMediaRejected(t *testing.T) {
	for _, path := range []string{
		"https://music.example/track.mp3",
		"http://cdn.example/song.mp4",
		"file:///C:/secret.mp3",
		"javascript:alert(1)",
	} {
		if err := AuthorizeLocalAsset("user_selected", path, "audio"); !errors.Is(err, ErrProtectedRemoteMedia) {
			t.Fatalf("%q: %v", path, err)
		}
	}
}

func TestMediaAssetAuthorization(t *testing.T) {
	if err := AuthorizeLocalAsset("torrent", `C:\song.mp3`, "audio"); !errors.Is(err, ErrMediaAssetUnauthorized) {
		t.Fatalf("unknown source: %v", err)
	}
	if err := AuthorizeLocalAsset("user_selected", `\\nas\share\song.mp3`, "audio"); !errors.Is(err, ErrMediaAssetUnauthorized) {
		t.Fatalf("unc: %v", err)
	}
	if err := AuthorizeLocalAsset("user_selected", "relative.mp3", "audio"); !errors.Is(err, ErrMediaAssetUnauthorized) {
		t.Fatalf("relative: %v", err)
	}
	if runtime.GOOS == "windows" {
		if err := AuthorizeLocalAsset("user_selected", `C:\song.mp3:hidden`, "audio"); !errors.Is(err, ErrMediaAssetUnauthorized) {
			t.Fatalf("ads: %v", err)
		}
		if err := AuthorizeLocalAsset("user_selected", `C:\Users\me\song.mp3`, "audio"); err != nil {
			t.Fatalf("abs windows: %v", err)
		}
		return
	}
	path := filepath.Join(string(filepath.Separator), "tmp", "song.mp3")
	if err := AuthorizeLocalAsset("workspace", path, "audio"); err != nil {
		t.Fatalf("abs unix: %v", err)
	}
}
