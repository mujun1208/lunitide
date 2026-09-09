package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/officeapp"
)

type officeDeliveryPayload struct {
	TaskID           string   `json:"taskId"`
	VersionID        string   `json:"versionId"`
	NodeID           string   `json:"nodeId"`
	NodeDigest       string   `json:"nodeDigest"`
	Name             string   `json:"name"`
	Unit             string   `json:"unit"`
	Currency         string   `json:"currency"`
	Period           string   `json:"period"`
	RoundingDigits   *int     `json:"roundingDigits,omitempty"`
	MetricID         string   `json:"metricId"`
	TargetVersionID  string   `json:"targetVersionId"`
	TargetNodeID     string   `json:"targetNodeId"`
	TargetNodeDigest string   `json:"targetNodeDigest"`
	Template         string   `json:"template"`
	ExpectedRevision int64    `json:"expectedRevision"`
	Title            string   `json:"title"`
	VersionIDs       []string `json:"versionIds"`
	BundleID         string   `json:"bundleId"`
	SnapshotOffset   int      `json:"snapshotOffset"`
	SnapshotDigest   string   `json:"snapshotDigest"`
}

func handleOfficeDelivery(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.officeStudio == nil {
		return r.Fail("FEATURE_DISABLED", "办公工作台尚未初始化", false)
	}
	var p officeDeliveryPayload
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.TaskID) {
		return officeFailure(r, domain.ErrInvalid)
	}
	org, _, err := e.boundOrgState(ctx)
	if err != nil {
		return officeFailure(r, err)
	}
	ctx = domain.WithScope(ctx, org)
	s := e.officeStudio
	if err = s.RecoverOnce(ctx); err != nil {
		return officeFailure(r, err)
	}
	task, err := s.Store.GetOfficeTask(ctx, p.TaskID)
	if err != nil {
		return officeFailure(r, err)
	}
	write := !strings.HasSuffix(r.Method, ".list") && r.Method != "office.bundle.export"
	if write {
		if f := requireIdempotency(r); f != nil {
			return *f
		}
		if !e.officeWritesEnabled(r.Method, "") {
			return r.Fail("FEATURE_DISABLED", "此办公写入能力已关闭", false)
		}
		if pid, ok, er := projectIDForSession(e, ctx, task.SessionID); er != nil {
			return officeFailure(r, er)
		} else if ok {
			if f := rejectIfProjectReadOnly(e, ctx, r, pid); f != nil {
				return *f
			}
		}
	}
	switch r.Method {
	case "office.metric.list":
		items, err := s.ListMetrics(ctx, task.ID)
		if err != nil {
			return officeFailure(r, err)
		}
		return officeSnapshotMetrics(r, items)
	case "office.bundle.list":
		items, err := s.ListBundles(ctx, task.ID)
		if err != nil {
			return officeFailure(r, err)
		}
		return officeSnapshotBundles(r, items)
	case "office.metric.capture":
		_, err = s.CaptureMetric(ctx, task.ID, officeapp.MetricCapture{SourceVersionID: p.VersionID, SourceNodeID: p.NodeID, SourceNodeDigest: p.NodeDigest, Name: p.Name, Unit: p.Unit, Currency: p.Currency, Period: p.Period, RoundingDigits: p.RoundingDigits}, r.IdempotencyKey)
	case "office.metric.apply":
		err = e.officeExclusive(ctx, task.ID, "patch", func(run context.Context) error {
			v, _, er := s.ReadVersion(run, task.ID, p.TargetVersionID)
			if er != nil {
				return er
			}
			if !e.officeWritesEnabled("office.patch", v.Kind) {
				return domain.ErrInvalid
			}
			_, er = s.ApplyMetric(run, task.ID, officeapp.MetricApply{MetricID: p.MetricID, TargetVersionID: p.TargetVersionID, TargetNodeID: p.TargetNodeID, TargetNodeDigest: p.TargetNodeDigest, Template: p.Template, ExpectedRevision: p.ExpectedRevision}, r.IdempotencyKey)
			return er
		})
	case "office.bundle.create":
		bundle, err := s.CreateBundle(ctx, task.ID, p.Title, p.VersionIDs, r.IdempotencyKey)
		if err != nil {
			return officeFailure(r, err)
		}
		return r.Ok(bundle)
	case "office.bundle.export":
		dir := filepath.Join(s.Root, "exports", task.ID)
		if err = os.MkdirAll(dir, 0700); err != nil {
			return officeFailure(r, err)
		}
		out, err := s.ExportBundle(ctx, task.ID, p.BundleID, dir)
		if err != nil {
			return officeFailure(r, err)
		}
		return r.Ok(out)
	default:
		return officeFailure(r, domain.ErrInvalid)
	}
	if err != nil {
		return officeFailure(r, err)
	}
	return e.officeDetailResponse(ctx, r, task)
}
