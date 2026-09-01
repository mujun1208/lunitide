package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/complexity"
	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/m6app"
	"github.com/lunitide/lunitide/internal/openapi"
)

// M6 S5C governance handlers (0053): openapi.parse, complexity.decide and
// the skill.import.* pipeline.
//
// Error mapping follows M6_ERROR_CATALOG_V2:
//
//	openapi.parse          OAS-001 size/compression bomb, OAS-002 $ref
//	                       depth/cycle/out-of-document target, OAS-003
//	                       unique paths over 500
//	skill.import.*         SKL-001 manifest missing fields or signature
//	                       failure (approving a skill candidate with an
//	                       unparsable manifest); candidate CAS conflicts
//	                       carry the family-specific version code.
//	complexity.decide      deterministic replay of the same signal digest
//	                       answers the stored decision (no double audit).

// handleOpenapiParse runs the offline OpenAPI 3.0 consumer-subset parser.
// The parser never touches the network; every $ref must resolve inside the
// document, so OAS-002 also covers remote targets.
func handleOpenapiParse(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Spec            string `json:"spec"`
		ContentEncoding string `json:"contentEncoding"`
		Name            string `json:"name"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.Spec) < 100 || len(p.Spec) > 5242880 ||
		len(p.Name) > 256 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "openapi.parse 参数无效", false)
	}
	switch p.ContentEncoding {
	case "", "identity", "gzip", "deflate":
	default:
		return r.Fail("BRIDGE_SCHEMA_INVALID", "openapi.parse 参数无效", false)
	}
	spec, err := openapi.ParseEncoded([]byte(p.Spec), p.ContentEncoding)
	if err != nil {
		return openapiFailure(r, err)
	}
	authTypes := make([]string, 0, len(spec.AuthTypes))
	for _, at := range spec.AuthTypes {
		authTypes = append(authTypes, string(at))
	}
	return r.Ok(struct {
		Digest         string   `json:"digest"`
		OpenAPI        string   `json:"openapi"`
		SpecVersion    string   `json:"specVersion"`
		Title          string   `json:"title,omitempty"`
		OperationCount int      `json:"operationCount"`
		AuthTypes      []string `json:"authTypes"`
	}{spec.Digest, spec.OpenAPI, spec.SpecVersion, spec.Title, len(spec.Operations), authTypes})
}

// openapiFailure maps parser catalog codes onto the wire verbatim.
func openapiFailure(r bridge.Request, err error) bridge.Response {
	var oasErr *openapi.Error
	if errors.As(err, &oasErr) {
		return r.Fail(oasErr.Code, "OpenAPI 文档被拒绝："+oasErr.Message, false)
	}
	return r.Fail("BRIDGE_SCHEMA_INVALID", "OpenAPI 文档无法解析", false)
}

// handleComplexityDecide routes one conversation snapshot through the
// complexity router. Identical signal digests replay the stored decision.
func handleComplexityDecide(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string                         `json:"sessionId"`
		Signals   complexity.ConversationSignals `json:"signals"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.SessionID) < 1 || len(p.SessionID) > 256 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "complexity.decide 参数无效", false)
	}
	if p.Signals.MessageCount < 0 || p.Signals.DelegationHints < 0 || p.Signals.EstTokens < 0 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "complexity.decide 参数无效", false)
	}
	if e.m6routing == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "复杂度路由服务暂时不可用", true)
	}
	decision, err := e.m6routing.Decide(ctx, p.SessionID, p.Signals)
	if err != nil {
		if errors.Is(err, m6app.ErrServiceUnavailable) {
			return r.Fail("STORAGE_UNAVAILABLE", "复杂度路由服务暂时不可用", true)
		}
		return r.Fail("BRIDGE_SCHEMA_INVALID", "复杂度决策无效", false)
	}
	var reasons []string
	if err := json.Unmarshal([]byte(decision.ReasonCodes), &reasons); err != nil || reasons == nil {
		reasons = []string{}
	}
	return r.Ok(struct {
		DecisionID  string   `json:"decisionId"`
		Tier        string   `json:"tier"`
		RoutedPath  string   `json:"routedPath"`
		ReasonCodes []string `json:"reasonCodes"`
		Confidence  float64  `json:"confidence"`
	}{decision.ID, decision.Tier, decision.RoutedPath, reasons, decision.Confidence})
}

// handleSkillImportDiscover opens the governed import pipeline.
func handleSkillImportDiscover(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		AssetType       string `json:"assetType"`
		SourceURL       string `json:"sourceUrl"`
		ImmutableCommit string `json:"immutableCommit"`
		ArchiveHash     string `json:"archiveHash"`
		License         string `json:"license"`
		NoticeRef       string `json:"noticeRef"`
		Publisher       string `json:"publisher"`
		Signature       string `json:"signature"`
	}
	if decodePayload(r.Payload, &p) != nil ||
		(p.AssetType != m6supply.AssetSkill && p.AssetType != m6supply.AssetProfile && p.AssetType != m6supply.AssetPromptBundle) ||
		len(p.SourceURL) < 1 || len(p.SourceURL) > 2048 ||
		len(p.ImmutableCommit) < 1 || len(p.ImmutableCommit) > 256 ||
		!validLowerHexDigest(p.ArchiveHash) ||
		len(p.License) < 1 || len(p.License) > 128 ||
		len(p.NoticeRef) > 512 ||
		len(p.Publisher) < 1 || len(p.Publisher) > 256 ||
		len(p.Signature) > 8192 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "skill.import.discover 参数无效", false)
	}
	if e.m6skills == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "技能导入服务暂时不可用", true)
	}
	c, err := e.m6skills.Discover(ctx, m6app.DiscoverInput{
		AssetType: p.AssetType, SourceURL: p.SourceURL, ImmutableCommit: p.ImmutableCommit,
		ArchiveHash: p.ArchiveHash, License: p.License, NoticeRef: p.NoticeRef,
		Publisher: p.Publisher, Signature: p.Signature,
	})
	if err != nil {
		return skillImportFailure(r, err)
	}
	return skillCandidateSuccess(r, c)
}

// handleSkillImportInspect walks pinned -> inspected, attaching the
// license/notice/signature evidence.
func handleSkillImportInspect(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p skillImportStepPayload
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.CandidateID) || p.ExpectedVersion < 1 ||
		len(p.NoticeRef) > 512 || len(p.Signature) > 8192 || len(p.SourceAttestation) > 16384 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "skill.import.inspect 参数无效", false)
	}
	if e.m6skills == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "技能导入服务暂时不可用", true)
	}
	c, err := e.m6skills.Pin(ctx, p.CandidateID, p.ExpectedVersion, m6supply.ImportEvidence{})
	if err != nil {
		return skillImportFailure(r, err)
	}
	c, err = e.m6skills.Inspect(ctx, p.CandidateID, c.Version, m6supply.ImportEvidence{
		NoticeRef: p.NoticeRef, Signature: p.Signature, SourceAttestation: p.SourceAttestation,
	})
	if err != nil {
		return skillImportFailure(r, err)
	}
	return skillCandidateSuccess(r, c)
}

type skillImportStepPayload struct {
	CandidateID       string `json:"candidateId"`
	ExpectedVersion   int64  `json:"expectedVersion"`
	NoticeRef         string `json:"noticeRef"`
	Signature         string `json:"signature"`
	SourceAttestation string `json:"sourceAttestation"`
	ScanRefs          string `json:"scanRefs"`
	InjectionScan     string `json:"injectionScan"`
	EvaluationID      string `json:"evaluationId"`
	Reason            string `json:"reason"`
}

// handleSkillImportSubmit walks scanned -> evaluated -> awaiting_approval
// with the scan, injection-scan and sandbox evaluation evidence. Each hop
// is its own audited CAS step.
func handleSkillImportSubmit(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p skillImportStepPayload
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.CandidateID) || p.ExpectedVersion < 1 ||
		!jsonArrayString(p.ScanRefs, 16384) || !jsonObjectString(p.InjectionScan, 16384) ||
		len(p.EvaluationID) < 1 || len(p.EvaluationID) > 256 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "skill.import.submit 参数无效", false)
	}
	if e.m6skills == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "技能导入服务暂时不可用", true)
	}
	c, err := e.m6skills.Scan(ctx, p.CandidateID, p.ExpectedVersion, m6supply.ImportEvidence{
		ScanRefs: p.ScanRefs, InjectionScan: p.InjectionScan,
	})
	if err != nil {
		return skillImportFailure(r, err)
	}
	c, err = e.m6skills.Evaluate(ctx, p.CandidateID, c.Version, m6supply.ImportEvidence{
		EvaluationID: p.EvaluationID,
	})
	if err != nil {
		return skillImportFailure(r, err)
	}
	c, err = e.m6skills.Submit(ctx, p.CandidateID, c.Version, m6supply.ImportEvidence{})
	if err != nil {
		return skillImportFailure(r, err)
	}
	return skillCandidateSuccess(r, c)
}

// handleSkillImportApprove is the install gate: awaiting_approval ->
// approved, materializing the skill entity chain for skill assets. A
// manifest that fails validation answers M6-SKL-001 and the candidate
// stays awaiting_approval (never quarantined product state).
func handleSkillImportApprove(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		CandidateID     string          `json:"candidateId"`
		ExpectedVersion int64           `json:"expectedVersion"`
		Approval        json.RawMessage `json:"approval"`
		Manifest        string          `json:"manifest"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.CandidateID) || p.ExpectedVersion < 1 ||
		len(p.Approval) < 3 || len(p.Approval) > 65536 || len(p.Manifest) > 262144 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "skill.import.approve 参数无效", false)
	}
	if e.m6skills == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "技能导入服务暂时不可用", true)
	}
	c, err := e.m6skills.Approve(ctx, m6app.ApproveInput{
		CandidateID: p.CandidateID, ExpectedVersion: p.ExpectedVersion,
		Approval: string(p.Approval), Manifest: []byte(p.Manifest),
	})
	if err != nil {
		return skillImportFailure(r, err)
	}
	return skillCandidateSuccess(r, c)
}

// handleSkillImportReject terminates the walk from any pre-approval state.
func handleSkillImportReject(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	return skillImportTerminalStep(e, ctx, r, "reject")
}

// handleSkillImportRevoke is the terminal cleanup after approval or
// rejection.
func handleSkillImportRevoke(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	return skillImportTerminalStep(e, ctx, r, "revoke")
}

func skillImportTerminalStep(e *Engine, ctx context.Context, r bridge.Request, op string) bridge.Response {
	var p skillImportStepPayload
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.CandidateID) || p.ExpectedVersion < 1 ||
		len(p.Reason) < 1 || len(p.Reason) > 2048 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "skill.import."+op+" 参数无效", false)
	}
	if e.m6skills == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "技能导入服务暂时不可用", true)
	}
	evidence := m6supply.ImportEvidence{
		Approval: `{"reason":` + jsonString(p.Reason) + `}`,
	}
	var (
		c   m6supply.ImportCandidate
		err error
	)
	if op == "reject" {
		c, err = e.m6skills.Reject(ctx, p.CandidateID, p.ExpectedVersion, evidence)
	} else {
		c, err = e.m6skills.Revoke(ctx, p.CandidateID, p.ExpectedVersion, evidence)
	}
	if err != nil {
		return skillImportFailure(r, err)
	}
	return skillCandidateSuccess(r, c)
}

func skillCandidateSuccess(r bridge.Request, c m6supply.ImportCandidate) bridge.Response {
	return r.Ok(struct {
		CandidateID string `json:"candidateId"`
		State       string `json:"state"`
		Version     int64  `json:"version"`
	}{c.ID, c.State, c.Version})
}

// skillImportFailure maps pipeline errors onto the wire.
func skillImportFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, m6app.ErrCandidateNotFound):
		return r.Fail("SKILL_CANDIDATE_NOT_FOUND", "导入候选不存在", false)
	case errors.Is(err, m6app.ErrCandidateExists):
		return r.Fail("SKILL_CANDIDATE_EXISTS", "同源同提交的候选已存在", false)
	case errors.Is(err, m6supply.ErrVersionConflict):
		return r.Fail("SKILL_VERSION_CONFLICT", "候选版本已变化，请刷新后重试", false)
	case errors.Is(err, m6supply.ErrInvalidTransition):
		return r.Fail("SKILL_INVALID_TRANSITION", "候选状态不支持该操作", false)
	case errors.Is(err, m6app.ErrSkillVersionExists):
		return r.Fail("M6-SKL-001", "技能版本已存在或清单校验失败", false)
	case errors.Is(err, m6app.ErrPromptBundleVersionExists):
		return r.Fail("M6-SKL-001", "提示词包版本已存在或清单校验失败", false)
	case isManifestError(err):
		return r.Fail("M6-SKL-001", "技能清单缺失字段或校验失败", false)
	case errors.Is(err, m6app.ErrServiceUnavailable):
		return r.Fail("STORAGE_UNAVAILABLE", "技能导入服务暂时不可用", true)
	default:
		return r.Fail("STORAGE_UNAVAILABLE", "技能导入服务暂时不可用", true)
	}
}

// isManifestError reports whether err originates from manifest parsing or
// the manifest/publisher consistency check (all prefixed "m6app:" or
// returned by m6supply.ParseManifest).
func isManifestError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, m6supply.ErrManifestInvalid) {
		return true
	}
	msg := err.Error()
	return strings.HasPrefix(msg, "m6supply: manifest") || strings.Contains(msg, "manifest publisher")
}

// jsonArrayString checks s is a JSON array within the size bound.
func jsonArrayString(s string, maxLen int) bool {
	if len(s) < 2 || len(s) > maxLen || !json.Valid([]byte(s)) {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(s), "[")
}

// jsonObjectString checks s is a JSON object within the size bound.
func jsonObjectString(s string, maxLen int) bool {
	if len(s) < 2 || len(s) > maxLen || !json.Valid([]byte(s)) {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(s), "{")
}

// jsonString marshals one string for embedding in a JSON literal.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
