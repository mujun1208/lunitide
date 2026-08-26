package tts

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Fitting a reference clip to the cloning window.
//
// GPT-SoVITS clones a timbre from a prompt of roughly 3 to 10 seconds. Longer
// than that and the request fails; the catalogue was assembled from voice
// packs where a third of the clips are narration samples running to a minute
// or more, and each of those was a voice the settings dropdown offered and
// that then simply did not work.
//
// Dropping them loses good timbres for a reason that has nothing to do with
// how they sound, so an over-long clip is trimmed to the front of itself
// instead. The opening seconds are a fair sample of a voice — it is the same
// speaker throughout — and the trimmed copy is written beside the cache
// rather than over the user's own file.

// refPromptSeconds is how much of an over-long clip to keep. Comfortably
// inside the window at both ends: long enough to carry a timbre, short enough
// that a clip whose length we measured slightly wrong is still accepted.
const refPromptSeconds = 6

// refPromptMaxSeconds is the longest clip passed through untouched.
const refPromptMaxSeconds = 10

var errNotRIFF = errors.New("tts: not a readable RIFF/WAVE file")

// refPromptClip returns a path to a clip inside the cloning window.
//
// Returns the original path unchanged when it already fits, or when it cannot
// be read as WAV — an unreadable file is the synthesis service's problem to
// report, and guessing at it here would replace a clear error with a strange
// one.
func refPromptClip(path string) string {
	trimmed, err := trimToPromptWindow(path)
	if err != nil {
		return path
	}
	return trimmed
}

func trimToPromptWindow(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	fmtChunk, data, byteRate, err := waveChunks(raw)
	if err != nil {
		return "", err
	}
	if byteRate == 0 {
		return "", errNotRIFF
	}
	if float64(len(data))/float64(byteRate) <= refPromptMaxSeconds {
		return path, nil
	}

	keep := int(byteRate) * refPromptSeconds
	if keep > len(data) {
		keep = len(data)
	}
	// Whole sample frames only: half a frame shifts every following sample
	// into the wrong channel and turns the clip into noise.
	if align := fmtChunk.blockAlign; align > 0 {
		keep -= keep % int(align)
	}
	if keep <= 0 {
		return "", errNotRIFF
	}

	cacheDir, err := refPromptCacheDir()
	if err != nil {
		return "", err
	}
	out := filepath.Join(cacheDir, fmt.Sprintf("%ds-%s", refPromptSeconds, filepath.Base(path)))
	if info, err := os.Stat(out); err == nil && info.Size() > 0 {
		return out, nil
	}
	if err := os.WriteFile(out, buildWave(fmtChunk, data[:keep]), 0o600); err != nil {
		return "", err
	}
	return out, nil
}

func refPromptCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "Lunitide", "refprompt")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// waveFormat is the part of a fmt chunk needed to rebuild a header.
type waveFormat struct {
	raw        []byte
	blockAlign uint16
}

// waveChunks pulls the fmt and data chunks out of a RIFF/WAVE file.
func waveChunks(raw []byte) (waveFormat, []byte, uint32, error) {
	if len(raw) < 12 || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return waveFormat{}, nil, 0, errNotRIFF
	}
	var (
		format   waveFormat
		data     []byte
		byteRate uint32
	)
	for at := 12; at+8 <= len(raw); {
		id := string(raw[at : at+4])
		size := int(binary.LittleEndian.Uint32(raw[at+4 : at+8]))
		body := at + 8
		if size < 0 || body+size > len(raw) {
			size = len(raw) - body
		}
		switch id {
		case "fmt ":
			if size >= 16 {
				format.raw = raw[body : body+size]
				format.blockAlign = binary.LittleEndian.Uint16(raw[body+12 : body+14])
				byteRate = binary.LittleEndian.Uint32(raw[body+8 : body+12])
			}
		case "data":
			data = raw[body : body+size]
		}
		at = body + size
		if size%2 == 1 {
			at++
		}
	}
	if format.raw == nil || data == nil {
		return waveFormat{}, nil, 0, errNotRIFF
	}
	return format, data, byteRate, nil
}

// buildWave writes a canonical RIFF/WAVE file: header, the original fmt
// chunk, then the samples. Any other chunks the source carried are dropped,
// which is the point — cue points and loop markers from a longer recording
// describe positions that no longer exist.
func buildWave(format waveFormat, data []byte) []byte {
	out := make([]byte, 0, 12+8+len(format.raw)+8+len(data))
	out = append(out, "RIFF"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(4+8+len(format.raw)+8+len(data)))
	out = append(out, "WAVE"...)
	out = append(out, "fmt "...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(format.raw)))
	out = append(out, format.raw...)
	out = append(out, "data"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(data)))
	out = append(out, data...)
	return out
}
