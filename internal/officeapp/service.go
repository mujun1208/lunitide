// Package officeapp manages Office snapshots beside the shared chat runtime.
// SQLite owns version/acceptance metadata; content-addressed files are durable
// before publication. A failed transaction may leave a harmless unreferenced
// blob, never a committed version whose bytes were not flushed.
package officeapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/officerender"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

type Service struct {
	Store          domain.Store
	Root           string
	Renderer       *officerender.Renderer
	mu             sync.Mutex
	runs           map[string]officeOperation
	recoveries     map[string]*officeRecovery
	StoragePolicy  domain.StoragePolicy
	storageInitMu  sync.Mutex
	storageReady   bool
	storageSweepAt time.Time
	storageClock   func() time.Time
	storageWrite   func([]byte, string) (string, error)
}

func New(store domain.Store, root string) (*Service, error) {
	if store == nil || !filepath.IsAbs(root) {
		return nil, domain.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Join(root, "blobs"), 0700); err != nil {
		return nil, err
	}
	s := &Service{Store: store, Root: root, runs: map[string]officeOperation{}}
	s.Renderer = &officerender.Renderer{Root: filepath.Join(root, "workers"), Preflight: func(kind string, data []byte) error {
		v, err := content.Inspect(content.Kind(kind), data)
		if err != nil {
			return err
		}
		if !v.RenderAllowed {
			return errors.New("文件含不支持的活动内容或外部引用，已保留原稿，仅开放结构检查")
		}
		return nil
	}}
	return s, nil
}

func digest(data []byte) string    { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func encode(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func safeName(name, kind string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "未命名." + kind
	}
	if len(name) > 240 || strings.ContainsAny(name, "/\\\x00<>:\"|?*") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return "", domain.ErrInvalid
	}
	if strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".") != kind {
		return "", domain.ErrInvalid
	}
	return name, nil
}

func (s *Service) read(ref string) ([]byte, error) {
	if len(ref) != 64 {
		return nil, domain.ErrInvalid
	}
	if _, err := hex.DecodeString(ref); err != nil {
		return nil, domain.ErrInvalid
	}
	r, err := os.OpenRoot(filepath.Join(s.Root, "blobs"))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := r.Open(ref)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, content.MaxInputBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > content.MaxInputBytes || digest(b) != ref {
		return nil, errors.New("存档完整性校验失败，请保留当前数据并重试")
	}
	return b, nil
}

func (s *Service) ReadVersion(ctx context.Context, taskID, versionID string) (domain.Version, []byte, error) {
	v, err := s.Store.GetOfficeVersion(ctx, versionID)
	if err != nil {
		return v, nil, err
	}
	if v.TaskID != taskID {
		return v, nil, domain.ErrScope
	}
	b, err := s.read(v.ContentRef)
	if err != nil {
		return v, nil, err
	}
	if digest(b) != v.SHA256 || int64(len(b)) != v.Size {
		return v, nil, errors.New("版本记录与文件摘要不一致")
	}
	return v, b, nil
}

func (s *Service) Import(ctx context.Context, taskID, artifactID, name string, data []byte, baseID string, revision int64, key string) (domain.Version, error) {
	kind := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	return s.publish(ctx, taskID, artifactID, name, kind, "imported", data, nil, baseID, revision, key)
}

func (s *Service) ImportLinked(ctx context.Context, taskID, artifactID, name, source string, data []byte, baseID string, revision int64, key string) (domain.Version, error) {
	kind := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	return s.publish(ctx, taskID, artifactID, name, kind, "imported", data, encode(map[string]string{"sourcePath": source}), baseID, revision, key)
}

func (s *Service) Generate(ctx context.Context, taskID, name string, spec content.Spec, key string) (domain.Version, error) {
	b, err := content.Generate(spec)
	if err != nil {
		return domain.Version{}, err
	}
	return s.publish(ctx, taskID, "", name, string(spec.Kind), "managed", b, encode(spec), "", 0, key)
}

func (s *Service) publish(ctx context.Context, taskID, artifactID, name, kind, mode string, data []byte, spec json.RawMessage, baseID string, revision int64, key string) (domain.Version, error) {
	if err := ctx.Err(); err != nil {
		return domain.Version{}, err
	}
	if _, err := s.Store.GetOfficeTask(ctx, taskID); err != nil {
		return domain.Version{}, err
	}
	name, err := safeName(name, kind)
	if err != nil {
		return domain.Version{}, err
	}
	inspection, err := content.Inspect(content.Kind(kind), data)
	if err != nil {
		return domain.Version{}, err
	}
	// Even unsafe imports remain inert snapshots. They are visibly blocked;
	// the renderer and patcher enforce their independent preflight gates.
	lease, err := s.putManaged(ctx, taskID, data)
	if err != nil {
		return domain.Version{}, err
	}
	defer s.releaseBlob(ctx, lease.ID)
	ref := lease.Digest
	if spec == nil {
		spec = json.RawMessage(`{}`)
	}
	// The package remains authoritative and is re-indexed for previews/patches.
	// Keep bounded metadata rather than copying a 50k-cell index into SQLite.
	storedIndex := inspection
	if len(storedIndex.Nodes) > 1000 {
		storedIndex.Nodes = storedIndex.Nodes[:1000]
	}
	if len(storedIndex.Preview) > 64000 {
		storedIndex.Preview = string([]rune(storedIndex.Preview)[:min(16000, len([]rune(storedIndex.Preview)))])
	}
	v := domain.Version{TaskID: taskID, ArtifactID: artifactID, Kind: kind, Name: name, ContentRef: ref, SHA256: ref, Size: int64(len(data)), ContentMode: mode, BaseVersionID: baseID, Spec: spec, Index: encode(storedIndex), Quality: "unverified", MediaType: mime(kind)}
	v, err = s.Store.PublishOfficeVersion(ctx, domain.PublishRequest{Version: v, ExpectedHeadRevision: revision, IdempotencyKey: key, CreatedBy: "office-studio", BlobLeaseID: lease.ID})
	if err != nil {
		return v, err
	}
	_, err = s.Check(ctx, taskID, v.ID, false)
	if err != nil {
		return v, fmt.Errorf("文件已存档，但检查记录失败：%w", err)
	}
	return s.Store.GetOfficeVersion(ctx, v.ID)
}

func mime(kind string) string {
	switch kind {
	case "pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case "docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case "pdf":
		return "application/pdf"
	}
	return "application/octet-stream"
}

func (s *Service) Patch(ctx context.Context, taskID, versionID string, revision int64, req content.PatchRequest, key string) (domain.Version, error) {
	v, b, err := s.ReadVersion(ctx, taskID, versionID)
	if err != nil {
		return v, err
	}
	if req.BaseSHA256 != v.SHA256 || string(req.Kind) != v.Kind {
		return v, domain.ErrConflict
	}
	result, err := content.Patch(b, req)
	if err != nil {
		return v, err
	}
	// A package text patch preserves the package as truth. Retaining a stale
	// managed Spec would silently undo the edit on later regeneration.
	report := encode(map[string]any{"changedParts": result.ChangedParts, "calculation": result.Calculation})
	return s.publish(ctx, taskID, v.ArtifactID, v.Name, v.Kind, "imported", result.Data, report, v.ID, revision, key)
}

func (s *Service) Restore(ctx context.Context, taskID, versionID string, revision int64, key string) (domain.Version, error) {
	v, b, err := s.ReadVersion(ctx, taskID, versionID)
	if err != nil {
		return v, err
	}
	return s.publish(ctx, taskID, v.ArtifactID, v.Name, v.Kind, v.ContentMode, b, v.Spec, v.ID, revision, key)
}

func (s *Service) Check(ctx context.Context, taskID, versionID string, render bool) (domain.Validation, error) {
	v, b, err := s.ReadVersion(ctx, taskID, versionID)
	if err != nil {
		return domain.Validation{}, err
	}
	local, err := content.Validate(content.Kind(v.Kind), b)
	if err != nil {
		return domain.Validation{}, err
	}
	checks := make([]domain.Check, 0, len(local.Checks)+3)
	for _, c := range local.Checks {
		// PDFs are already paginated source files, not Office documents that
		// need conversion in Word/PowerPoint/Excel to obtain a PDF preview.
		if v.Kind == "pdf" && c.ID == "native_render" {
			continue
		}
		status := c.Status
		if status == "blocked" {
			status = "failed"
		}
		if status == "missing" {
			status = "unsupported"
		}
		checks = append(checks, domain.Check{ID: c.ID, Label: officeCheckLabel(c.ID), Status: status, Required: true, Detail: c.Message})
	}
	evidence := map[string]any{"issues": local.Issues, "sourceDigest": v.SHA256}
	fontChecks, fontReport := s.fontChecks(ctx, v.Kind, b)
	if err := ctx.Err(); err != nil {
		return domain.Validation{}, err
	}
	checks = append(checks, fontChecks...)
	if fontReport != nil {
		evidence["fonts"] = fontReport
	}
	nativeCacheChecks(v, checks)
	var previewLease domain.BlobLease
	defer func() { s.releaseBlob(ctx, previewLease.ID) }()
	if render && v.Kind != "pdf" {
		result, renderErr := s.Renderer.RenderWithChecks(ctx, v.Kind, b, nativeOptions(v.Kind, checks))
		if renderErr != nil {
			if ctx.Err() != nil {
				return domain.Validation{}, ctx.Err()
			}
			status := "failed"
			if errors.Is(renderErr, officerender.ErrUnavailable) {
				status = "unsupported"
			}
			checks = append(checks, domain.Check{ID: "actual-render", Label: "实际排版", Status: status, Required: true, Detail: renderErr.Error()})
		} else {
			lease, putErr := s.putManaged(ctx, taskID, result.PDF)
			if putErr != nil {
				return domain.Validation{}, putErr
			}
			previewLease = lease
			evidence["pdfRef"] = lease.Digest
			evidence["renderer"] = result.Renderer
			evidence["rendererVersion"] = result.RendererVersion
			if result.Native != nil {
				evidence["native"] = result.Native
				checks = append(checks, nativePreviewChecks(*result.Native)...)
			}
			for i := range checks {
				if checks[i].ID == "native_render" {
					checks[i].Status = "passed"
					checks[i].Detail = "当前版本已由 " + result.Renderer + " 实际导出 PDF"
				}
			}
			checks = append(checks, domain.Check{ID: "actual-render", Label: "实际排版", Status: "passed", Required: true, Detail: result.Notice})
		}
	}
	if v.Kind != "pdf" {
		checks = append(checks, domain.Check{ID: "target-compatibility", Label: "Office/WPS 目标软件兼容性", Status: "unsupported", Required: true, Detail: "尚未在目标软件完成打开验证"})
	}
	qa := domain.Validation{VersionID: v.ID, SHA256: v.SHA256, Validator: "office-studio-go-v1", Quality: domain.QualityFor(checks), Checks: checks, Evidence: encode(evidence), BlobLeaseID: previewLease.ID}
	return s.Store.AddOfficeValidation(ctx, qa)
}

type Preview struct {
	VersionID      string         `json:"versionId"`
	Kind           string         `json:"kind"`
	Content        string         `json:"content"`
	Notice         string         `json:"notice"`
	Nodes          []content.Node `json:"nodes"`
	PreviewBasis   string         `json:"previewBasis"`
	PDFReady       bool           `json:"pdfReady"`
	Truncated      bool           `json:"truncated"`
	Parts          []content.Part `json:"parts"`
	NodeOffset     int            `json:"nodeOffset"`
	NextNodeOffset int            `json:"nextNodeOffset"`
	TotalNodes     int            `json:"totalNodes"`
}

func (s *Service) Preview(ctx context.Context, taskID, versionID string) (Preview, error) {
	return s.PreviewPage(ctx, taskID, versionID, 0)
}

func (s *Service) PreviewPage(ctx context.Context, taskID, versionID string, offset int) (Preview, error) {
	v, b, err := s.ReadVersion(ctx, taskID, versionID)
	if err != nil {
		return Preview{}, err
	}
	i, err := content.Inspect(content.Kind(v.Kind), b)
	if err != nil {
		return Preview{}, err
	}
	if offset < 0 || offset > len(i.Nodes) {
		return Preview{}, domain.ErrInvalid
	}
	p := Preview{VersionID: v.ID, Kind: v.Kind, Content: i.Preview, Nodes: []content.Node{}, Parts: []content.Part{}, PreviewBasis: "structure", Notice: "结构预览不代表真实分页与排版；检查结果仅针对当前版本", NodeOffset: offset, NextNodeOffset: offset, TotalNodes: len(i.Nodes)}
	// Leave room for the envelope and labels under the Host's 256KiB frame.
	// A node is always returned whole, so its edit target is never abbreviated.
	if len(p.Content) > 16000 {
		p.Content = string([]rune(p.Content)[:min(4000, len([]rune(p.Content)))])
		p.Truncated = true
	}
	budget := 0
	for _, node := range i.Nodes[offset:] {
		cost := len(encode(node))
		if budget+cost > 110000 || len(p.Nodes) >= 400 {
			p.Truncated = true
			break
		}
		p.Nodes = append(p.Nodes, node)
		p.NextNodeOffset++
		budget += cost
	}
	if len(p.Nodes) == 0 && p.NextNodeOffset < len(i.Nodes) {
		// A single oversized paragraph remains addressable, but must not be
		// editable from an abbreviated value. Move past it on the next page.
		n := i.Nodes[offset]
		n.Text = string([]rune(n.Text)[:min(4000, len([]rune(n.Text)))])
		n.Editable = false
		p.Nodes = append(p.Nodes, n)
		p.NextNodeOffset++
		p.Truncated = true
	}
	for _, part := range i.Parts {
		if strings.HasPrefix(part.Name, "xl/worksheets/") {
			p.Parts = append(p.Parts, part)
			if len(p.Parts) >= 128 {
				p.Truncated = true
				break
			}
		}
	}
	if p.Truncated {
		p.Notice += "；内容较长，仅显示部分结构，完整内容保存在文件中，可打开副本查看"
	}
	if v.Kind == "pdf" {
		p.PDFReady = true
	} else {
		items, e := s.Store.ListOfficeValidations(ctx, v.ID)
		if e != nil {
			return p, e
		}
		for _, q := range items {
			var evidence map[string]any
			if json.Unmarshal(q.Evidence, &evidence) == nil {
				if ref, ok := evidence["pdfRef"].(string); ok && ref != "" {
					p.PDFReady = true
					break
				}
			}
		}
	}
	return p, nil
}

func (s *Service) ReadPDF(ctx context.Context, taskID, versionID string) ([]byte, error) {
	v, b, err := s.ReadVersion(ctx, taskID, versionID)
	if err != nil {
		return nil, err
	}
	if v.Kind == "pdf" {
		return b, nil
	}
	items, err := s.Store.ListOfficeValidations(ctx, v.ID)
	if err != nil {
		return nil, err
	}
	for _, q := range items {
		var evidence map[string]any
		if json.Unmarshal(q.Evidence, &evidence) == nil {
			if ref, ok := evidence["pdfRef"].(string); ok && ref != "" {
				return s.read(ref)
			}
		}
	}
	return nil, domain.ErrNotFound
}

func (s *Service) Export(ctx context.Context, taskID, versionID, dir string, exportNames ...string) (string, error) {
	v, b, err := s.ReadVersion(ctx, taskID, versionID)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(dir) {
		return "", domain.ErrInvalid
	}
	if len(exportNames) > 0 && strings.TrimSpace(exportNames[0]) != "" {
		v.Name, err = safeName(exportNames[0], v.Kind)
		if err != nil {
			return "", err
		}
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer r.Close()
	name := strings.TrimSuffix(v.Name, filepath.Ext(v.Name)) + fmt.Sprintf("-v%d-%s", v.VersionNo, v.ID[len(v.ID)-6:]) + filepath.Ext(v.Name)
	if _, err = writeBundleFile(ctx, r, name, b); err != nil {
		return "", err
	}
	if err = s.exportReceipt(ctx, v, dir, name); err != nil {
		return "", fmt.Errorf("副本已导出，但回执保存失败：%w", err)
	}
	return filepath.Join(dir, name), nil
}

func (s *Service) exportReceipt(ctx context.Context, v domain.Version, dir, name string) error {
	task, err := s.Store.GetOfficeTask(ctx, v.TaskID)
	if err != nil {
		return err
	}
	runID := task.RunID
	if runID == "" {
		runID = task.ID
	}
	receipt := domain.StepReceipt{TaskID: v.TaskID, RunID: runID, StepKey: "export", IdempotencyKey: "export-" + digest([]byte(filepath.Join(dir, name))), InputDigest: v.SHA256, State: "succeeded", Result: encode(map[string]string{"path": filepath.Join(dir, name), "sha256": v.SHA256}), CreatedAt: time.Now().UTC()}
	return s.Store.AppendOfficeStepReceipt(ctx, receipt)
}
