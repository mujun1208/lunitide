package toolruntime

import (
	"errors"
	"path/filepath"
	"strings"
)

func audioExtOK(ext string) bool {
	switch strings.ToLower(ext) {
	case ".wav", ".mp3":
		return true
	}
	return false
}

func looksLikeWAV(data []byte) bool {
	return len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE"
}

func looksLikeMP3(data []byte) bool {
	if len(data) >= 3 && string(data[0:3]) == "ID3" {
		return true
	}
	return len(data) >= 2 && data[0] == 0xff && data[1]&0xe0 == 0xe0
}

// SaveGeneratedAudio persists a playable clip as a chat artifact.
func (r *Runtime) SaveGeneratedAudio(session, path string, data []byte) (Result, error) {
	if len(data) == 0 || len(data) > maxGeneratedBytes {
		return Result{}, errors.New("generated audio exceeds size limit or is empty")
	}
	ext := strings.ToLower(filepath.Ext(path))
	if filepath.IsAbs(path) || filepath.VolumeName(path) != "" || !audioExtOK(ext) {
		return Result{}, errors.New("generated audio requires a workspace-relative .wav or .mp3 path")
	}
	if ext == ".wav" && !looksLikeWAV(data) {
		return Result{}, errors.New("provider did not return a valid WAV")
	}
	if ext == ".mp3" && !looksLikeMP3(data) {
		return Result{}, errors.New("provider did not return a valid MP3")
	}
	out, err := r.writeGenerated(AutoEdit, session, path, data, -1, false)
	if err != nil {
		return Result{}, err
	}
	out.Artifact = &Artifact{Kind: "audio", Path: filepath.ToSlash(path)}
	return out, nil
}
