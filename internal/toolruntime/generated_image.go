package toolruntime

import (
	"bytes"
	"errors"
	"image/png"
	"path/filepath"
	"strings"
)

// Generated images use the existing PNG artifact/preview contract. Never
// label arbitrary provider bytes as a successfully generated image.
func (r *Runtime) SaveGeneratedImage(session, path string, data []byte) (Result, error) {
	if len(data) == 0 || len(data) > maxGeneratedBytes {
		return Result{}, errors.New("generated image exceeds size limit or is empty")
	}
	if filepath.IsAbs(path) || filepath.VolumeName(path) != "" || strings.ToLower(filepath.Ext(path)) != ".png" {
		return Result{}, errors.New("generated image requires a workspace-relative .png path")
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 40_000_000 {
		return Result{}, errors.New("provider did not return a valid bounded PNG")
	}
	if _, err = png.Decode(bytes.NewReader(data)); err != nil {
		return Result{}, errors.New("provider returned a corrupt PNG")
	}
	out, err := r.writeGenerated(AutoEdit, session, path, data, -1, false)
	if err != nil {
		return Result{}, err
	}
	out.Artifact = &Artifact{Kind: "image", Path: filepath.ToSlash(path)}
	return out, nil
}
