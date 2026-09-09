package officeapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/officerender"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

type nativeRefreshAudit struct {
	BaseVersionID    string                      `json:"refreshBaseVersionId"`
	ExpectedRevision int64                       `json:"refreshExpectedRevision"`
	CacheMerge       content.CacheMerge          `json:"cacheMerge"`
	Native           officerender.NativeEvidence `json:"native"`
	Renderer         string                      `json:"renderer"`
	RendererVersion  string                      `json:"rendererVersion"`
}

// RefreshNativeCaches publishes only the independently merged cache deltas.
// The converted Office file is private evidence, not the replacement source.
func (s *Service) RefreshNativeCaches(ctx context.Context, taskID, versionID string, revision int64, key string) (domain.Version, error) {
	if store, ok := s.Store.(interface {
		FindOfficePublishedVersion(context.Context, string, string) (domain.Version, error)
	}); ok {
		old, err := store.FindOfficePublishedVersion(ctx, taskID, key)
		if err == nil {
			var audit nativeRefreshAudit
			if json.Unmarshal(old.Spec, &audit) != nil || audit.BaseVersionID != versionID || audit.ExpectedRevision != revision || audit.CacheMerge.OutputSHA256 != old.SHA256 {
				return domain.Version{}, domain.ErrConflict
			}
			verified, _, readErr := s.ReadVersion(ctx, taskID, old.ID)
			if readErr == nil {
				checks, checkErr := s.Store.ListOfficeValidations(ctx, old.ID)
				if checkErr != nil {
					return verified, checkErr
				}
				if len(checks) == 0 {
					_, readErr = s.Check(ctx, taskID, old.ID, false)
					if readErr == nil {
						verified, readErr = s.Store.GetOfficeVersion(ctx, old.ID)
					}
				}
			}
			return verified, readErr
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return domain.Version{}, err
		}
	}
	v, source, err := s.ReadVersion(ctx, taskID, versionID)
	if err != nil {
		return v, err
	}
	if revision < 1 || (v.Kind != "docx" && v.Kind != "xlsx") {
		return v, domain.ErrInvalid
	}
	heads, err := s.Store.ListOfficeHeads(ctx, taskID)
	if err != nil {
		return v, err
	}
	current := false
	for _, head := range heads {
		if head.ArtifactID == v.ArtifactID && head.Revision == revision {
			current = true
			break
		}
	}
	if !current {
		return v, domain.ErrConflict
	}
	options := officerender.NativeOptions{UpdateFields: v.Kind == "docx", Recalculate: v.Kind == "xlsx", ExportUpdatedCopy: true}
	result, err := s.Renderer.RenderWithChecks(ctx, v.Kind, source, options)
	if err != nil {
		return v, err
	}
	if result.Native == nil || !result.Native.SourceUnchanged || result.SourceDigest != v.SHA256 {
		return v, fmt.Errorf("原生更新回执无法绑定原文件")
	}
	if v.Kind == "xlsx" && (!result.Native.Recalculated || !result.Native.FormulaScanFull || result.Native.FormulaErrorCount != 0) {
		detail := "原生公式重算未全部通过，原版本未改写。"
		if checks := nativePreviewChecks(*result.Native); len(checks) > 0 {
			detail += checks[len(checks)-1].Detail
		}
		return v, fmt.Errorf("%s", detail)
	}
	if v.Kind == "docx" && !result.Native.FieldsRefreshed {
		return v, fmt.Errorf("目录与页码尚未成功更新")
	}
	merged, err := content.MergeNativeCaches(content.Kind(v.Kind), source, result.UpdatedOffice)
	if err != nil {
		return v, err
	}
	if v.Kind == "xlsx" && merged.UpdatedCells != result.Native.FormulaCells {
		return v, fmt.Errorf("原生公式扫描数量与实际更新数量不一致")
	}
	audit := nativeRefreshAudit{BaseVersionID: versionID, ExpectedRevision: revision, CacheMerge: merged, Native: *result.Native, Renderer: result.Renderer, RendererVersion: result.RendererVersion}
	return s.publish(ctx, taskID, v.ArtifactID, v.Name, v.Kind, "imported", merged.Data, encode(audit), v.ID, revision, key)
}

func nativeCacheChecks(v domain.Version, checks []domain.Check) {
	var audit nativeRefreshAudit
	if json.Unmarshal(v.Spec, &audit) != nil || audit.CacheMerge.OutputSHA256 != v.SHA256 || !audit.Native.SourceUnchanged {
		return
	}
	for i := range checks {
		c := &checks[i]
		if c.ID == "full_recalculation" && audit.Native.Recalculated && audit.Native.FormulaScanFull && audit.Native.FormulaErrorCount == 0 && audit.CacheMerge.UpdatedCells == audit.Native.FormulaCells && audit.CacheMerge.UpdatedCells > 0 {
			c.Status = "passed"
			c.Detail = fmt.Sprintf("已将 %d 个真实重算缓存写入新版本，保留原公式、输入单元格、样式和其他部件；计算器 %s %s。", audit.CacheMerge.UpdatedCells, audit.Renderer, audit.RendererVersion)
		}
		if c.ID == "fields_update" && audit.Native.FieldsRefreshed && audit.CacheMerge.UpdatedFields > 0 {
			c.Status = "passed"
			c.Detail = fmt.Sprintf("已按真实原生结果更新 %d 个简单域的显示缓存，保留域代码和其他正文；新版本仍需排版检查，目标软件打开后可刷新动态页码。", audit.CacheMerge.UpdatedFields)
		}
	}
}
