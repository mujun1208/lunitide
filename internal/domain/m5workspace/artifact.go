package m5workspace

import (
	"errors"
	"time"
)

// DownloadState is the frozen three-state artifact download lifecycle
// (M5 T-5.2.5): blocked until the user explicitly allows, allowed once
// confirmed, downloaded after the bytes left the product.
type DownloadState string

const (
	DownloadBlocked    DownloadState = "blocked"
	DownloadAllowed    DownloadState = "allowed"
	DownloadDownloaded DownloadState = "downloaded"
)

// Terminal reports whether the state may not transition further.
func (d DownloadState) Terminal() bool { return d == DownloadDownloaded }

// Artifact is the m5_artifact row.
type Artifact struct {
	ID            string        `json:"id"`
	RunID         string        `json:"runId"`
	Mime          string        `json:"mime"`
	Size          int64         `json:"size"`
	SHA256        string        `json:"sha256"`
	Generator     string        `json:"generator"`
	DownloadState DownloadState `json:"downloadState"`
	CreatedAt     time.Time     `json:"createdAt"`
}

var (
	ErrArtifactNotFound   = errors.New("m5workspace: artifact not found")
	ErrArtifactStateBad   = errors.New("m5workspace: artifact download state does not allow this transition")
	ErrArtifactTooLarge   = errors.New("m5workspace: artifact exceeds the size limit")
	ErrArtifactMime       = errors.New("m5workspace: artifact mime is invalid or masks an executable")
	ErrArtifactTampered   = errors.New("m5workspace: artifact content no longer matches its registered digest")
	ErrArtifactNotPreview = errors.New("m5workspace: artifact type may only be downloaded, not previewed")
)
