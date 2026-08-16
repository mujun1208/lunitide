package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

// M7 slice-3 handlers (T-7.3.x): release.createRevision / release.buildPackage
// / release.getRevision / release.getPackage.
//
// Error mapping follows the M7 wire contract: digest mismatches and missing
// evidence isolate the package via M7-PKG-002, malformed manifests, SBOM or
// signatures block promotion via M7-PKG-003, closed revisions answer
// M7-REV-002 (create a new revision instead) and forged review authors answer
// M7-REV-001 (SoD).

func handleReleaseCreateRevision(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		CRID      string         `json:"crId"`
		Manifest  map[string]any `json:"manifest"`
		RequestID string         `json:"requestId"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.CRID) < 1 || len(p.CRID) > 128 ||
		len(p.Manifest) < 2 || len(p.Manifest) > 64 ||
		len(p.RequestID) < 1 || len(p.RequestID) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "release.createRevision 参数无效", false)
	}
	if e.m7release == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "发行服务暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	rev, err := e.m7release.CreateRevision(ctx, p.CRID, p.Manifest)
	if err != nil {
		return m7ReleaseFailure(r, err, "release.createRevision")
	}
	return bridge.Success(r.ID, struct {
		CRRevisionID string `json:"crRevisionId"`
		RevisionNo   int64  `json:"revisionNo"`
		Digest       string `json:"digest"`
	}{rev.ID, rev.RevisionNo, rev.Digest})
}

func handleReleaseBuildPackage(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		CRRevisionID   string `json:"crRevisionId"`
		ExpectedDigest string `json:"expectedDigest"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.CRRevisionID) || !m7TraceDigest(p.ExpectedDigest) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "release.buildPackage 参数无效", false)
	}
	if e.m7release == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "发行服务暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	pkg, err := e.m7release.BuildPackage(ctx, p.CRRevisionID, p.ExpectedDigest)
	if err != nil {
		return m7ReleaseFailure(r, err, "release.buildPackage")
	}
	return bridge.Success(r.ID, struct {
		PackageID      string `json:"packageId"`
		ManifestDigest string `json:"manifestDigest"`
		BlobDigest     string `json:"blobDigest"`
	}{pkg.ID, pkg.ManifestDigest, pkg.BlobDigest})
}

func handleReleaseGetRevision(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		CRID       string `json:"crId"`
		RevisionNo int64  `json:"revisionNo"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.CRID) < 1 || len(p.CRID) > 128 || p.RevisionNo < 0 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "release.getRevision 参数无效", false)
	}
	if e.m7release == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "发行服务暂时不可用", true)
	}
	view, err := e.m7release.GetRevision(ctx, p.CRID, p.RevisionNo)
	if err != nil {
		return m7ReleaseFailure(r, err, "release.getRevision")
	}
	manifest := view.Manifest
	if manifest == nil {
		manifest = map[string]any{}
	}
	reviews := make([]m7ReviewDTO, 0, len(view.Reviews))
	for _, rv := range view.Reviews {
		reviews = append(reviews, m7ReviewDTO{
			SubjectType: rv.SubjectType, SubjectID: rv.SubjectID,
			SubjectVersion: rv.SubjectVersion, Verdict: rv.Verdict,
			ReviewerID: rv.ReviewerID, Reason: rv.Reason,
			CreatedAt: rv.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		})
	}
	return bridge.Success(r.ID, struct {
		Revisions []m7app.RevisionSummary `json:"revisions"`
		Manifest  map[string]any          `json:"manifest"`
		Reviews   []m7ReviewDTO           `json:"reviews"`
	}{view.Revisions, manifest, reviews})
}

// m7ReviewDTO is the wire form of one append-only review row.
type m7ReviewDTO struct {
	SubjectType    string `json:"subjectType"`
	SubjectID      string `json:"subjectId"`
	SubjectVersion int64  `json:"subjectVersion"`
	Verdict        string `json:"verdict"`
	ReviewerID     string `json:"reviewerId"`
	Reason         string `json:"reason"`
	CreatedAt      string `json:"createdAt"`
}

// releasePackageDTO is the wire form of one verified release package.
type releasePackageDTO struct {
	ID             string                 `json:"id"`
	CRRevisionID   string                 `json:"crRevisionId"`
	ManifestDigest string                 `json:"manifestDigest"`
	BlobDigest     string                 `json:"blobDigest"`
	Signature      string                 `json:"signature"`
	State          string                 `json:"state"`
	Verified       bool                   `json:"verified"`
	SealedAt       string                 `json:"sealedAt,omitempty"`
	MemberDigests  []m7flow.PackageMember `json:"memberDigests"`
}

func handleReleaseGetPackage(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PackageID string `json:"packageId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.PackageID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "release.getPackage 参数无效", false)
	}
	if e.m7release == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "发行服务暂时不可用", true)
	}
	view, err := e.m7release.GetPackage(ctx, p.PackageID)
	if err != nil {
		return m7ReleaseFailure(r, err, "release.getPackage")
	}
	members := view.MemberDigests
	if members == nil {
		members = []m7flow.PackageMember{}
	}
	pkg := releasePackageDTO{
		ID: view.ID, CRRevisionID: view.CRRevisionID,
		ManifestDigest: view.ManifestDigest, BlobDigest: view.BlobDigest,
		Signature: view.Signature, State: view.State,
		Verified: view.Verified, SealedAt: view.SealedAt,
		MemberDigests: members,
	}
	return bridge.Success(r.ID, struct {
		Package releasePackageDTO `json:"package"`
		SBOM    *m7flow.SBOMRef   `json:"sbom,omitempty"`
	}{pkg, view.SBOM})
}

// m7ReleaseFailure maps m7app slice-3 errors onto the M7 wire family.
func m7ReleaseFailure(r bridge.Request, err error, method string) bridge.Response {
	switch {
	case errors.Is(err, m7app.ErrRevisionNotFound), errors.Is(err, m7app.ErrPackageNotFound):
		return bridge.Failure(r.ID, r.TraceID, "NOT_FOUND", "发行对象不存在", false)
	case errors.Is(err, m7app.ErrDigestMismatch), errors.Is(err, m7app.ErrEvidenceMissing):
		return bridge.Failure(r.ID, r.TraceID, "M7-PKG-002", "摘要校验失败，发行包已被隔离", false)
	case errors.Is(err, m7app.ErrPackageInvalid):
		return bridge.Failure(r.ID, r.TraceID, "M7-PKG-003", "发行清单或 SBOM 不合规，禁止晋级", false)
	case errors.Is(err, m7app.ErrSignatureInvalid):
		return bridge.Failure(r.ID, r.TraceID, "M7-PKG-003", "发行包签名校验失败，禁止晋级", false)
	case errors.Is(err, m7app.ErrRevisionFrozen), errors.Is(err, m7app.ErrIllegalRevisionTransition):
		return bridge.Failure(r.ID, r.TraceID, "M7-REV-002", "修订已关闭不可变更，请新建修订", false)
	case errors.Is(err, m7app.ErrAuthorMismatch):
		return bridge.Failure(r.ID, r.TraceID, "M7-REV-001", "评审作者与修订作者不匹配", false)
	case errors.Is(err, m7app.ErrServiceUnavailable):
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "发行服务暂时不可用", true)
	}
	return bridge.Failure(r.ID, r.TraceID, "INTERNAL_ERROR", method+" 执行失败", false)
}
