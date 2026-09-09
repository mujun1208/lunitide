package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/oklog/ulid/v2"
)

func officeTaskCreatedBefore(a, b domain.Task) bool {
	if a.CreatedAt.Equal(b.CreatedAt) {
		return a.ID < b.ID
	}
	return a.CreatedAt.Before(b.CreatedAt)
}

// StartMessageID is exclusive. The next task starts after its own boundary,
// so a message at that boundary still belongs to the preceding task. An
// include-history task has no explicit boundary; its creation time prevents
// older tasks from absorbing later unbound outputs in that case.
func officeArchiveMessageInTask(id string, task domain.Task, tasks []domain.Task) bool {
	if task.StartMessageID != "" && id <= task.StartMessageID {
		return false
	}
	var next *domain.Task
	for i := range tasks {
		candidate := &tasks[i]
		if officeTaskCreatedBefore(task, *candidate) && (next == nil || officeTaskCreatedBefore(*candidate, *next)) {
			next = candidate
		}
	}
	if next == nil {
		return true
	}
	if next.StartMessageID != "" {
		return id <= next.StartMessageID
	}
	messageID, err := ulid.ParseStrict(id)
	return err == nil && messageID.Time() < uint64(next.CreatedAt.UnixMilli())
}

func officeArchiveItemInTask(id string, a SessionArtifact, t domain.Task, tasks []domain.Task) bool {
	if a.OfficeTaskID != "" {
		return a.OfficeTaskID == t.ID
	}
	return officeArchiveMessageInTask(id, t, tasks)
}

func (e *Engine) syncOfficeArtifacts(ctx context.Context, supplied domain.Task, selectedPaths ...string) error {
	if e.tools == nil {
		return nil
	}
	archiveStore, ok := e.officeStudio.Store.(domain.ArchiveStore)
	if !ok {
		return fmt.Errorf("office archive metadata is unavailable")
	}
	snapshot, err := archiveStore.ReadOfficeArchiveSnapshot(ctx, supplied.ID)
	if err != nil {
		return err
	}
	t := snapshot.Task
	if t.SessionID != supplied.SessionID {
		return domain.ErrScope
	}
	index := e.loadSessionArtifactsByMessage(t.SessionID)
	selectedPath := ""
	if len(selectedPaths) > 0 {
		selectedPath = selectedPaths[0]
	}
	ids := make([]string, 0, len(index))
	for id := range index {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	// A reused workspace path contains its newest bytes. Do not re-import a
	// newer task's replacement through an older message's file card.
	type location struct {
		id       string
		artifact SessionArtifact
	}
	latestPath := map[string]location{}
	for _, id := range ids {
		for _, a := range index[id] {
			latestPath[a.Path] = location{id, a}
		}
	}
	known := map[string]bool{}
	foreign := map[string]bool{}
	artifactByPath := map[string]string{}
	for _, v := range snapshot.Versions {
		if v.TaskID == t.ID {
			known[v.SHA256] = true
			if v.SourcePath != "" {
				artifactByPath[v.SourcePath] = v.ArtifactID
			}
		} else {
			foreign[v.SHA256] = true
		}
	}
	selectedFound := selectedPath == ""
	for _, id := range ids {
		for _, a := range index[id] {
			if latestPath[a.Path].id != id {
				continue // Current bytes are evidence for the newest file card.
			}
			if selectedPath != "" {
				if a.Path != selectedPath {
					continue
				}
				selectedFound = true
			} else {
				if !officeArchiveItemInTask(id, a, t, snapshot.Tasks) {
					continue
				}
				last := latestPath[a.Path]
				if !officeArchiveItemInTask(last.id, last.artifact, t, snapshot.Tasks) {
					continue
				}
			}
			if a.Kind != "pptx" && a.Kind != "docx" && a.Kind != "xlsx" && a.Kind != "pdf" {
				continue
			}
			data, readErr := e.tools.ReadWorkspaceFile(t.SessionID, a.Path, 8<<20)
			if readErr != nil {
				if selectedPath != "" {
					return fmt.Errorf("读取文件 %s：%w", filepath.Base(a.Path), readErr)
				}
				continue // A removed historical file cannot block other deliveries.
			}
			hash := sha256.Sum256(data)
			sha := hex.EncodeToString(hash[:])
			if known[sha] {
				continue
			}
			// Legacy approval results may lack an OfficeTaskID. Preserve a
			// formal Office version's ownership instead of duplicating it into
			// the currently visible task. An explicit user import can reuse it.
			if selectedPath == "" && a.OfficeTaskID == "" && foreign[sha] {
				continue
			}
			artifactID, baseID := artifactByPath[a.Path], ""
			var revision int64
			if artifactID != "" {
				heads, headErr := e.officeStudio.Store.ListOfficeHeads(ctx, t.ID)
				if headErr != nil {
					return headErr
				}
				for _, h := range heads {
					if h.ArtifactID == artifactID {
						revision, baseID = h.Revision, h.LatestVersionID
					}
				}
			}
			var v domain.Version
			var runID string
			err = e.officeStudio.Execute(ctx, t.ID, "sync", func(run context.Context) error {
				current, getErr := e.officeStudio.Store.GetOfficeTask(run, t.ID)
				if getErr != nil {
					return getErr
				}
				runID = current.RunID
				var importErr error
				v, importErr = e.officeStudio.ImportLinked(run, t.ID, artifactID, filepath.Base(a.Path), a.Path, data, baseID, revision, "chat-"+t.ID+"-"+sha)
				return importErr
			})
			if err != nil {
				return fmt.Errorf("同步文件 %s：%w", filepath.Base(a.Path), err)
			}
			known[sha] = true
			artifactByPath[a.Path] = v.ArtifactID
			if runID == "" {
				runID = t.ID
			}
			receipt := domain.StepReceipt{TaskID: t.ID, RunID: runID, StepKey: "从当前会话归档产物", IdempotencyKey: "chat-receipt-" + v.ID, InputDigest: sha, State: "succeeded", Result: json.RawMessage(fmt.Sprintf(`{"versionId":%q,"messageId":%q}`, v.ID, id)), CreatedAt: time.Now().UTC()}
			if err = e.officeStudio.Store.AppendOfficeStepReceipt(ctx, receipt); err != nil {
				return err
			}
		}
	}
	if !selectedFound {
		return domain.ErrNotFound
	}
	return nil
}
