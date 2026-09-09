package app

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/canonpath"
	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/officeapp"
	"github.com/lunitide/lunitide/internal/officerender"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

func (e *Engine) SetOfficeStudio(s *officeapp.Service) {
	e.officeStudio = s
	if e.tools != nil {
		e.tools.SetOfficeExecutor(e.executeOfficeTool)
	}
}

type officePayload struct {
	Path             string               `json:"path"`
	Reveal           bool                 `json:"reveal"`
	TaskID           string               `json:"taskId"`
	SessionID        string               `json:"sessionId"`
	Title            string               `json:"title"`
	Goal             string               `json:"goal"`
	Query            string               `json:"query"`
	IncludeHistory   bool                 `json:"includeHistory"`
	ArtifactPath     string               `json:"artifactPath"`
	VersionID        string               `json:"versionId"`
	ArtifactID       string               `json:"artifactId"`
	AttachmentID     string               `json:"attachmentId"`
	SHA256           string               `json:"sha256"`
	Fit              string               `json:"fit"`
	Alt              *string              `json:"alt"`
	Name             string               `json:"name"`
	BaseVersionID    string               `json:"baseVersionId"`
	ExpectedRevision int64                `json:"expectedRevision"`
	NodeID           string               `json:"nodeId"`
	NodeDigest       string               `json:"nodeDigest"`
	Text             string               `json:"text"`
	Draft            bool                 `json:"draft"`
	Offset           int64                `json:"offset"`
	Limit            int64                `json:"limit"`
	Ranges           []content.RangePatch `json:"ranges"`
	Chart            content.SlideChart   `json:"chart"`
	NodeOffset       int                  `json:"nodeOffset"`
	PartOffset       int                  `json:"partOffset"`
	SnapshotOffset   int                  `json:"snapshotOffset"`
	SnapshotDigest   string               `json:"snapshotDigest"`
}

func officeFailure(r bridge.Request, err error) bridge.Response {
	code, retry := "OFFICE_OPERATION_FAILED", true
	switch {
	case errors.Is(err, domain.ErrStorageQuota):
		code, retry = "OFFICE_STORAGE_QUOTA", false
	case errors.Is(err, domain.ErrBlobLease):
		code = "OFFICE_BLOB_LEASE_EXPIRED"
	case errors.Is(err, domain.ErrNotFound):
		code = "OFFICE_NOT_FOUND"
		retry = false
	case errors.Is(err, domain.ErrScope):
		code = "OFFICE_SCOPE_MISMATCH"
		retry = false
	case errors.Is(err, domain.ErrConflict):
		code = "OFFICE_VERSION_CONFLICT"
		retry = false
	case errors.Is(err, domain.ErrInvalid):
		code = "BRIDGE_SCHEMA_INVALID"
		retry = false
	case errors.Is(err, context.Canceled):
		code = "OFFICE_CANCELLED"
	case errors.Is(err, context.DeadlineExceeded):
		code = "OFFICE_TIMEOUT"
	}
	return r.Fail(code, err.Error(), retry)
}

func officeMutation(method string) bool {
	switch method {
	case "office.task.list", "office.task.get", "office.artifact.preview", "office.artifact.chart", "office.artifact.diff", "office.artifact.readChunk", "office.renderer.probe", "office.artifact.export", "office.artifact.open":
		return false
	}
	return true
}

func handleOfficeStudio(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.officeStudio == nil {
		return r.Fail("FEATURE_DISABLED", "办公工作台尚未初始化", false)
	}
	if officeMutation(r.Method) && r.Method != "office.task.cancel" && !e.officeCapabilities().Studio {
		return r.Fail("FEATURE_DISABLED", "办公工作台已关闭新建和修改；已有文件仍可查看、导出", false)
	}
	var p officePayload
	if decodePayload(r.Payload, &p) != nil {
		return officeFailure(r, domain.ErrInvalid)
	}
	orgID, _, scopeErr := e.boundOrgState(ctx)
	if scopeErr != nil {
		return officeFailure(r, scopeErr)
	}
	ctx = domain.WithScope(ctx, orgID)
	s := e.officeStudio
	if err := s.RecoverOnce(ctx); err != nil {
		return officeFailure(r, err)
	}
	if r.Method == "office.renderer.probe" {
		c := s.Renderer.Probe(ctx)
		status := "unavailable"
		if c.Available {
			status = "ready"
		}
		desktopNotice, desktopReady := officerender.DesktopApplicationsNotice(officerender.ProbeDesktopApplications())
		desktopStatus := "unavailable"
		if desktopReady {
			desktopStatus = "ready"
		}
		return r.Ok(map[string]any{"components": []map[string]string{
			{"id": "go", "label": "本地文件结构检查", "status": "ready", "detail": "可生成与检查受支持的 Office 文件；不等于目标软件验证"},
			{"id": "desktop-office", "label": "本机 Office / WPS", "status": desktopStatus, "detail": desktopNotice},
			{"id": "libreoffice", "label": "LibreOffice 隔离排版检查", "status": status, "detail": strings.TrimSpace(c.Version + " " + c.Notice)},
		}})
	}
	if r.Method == "office.task.list" {
		return e.officeSnapshotTaskList(ctx, r, p.SessionID, p.Query)
	}
	if r.Method == "office.task.create" {
		if failure := requireIdempotency(r); failure != nil {
			return *failure
		}
		if e.sessions == nil || e.projects == nil {
			return r.Fail("STORAGE_UNAVAILABLE", "会话服务尚未就绪", true)
		}
		title, err := session.NormalizeTitle(p.Title)
		if err != nil {
			return officeFailure(r, domain.ErrInvalid)
		}
		if len(p.Goal) > 8192 {
			return officeFailure(r, domain.ErrInvalid)
		}
		sid := p.SessionID
		if sid == "" {
			pid, err := e.ensurePersonalChatProject(ctx)
			if err != nil {
				return officeFailure(r, err)
			}
			created, err := e.sessions.Create(ctx, "office-session-"+r.IdempotencyKey, "office-studio", map[string]string{"title": title, "goal": p.Goal}, session.Session{ProjectID: pid, Title: title})
			if err != nil {
				return officeFailure(r, err)
			}
			sid = created.ID
		}
		if !validCanonicalULID(sid) {
			return officeFailure(r, domain.ErrInvalid)
		}
		if pid, ok, err := projectIDForSession(e, ctx, sid); err != nil {
			return officeFailure(r, err)
		} else if ok {
			if f := rejectIfProjectReadOnly(e, ctx, r, pid); f != nil {
				return *f
			}
		}
		previous, lookupErr := s.Store.FindOfficeTaskByKey(ctx, sid, r.IdempotencyKey)
		if lookupErr == nil {
			// Revalidate the frozen creation payload, not mutable current title.
			replayed, er := s.Store.CreateOfficeTask(ctx, domain.Task{SessionID: sid, Title: title, Goal: p.Goal, Status: "draft", StartMessageID: previous.StartMessageID}, r.IdempotencyKey)
			if er != nil {
				return officeFailure(r, er)
			}
			return e.officeDetailResponse(ctx, r, replayed)
		}
		if !errors.Is(lookupErr, domain.ErrNotFound) {
			return officeFailure(r, lookupErr)
		}
		start := ""
		if !p.IncludeHistory && e.messages != nil {
			page, err := e.messages.List(ctx, messageapp.PageRequest{SessionID: sid, Limit: 1, Direction: messageapp.Backward})
			if err != nil {
				return officeFailure(r, err)
			}
			if len(page.Items) > 0 {
				start = page.Items[0].ID
			}
		}
		created, err := s.Store.CreateOfficeTask(ctx, domain.Task{SessionID: sid, Title: title, Goal: p.Goal, Status: "draft", StartMessageID: start}, r.IdempotencyKey)
		if err != nil {
			return officeFailure(r, err)
		}
		return e.officeDetailResponse(ctx, r, created)
	}
	if !validCanonicalULID(p.TaskID) {
		return officeFailure(r, domain.ErrInvalid)
	}
	task, err := s.Store.GetOfficeTask(ctx, p.TaskID)
	if err != nil {
		return officeFailure(r, err)
	}
	if officeMutation(r.Method) {
		if pid, ok, err := projectIDForSession(e, ctx, task.SessionID); err != nil {
			return officeFailure(r, err)
		} else if ok {
			if f := rejectIfProjectReadOnly(e, ctx, r, pid); f != nil {
				return *f
			}
		}
	}
	if r.Method == "office.task.get" {
		return e.officeDetailResponse(ctx, r, task)
	}
	if r.Method == "office.artifact.open" {
		root, err := canonpath.Canonical(filepath.Join(s.Root, "exports", p.TaskID))
		if err != nil {
			return officeFailure(r, err)
		}
		target, err := canonpath.Canonical(p.Path)
		if err != nil {
			return officeFailure(r, err)
		}
		rel, err := filepath.Rel(root, target)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return officeFailure(r, domain.ErrScope)
		}
		st, err := os.Stat(target)
		if err != nil || !st.Mode().IsRegular() {
			return officeFailure(r, domain.ErrNotFound)
		}
		if err = openArtifactTarget(target, true, p.Reveal); err != nil {
			return officeFailure(r, err)
		}
		return r.Ok(map[string]any{"opened": filepath.ToSlash(target)})
	}
	if r.Method == "office.task.cancel" {
		if err = s.Cancel(ctx, task.ID); err != nil {
			return officeFailure(r, err)
		}
		return e.officeDetailResponse(ctx, r, task)
	}
	if r.Method == "office.task.update" {
		if p.ExpectedRevision < 1 {
			return officeFailure(r, domain.ErrInvalid)
		}
		task.Title = p.Title
		task.Goal = p.Goal
		task, err = s.Store.UpdateOfficeTask(ctx, task, p.ExpectedRevision)
		if err != nil {
			return officeFailure(r, err)
		}
		return e.officeDetailResponse(ctx, r, task)
	}
	if r.Method == "office.task.sync" {
		if err = e.syncOfficeArtifacts(ctx, task, p.ArtifactPath); err != nil {
			return officeFailure(r, err)
		}
		return e.officeDetailResponse(ctx, r, task)
	}
	if r.Method == "office.artifact.import" {
		if failure := requireIdempotency(r); failure != nil {
			return *failure
		}
		if e.attachmentService == nil {
			return r.Fail("FEATURE_DISABLED", "附件服务尚未就绪", false)
		}
		a, data, err := e.attachmentService.ReadOfficeSnapshot(ctx, p.AttachmentID, task.SessionID)
		if err != nil {
			return officeFailure(r, err)
		}
		name := a.OriginalName
		if p.Name != "" {
			name = p.Name
		}
		if p.ArtifactID != "" || p.BaseVersionID != "" || p.ExpectedRevision != 0 {
			v, _, er := s.ReadVersion(ctx, p.TaskID, p.BaseVersionID)
			if er != nil {
				return officeFailure(r, er)
			}
			if v.ArtifactID != p.ArtifactID || p.ExpectedRevision < 1 {
				return officeFailure(r, domain.ErrScope)
			}
			if strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".") != v.Kind {
				return officeFailure(r, domain.ErrInvalid)
			}
		}
		err = s.Execute(ctx, p.TaskID, "running", func(run context.Context) error {
			_, importErr := s.Import(run, p.TaskID, p.ArtifactID, name, data, p.BaseVersionID, p.ExpectedRevision, r.IdempotencyKey)
			return importErr
		})
		if err != nil {
			return officeFailure(r, err)
		}
		return e.officeDetailResponse(ctx, r, task)
	}
	vid := p.VersionID
	if r.Method == "office.artifact.patch" || r.Method == "office.artifact.patchRange" || r.Method == "office.artifact.patchChart" || r.Method == "office.artifact.replaceImage" {
		vid = p.BaseVersionID
	}
	if !validCanonicalULID(vid) {
		return officeFailure(r, domain.ErrInvalid)
	}
	v, _, err := s.ReadVersion(ctx, p.TaskID, vid)
	if err != nil {
		return officeFailure(r, err)
	}
	if p.ArtifactID != "" && p.ArtifactID != v.ArtifactID {
		return officeFailure(r, domain.ErrScope)
	}
	if (r.Method == "office.artifact.patch" || r.Method == "office.artifact.patchRange" || r.Method == "office.artifact.patchChart" || r.Method == "office.artifact.replaceImage" || r.Method == "office.artifact.refresh" || r.Method == "office.artifact.validate") && !e.officeWritesEnabled(strings.NewReplacer("patchRange", "patch", "patchChart", "patch", "replaceImage", "patch", "refresh", "patch").Replace(r.Method), v.Kind) {
		return r.Fail("FEATURE_DISABLED", "此办公能力已关闭；文件可以继续查看和导出", false)
	}
	switch r.Method {
	case "office.artifact.chart":
		_, data, er := s.ReadVersion(ctx, p.TaskID, vid)
		if er != nil {
			return officeFailure(r, er)
		}
		chart, er := content.ReadSlideChart(data, p.NodeID, p.NodeDigest)
		if er != nil {
			return officeFailure(r, er)
		}
		return r.Ok(chart)
	case "office.artifact.patchChart":
		if f := requireIdempotency(r); f != nil {
			return *f
		}
		if v.Kind != "pptx" || p.ExpectedRevision < 1 {
			return officeFailure(r, domain.ErrInvalid)
		}
		err = s.Execute(ctx, p.TaskID, "patch", func(run context.Context) error {
			_, er := s.Patch(run, p.TaskID, vid, p.ExpectedRevision, content.PatchRequest{Kind: content.PPTX, BaseSHA256: v.SHA256, Charts: []content.ChartPatch{{NodeID: p.NodeID, ExpectedDigest: p.NodeDigest, Chart: p.Chart}}}, r.IdempotencyKey)
			return er
		})
	case "office.artifact.refresh":
		if !e.officeCapabilities().Render {
			return r.Fail("FEATURE_DISABLED", "本机排版更新已关闭", false)
		}
		if f := requireIdempotency(r); f != nil {
			return *f
		}
		if p.ExpectedRevision < 1 {
			return officeFailure(r, domain.ErrInvalid)
		}
		err = s.Execute(ctx, p.TaskID, "validating", func(run context.Context) error {
			_, er := s.RefreshNativeCaches(run, p.TaskID, vid, p.ExpectedRevision, r.IdempotencyKey)
			return er
		})
	case "office.artifact.diff":
		result, err := s.Diff(ctx, p.TaskID, p.BaseVersionID, vid, officeapp.DiffOptions{NodeOffset: p.NodeOffset, PartOffset: p.PartOffset})
		if err != nil {
			return officeFailure(r, err)
		}
		return r.Ok(result)
	case "office.artifact.preview":
		preview, err := s.PreviewPage(ctx, p.TaskID, vid, p.NodeOffset)
		if err != nil {
			return officeFailure(r, err)
		}
		nodes := make([]any, 0, len(preview.Nodes))
		for _, n := range preview.Nodes {
			row := map[string]any{"id": n.ID, "label": n.Part + " · " + n.Locator, "text": n.Text, "location": n.Part, "digest": n.Digest, "editable": n.Editable, "valueType": n.Kind}
			if n.Image != nil {
				row["image"] = n.Image
			}
			if n.Chart != nil {
				row["chart"] = n.Chart
			}
			nodes = append(nodes, row)
		}
		return r.Ok(map[string]any{"versionId": vid, "kind": preview.Kind, "content": preview.Content, "notice": preview.Notice, "nodes": nodes, "previewBasis": preview.PreviewBasis, "pdfReady": preview.PDFReady, "truncated": preview.Truncated, "parts": preview.Parts, "nodeOffset": preview.NodeOffset, "nextNodeOffset": preview.NextNodeOffset, "totalNodes": preview.TotalNodes})
	case "office.artifact.readChunk":
		b, err := s.ReadPDF(ctx, p.TaskID, vid)
		if err != nil {
			return officeFailure(r, err)
		}
		if p.Offset < 0 || p.Offset > int64(len(b)) || p.Limit < 0 || p.Limit > 32768 {
			return officeFailure(r, domain.ErrInvalid)
		}
		limit := p.Limit
		if limit == 0 {
			limit = 32768
		}
		end := p.Offset + limit
		if end > int64(len(b)) {
			end = int64(len(b))
		}
		return r.Ok(map[string]any{"contentBase64": base64.StdEncoding.EncodeToString(b[p.Offset:end]), "nextOffset": end, "total": len(b), "eof": end == int64(len(b))})
	case "office.artifact.patch":
		if failure := requireIdempotency(r); failure != nil {
			return *failure
		}
		if p.ExpectedRevision < 1 || p.NodeID == "" || len(p.NodeDigest) != 64 || len(p.Text) > 131072 {
			return officeFailure(r, domain.ErrInvalid)
		}
		err = s.Execute(ctx, p.TaskID, "running", func(run context.Context) error {
			_, patchErr := s.Patch(run, p.TaskID, vid, p.ExpectedRevision, content.PatchRequest{Kind: content.Kind(v.Kind), BaseSHA256: v.SHA256, Operations: []content.TextPatch{{NodeID: p.NodeID, ExpectedDigest: p.NodeDigest, Text: p.Text}}}, r.IdempotencyKey)
			return patchErr
		})
	case "office.artifact.replaceImage":
		if f := requireIdempotency(r); f != nil {
			return *f
		}
		if v.Kind != "pptx" || p.ExpectedRevision < 1 || p.NodeID == "" || len(p.NodeDigest) != 64 {
			return officeFailure(r, domain.ErrInvalid)
		}
		imageBytes, sourceErr := e.officeImageSource(ctx, task.SessionID, p.AttachmentID, p.SHA256)
		if sourceErr != nil {
			return officeFailure(r, sourceErr)
		}
		err = s.Execute(ctx, p.TaskID, "patch", func(run context.Context) error {
			_, er := s.Patch(run, p.TaskID, vid, p.ExpectedRevision, content.PatchRequest{Kind: content.PPTX, BaseSHA256: v.SHA256, Images: []content.ImagePatch{{NodeID: p.NodeID, ExpectedDigest: p.NodeDigest, SourceID: p.AttachmentID, SHA256: p.SHA256, Fit: p.Fit, Alt: p.Alt, Data: imageBytes}}}, r.IdempotencyKey)
			return er
		})
	case "office.artifact.patchRange":
		if f := requireIdempotency(r); f != nil {
			return *f
		}
		if v.Kind != "xlsx" || p.ExpectedRevision < 1 || len(p.Ranges) == 0 {
			return officeFailure(r, domain.ErrInvalid)
		}
		err = s.Execute(ctx, p.TaskID, "patch", func(run context.Context) error {
			_, er := s.Patch(run, p.TaskID, vid, p.ExpectedRevision, content.PatchRequest{Kind: content.XLSX, BaseSHA256: v.SHA256, Ranges: p.Ranges}, r.IdempotencyKey)
			return er
		})
	case "office.artifact.restore":
		if failure := requireIdempotency(r); failure != nil {
			return *failure
		}
		err = s.Execute(ctx, p.TaskID, "running", func(run context.Context) error {
			_, restoreErr := s.Restore(run, p.TaskID, vid, p.ExpectedRevision, r.IdempotencyKey)
			return restoreErr
		})
	case "office.artifact.accept":
		_, err = s.Store.AcceptOfficeVersion(ctx, p.TaskID, v.ArtifactID, vid, p.ExpectedRevision)
	case "office.artifact.validate":
		err = s.Execute(ctx, p.TaskID, "validating", func(run context.Context) error { _, checkErr := s.Check(run, p.TaskID, vid, true); return checkErr })
	case "office.artifact.export":
		if v.Quality != "passed" && !p.Draft {
			return r.Fail("OFFICE_DRAFT_REQUIRED", "此版本检查未全部完成，请选择导出草稿", false)
		}
		dir := filepath.Join(s.Root, "exports", p.TaskID)
		if err = os.MkdirAll(dir, 0700); err != nil {
			return officeFailure(r, err)
		}
		path, exportErr := s.Export(ctx, p.TaskID, vid, dir, p.Name)
		if exportErr != nil {
			return officeFailure(r, exportErr)
		}
		if canon, canonErr := canonpath.Canonical(path); canonErr == nil {
			path = canon
		}
		return r.Ok(map[string]any{"path": filepath.ToSlash(path), "absolutePath": filepath.ToSlash(path), "notice": "已导出可编辑副本；原始存档与接受状态保持不变"})
	default:
		return officeFailure(r, domain.ErrInvalid)
	}
	if err != nil {
		return officeFailure(r, err)
	}
	return e.officeDetailResponse(ctx, r, task)
}

func (e *Engine) officeTaskDTO(ctx context.Context, t domain.Task, projectIDs ...string) map[string]any {
	o := map[string]any{"id": t.ID, "sessionId": t.SessionID, "title": t.Title, "goal": t.Goal, "revision": t.Revision, "status": t.Status, "createdAt": t.CreatedAt, "updatedAt": t.UpdatedAt}
	if len(projectIDs) > 0 {
		if projectIDs[0] != "" {
			o["projectId"] = projectIDs[0]
		}
		return o
	}
	if pid, ok, err := projectIDForSession(e, ctx, t.SessionID); err == nil && ok {
		o["projectId"] = pid
	}
	return o
}

func (e *Engine) officeDetailResponse(ctx context.Context, r bridge.Request, knownTask domain.Task) bridge.Response {
	response := e.officeSnapshotDetail(ctx, r, knownTask.ID)
	if response.OK || r.Method == "office.task.get" {
		return response
	}
	return e.officeCommittedSnapshotFallback(ctx, r, knownTask)
}
