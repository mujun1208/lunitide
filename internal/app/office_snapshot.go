package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/bridge"
	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
)

const officeSnapshotFrameBytes = 120 << 10

// Only a handler that has already completed its mutation calls this path.
// Failure to read the first display snapshot must not turn a committed write
// into a failed operation. The known task came from an authorized read or the
// successful mutator; it is never an invented placeholder for missing data.
func (e *Engine) officeCommittedSnapshotFallback(ctx context.Context, r bridge.Request, knownTask domain.Task) bridge.Response {
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	task, err := e.officeStudio.Store.GetOfficeTask(readCtx, knownTask.ID)
	stale := err != nil
	if stale {
		task = knownTask
	}
	return r.Ok(map[string]any{
		"task":      e.officeTaskDTO(ctx, task, ""),
		"artifacts": []any{}, "steps": []any{}, "sources": []any{},
		"committed": true, "snapshotIncomplete": true, "taskSnapshotStale": stale,
		"loadNotice": "操作已提交，工作台记录暂未读取完整。请刷新查看；本次空列表不表示文件已删除。",
	})
}

type officeSnapshotRequest struct {
	SnapshotOffset int    `json:"snapshotOffset"`
	SnapshotDigest string `json:"snapshotDigest"`
}

type officeSnapshotItem struct {
	Kind string
	Row  map[string]any
}

func officeSnapshotText(value string, limit int) (string, bool) {
	if len(value) <= limit {
		return value, false
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit] + "…", true
}

func officeSnapshotHash(value any) (string, error) {
	h := sha256.New()
	if err := json.NewEncoder(h).Encode(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func officeSnapshotRequestFor(r bridge.Request) (officeSnapshotRequest, error) {
	var p officeSnapshotRequest
	if err := json.Unmarshal(r.Payload, &p); err != nil {
		return p, domain.ErrInvalid
	}
	if p.SnapshotOffset < 0 || (p.SnapshotOffset > 0 && len(p.SnapshotDigest) != 64) {
		return p, domain.ErrInvalid
	}
	if p.SnapshotDigest != "" {
		if _, err := hex.DecodeString(p.SnapshotDigest); err != nil || len(p.SnapshotDigest) != 64 {
			return p, domain.ErrInvalid
		}
	}
	return p, nil
}

// The digest includes every immutable record ID and mutable head/revision.
// Abbreviating large evidence for display cannot conceal a changed snapshot:
// changes require a new immutable ID or a revision and invalidate the digest.
func officeSnapshotResponse(r bridge.Request, p officeSnapshotRequest, identity any, items []officeSnapshotItem, base map[string]any) bridge.Response {
	digest, err := officeSnapshotHash(identity)
	if err != nil {
		return officeFailure(r, err)
	}
	if p.SnapshotDigest != "" && p.SnapshotDigest != digest {
		return r.Fail("OFFICE_SNAPSHOT_CHANGED", "文件或任务状态已变化，请从第一页重新读取", true)
	}
	if p.SnapshotOffset > len(items) {
		return officeFailure(r, domain.ErrInvalid)
	}
	build := func(end int) map[string]any {
		out := map[string]any{}
		for k, v := range base {
			out[k] = v
		}
		out["snapshotOffset"] = p.SnapshotOffset
		out["totalSnapshotItems"] = len(items)
		out["snapshotDigest"] = digest
		out["nextSnapshotOffset"] = -1
		if end < len(items) {
			out["nextSnapshotOffset"] = end
		}
		groups := map[string][]any{}
		artifactRows := map[string]map[string]any{}
		versionRows := map[string]map[string]any{}
		for _, item := range items[p.SnapshotOffset:end] {
			if item.Kind != "artifacts" {
				groups[item.Kind] = append(groups[item.Kind], item.Row)
				continue
			}
			id := item.Row["id"].(string)
			artifact := artifactRows[id]
			if artifact == nil {
				artifact = map[string]any{}
				for k, v := range item.Row {
					artifact[k] = v
				}
				artifact["versions"] = []any{}
				artifactRows[id] = artifact
				groups["artifacts"] = append(groups["artifacts"], artifact)
			}
			v := item.Row["version"].(map[string]any)
			vid := v["id"].(string)
			existing := versionRows[vid]
			if existing == nil {
				existing = map[string]any{}
				for k, value := range v {
					existing[k] = value
				}
				versionRows[vid] = existing
				artifact["versions"] = append(artifact["versions"].([]any), existing)
			} else {
				existing["validations"] = append(existing["validations"].([]any), v["validations"].([]any)...)
			}
			delete(artifact, "version")
		}
		for k, v := range groups {
			out[k] = v
		}
		return out
	}
	response := r.Ok(build(p.SnapshotOffset))
	for end := p.SnapshotOffset + 1; end <= min(len(items), p.SnapshotOffset+200); end++ {
		candidate := response
		candidate.Payload = build(end)
		encoded, e := json.Marshal(candidate)
		if e != nil {
			return officeFailure(r, e)
		}
		if len(encoded) > officeSnapshotFrameBytes {
			if end == p.SnapshotOffset+1 {
				return r.Fail("OFFICE_SNAPSHOT_ITEM_TOO_LARGE", "此条概要仍然过大，请缩小显示内容", false)
			}
			break
		}
		response = candidate
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > officeSnapshotFrameBytes {
		return officeFailure(r, domain.ErrInvalid)
	}
	return response
}

func (e *Engine) officeSnapshotDetail(ctx context.Context, r bridge.Request, id string) bridge.Response {
	loader, ok := e.officeStudio.Store.(domain.SnapshotStore)
	if !ok {
		return r.Fail("OFFICE_SNAPSHOT_UNAVAILABLE", "办公快照存储尚未就绪", true)
	}
	p := officeSnapshotRequest{}
	if r.Method == "office.task.get" {
		var err error
		p, err = officeSnapshotRequestFor(r)
		if err != nil {
			return officeFailure(r, err)
		}
	}
	snapshot, err := loader.ReadOfficeSnapshot(ctx, id)
	if err != nil {
		return officeFailure(r, err)
	}
	heads := map[string]domain.Head{}
	latest := map[string]domain.Version{}
	for _, h := range snapshot.Heads {
		heads[h.ArtifactID] = h
	}
	for _, v := range snapshot.Versions {
		if v.ID == heads[v.ArtifactID].LatestVersionID {
			latest[v.ArtifactID] = v
		}
	}
	items := []officeSnapshotItem{}
	roles := map[string]string{}
	for _, v := range snapshot.Versions {
		var spec struct {
			SourcePath string `json:"sourcePath"`
		}
		_ = json.Unmarshal(v.Spec, &spec)
		if v.ContentMode == "managed" || spec.SourcePath != "" || v.BaseVersionID != "" {
			roles[v.ArtifactID] = "deliverable"
		} else if roles[v.ArtifactID] == "" {
			roles[v.ArtifactID] = "reference"
		}
	}
	taskDTO := e.officeTaskDTO(ctx, snapshot.Task)
	taskJSON, err := json.Marshal(taskDTO)
	if err != nil {
		return officeFailure(r, err)
	}
	checkChunk := 8
	if len(taskJSON) > 64<<10 {
		checkChunk = 1
	}
	for _, v := range snapshot.Versions {
		h := heads[v.ArtifactID]
		checks := snapshot.Checks[v.ID]
		for offset := 0; offset < len(checks) || offset == 0; offset += checkChunk {
			qa := []any{}
			for _, c := range checks[offset:min(offset+checkChunk, len(checks))] {
				status, severity := c.Status, "info"
				if status == "missing" || status == "skipped" || status == "unsupported" {
					status = "unavailable"
					severity = "warning"
				}
				if status == "failed" && c.Required {
					severity = "blocking"
				}
				label, labelTruncated := officeSnapshotText(c.Label, 128)
				qa = append(qa, map[string]any{"id": c.ID, "label": label, "labelTruncated": labelTruncated, "status": status, "severity": severity, "message": c.Detail, "messageTruncated": c.DetailTruncated})
			}
			row := map[string]any{"id": v.ID, "versionNo": v.VersionNo, "quality": v.Quality, "mode": v.ContentMode, "size": v.Size, "sha256": v.SHA256, "createdAt": v.CreatedAt, "validations": qa, "validationOffset": offset, "totalValidations": len(checks)}
			if v.BaseVersionID != "" {
				row["parentVersionId"] = v.BaseVersionID
			}
			artifact := map[string]any{"id": h.ArtifactID, "revision": h.Revision, "name": latest[v.ArtifactID].Name, "kind": latest[v.ArtifactID].Kind, "headVersionId": h.LatestVersionID, "version": row}
			artifact["role"] = roles[v.ArtifactID]
			if h.AcceptedVersionID != "" {
				artifact["acceptedVersionId"] = h.AcceptedVersionID
			}
			items = append(items, officeSnapshotItem{Kind: "artifacts", Row: artifact})
		}
	}
	for _, st := range snapshot.Steps {
		text, truncated := officeSnapshotText(st.Summary, 1024)
		items = append(items, officeSnapshotItem{Kind: "steps", Row: map[string]any{"id": st.ID, "label": st.Label, "status": st.State, "summary": text, "summaryTruncated": truncated || st.SummaryTruncated, "createdAt": st.CreatedAt}})
	}
	for _, src := range snapshot.Sources {
		text, truncated := officeSnapshotText(src.Transform, 1024)
		items = append(items, officeSnapshotItem{Kind: "sources", Row: map[string]any{"id": src.ID, "name": "来源版本", "versionId": src.VersionID, "targetVersionId": src.TargetVersionID, "location": src.Location, "targetLocation": src.TargetLocation, "transform": text, "transformTruncated": truncated || src.TransformTruncated}})
	}
	for _, h := range snapshot.Heads {
		if roles[h.ArtifactID] != "reference" {
			continue
		}
		v := latest[h.ArtifactID]
		items = append(items, officeSnapshotItem{Kind: "sources", Row: map[string]any{"id": "reference-" + h.ArtifactID, "name": v.Name, "versionId": v.ID, "location": "上传的参考材料", "transform": "已保存原件；是否用于交付文件请核对生成结果与引用记录。"}})
	}
	return officeSnapshotResponse(r, p, snapshot, items, map[string]any{"task": taskDTO, "artifacts": []any{}, "steps": []any{}, "sources": []any{}})
}

func (e *Engine) officeSnapshotTaskList(ctx context.Context, r bridge.Request, sessionID, query string) bridge.Response {
	loader, ok := e.officeStudio.Store.(domain.SnapshotStore)
	if !ok {
		return r.Fail("OFFICE_SNAPSHOT_UNAVAILABLE", "办公快照存储尚未就绪", true)
	}
	p, err := officeSnapshotRequestFor(r)
	if err != nil {
		return officeFailure(r, err)
	}
	tasks, err := loader.ReadOfficeTaskListSnapshot(ctx, sessionID, query)
	if err != nil {
		return officeFailure(r, err)
	}
	items := make([]officeSnapshotItem, 0, len(tasks))
	for _, t := range tasks {
		row := e.officeTaskDTO(ctx, t.Task, t.ProjectID)
		goal, truncated := officeSnapshotText(t.Goal, 256)
		row["goal"] = goal
		row["goalTruncated"] = truncated || t.GoalTruncated
		items = append(items, officeSnapshotItem{Kind: "items", Row: row})
	}
	return officeSnapshotResponse(r, p, struct {
		SessionID, Query string
		Tasks            []domain.SnapshotTask
	}{sessionID, query, tasks}, items, map[string]any{"items": []any{}})
}

func officeSnapshotMetrics(r bridge.Request, metrics []domain.Metric) bridge.Response {
	p, err := officeSnapshotRequestFor(r)
	if err != nil {
		return officeFailure(r, err)
	}
	items := make([]officeSnapshotItem, 0, len(metrics))
	for _, metric := range metrics {
		raw, _ := json.Marshal(metric)
		var row map[string]any
		if err = json.Unmarshal(raw, &row); err != nil {
			return officeFailure(r, err)
		}
		row["rawValue"], row["rawValueTruncated"] = officeSnapshotText(metric.RawValue, 1024)
		row["displayValue"], row["displayValueTruncated"] = officeSnapshotText(metric.DisplayValue, 1024)
		items = append(items, officeSnapshotItem{Kind: "items", Row: row})
	}
	return officeSnapshotResponse(r, p, metrics, items, map[string]any{"items": []any{}})
}

func officeSnapshotBundles(r bridge.Request, bundles []domain.Bundle) bridge.Response {
	p, err := officeSnapshotRequestFor(r)
	if err != nil {
		return officeFailure(r, err)
	}
	items := make([]officeSnapshotItem, 0, len(bundles))
	for _, bundle := range bundles {
		raw, _ := json.Marshal(bundle)
		var row map[string]any
		if err = json.Unmarshal(raw, &row); err != nil {
			return officeFailure(r, err)
		}
		items = append(items, officeSnapshotItem{Kind: "items", Row: row})
	}
	return officeSnapshotResponse(r, p, bundles, items, map[string]any{"items": []any{}})
}
