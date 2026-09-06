package app

import (
	"context"
	"encoding/json"
	"fmt"

	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

func handleKBSearch(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ExpertID string `json:"expertId"`
		Query    string `json:"query"`
		TopK     int    `json:"topK"`
		TailNo   string `json:"tailNo"`
		AsOf     string `json:"asOf"`
		DocType  string `json:"docType"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ExpertID) ||
		len(strings.TrimSpace(p.Query)) < 1 || len(p.Query) > 2048 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "kb.search 参数无效", false)
	}
	if e.m8kb == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "知识库服务暂时不可用", true)
	}
	res, err := e.m8kb.Search(ctx, m8app.KBSearchInput{
		ExpertID: p.ExpertID, Query: p.Query, TopK: p.TopK,
		TailNo: p.TailNo, AsOf: p.AsOf, DocType: p.DocType,
	})
	if err != nil {
		return m8SliceFailure(r, err)
	}
	return r.Ok(res)
}

func handleKBCite(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p m8app.KBCitedHit
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ExpertID) ||
		!validCanonicalULID(p.DocID) || strings.TrimSpace(p.Locator) == "" ||
		strings.TrimSpace(p.Quote) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "kb.cite 参数无效", false)
	}
	if e.m8kb == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "知识库服务暂时不可用", true)
	}
	res, err := e.m8kb.Cite(ctx, p)
	if err != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "引用校验失败", false)
	}
	return r.Ok(res)
}

func handleExpertKnowledgeGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ExpertID             string `json:"expertId"`
		SourcesAfter         string `json:"sourcesAfter"`
		HistorySourceID      string `json:"historySourceId"`
		HistoryBeforeVersion int64  `json:"historyBeforeVersion"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ExpertID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "expert.knowledge.get 参数无效", false)
	}
	if e.m8kb == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "知识库服务暂时不可用", true)
	}
	res, err := e.m8kb.KnowledgeGetPage(ctx, p.ExpertID, m8app.KBSourcePage{SourcesAfter: p.SourcesAfter, HistorySourceID: p.HistorySourceID, HistoryBeforeVersion: p.HistoryBeforeVersion})
	if err != nil {
		return m8SliceFailure(r, err)
	}
	return r.Ok(res)
}

func handleExpertKnowledgeIngest(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ExpertID         string `json:"expertId"`
		Path             string `json:"path"`
		SourceLocator    string `json:"sourceLocator"`
		MediaType        string `json:"mediaType"`
		ExpectedRevision *int64 `json:"expectedRevision"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ExpertID) || strings.TrimSpace(p.Path) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "expert.knowledge.ingest 参数无效", false)
	}
	if e.m8kb == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "知识库服务暂时不可用", true)
	}
	ingest := &ExpertKBIngest{kb: e.m8kb}
	res, err := ingest.Ingest(ctx, ExpertKBIngestInput{
		ExpertID: p.ExpertID, Path: p.Path, ExpectedRevision: p.ExpectedRevision,
		SourceLocator: strings.TrimSpace(p.SourceLocator), MediaType: strings.TrimSpace(p.MediaType),
	})
	if err != nil {
		return m8SliceFailure(r, err)
	}
	docs := make([]map[string]any, 0, len(res.Documents))
	for _, d := range res.Documents {
		row := map[string]any{"documentId": d.DocumentID, "version": d.Version, "indexState": d.IndexState}
		if len(d.Preview) > 0 {
			row["preview"] = d.Preview
		}
		if strings.TrimSpace(d.FailReason) != "" {
			row["failReason"] = d.FailReason
		}
		docs = append(docs, row)
	}
	return r.Ok(map[string]any{"collectionId": res.CollectionID, "documents": docs, "source": res.Source})
}

func handleExpertGrowthGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ExpertID string `json:"expertId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ExpertID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "expert.growth.get 参数无效", false)
	}
	out := map[string]any{
		"missionSnapshot": "",
		"ladder":          []any{},
		"coverage":        map[string]any{"docTypes": []any{}, "gaps": []any{}},
		"scenarios":       []any{},
	}
	if e.m8growth != nil {
		path, ok, err := e.m8growth.Get(ctx, p.ExpertID)
		if err != nil {
			return m8SliceFailure(r, err)
		}
		if ok {
			out["missionSnapshot"] = path.MissionSnapshot
			var ladder any
			if json.Unmarshal([]byte(path.LadderJSON), &ladder) == nil {
				out["ladder"] = ladder
			}
			var coverage any
			if json.Unmarshal([]byte(path.CoverageJSON), &coverage) == nil {
				out["coverage"] = coverage
			}
		}
	}
	if e.m10scenario != nil {
		cards, err := e.m10scenario.ListScenarios(ctx, p.ExpertID, "")
		if err != nil {
			return m8SliceFailure(r, err)
		}
		scenarios := make([]map[string]string, 0, len(cards))
		for _, c := range cards {
			scenarios = append(scenarios, map[string]string{"title": c.Title, "phaseKey": c.PhaseKey})
		}
		out["scenarios"] = scenarios
	}
	return r.Ok(out)
}

// doctextProjector decodes a DOCX/PPTX/XLSX/PDF content_ref to its text layer
// then splits it into searchable chunks. A scanned or unsupported binary fails
// closed with an honest reason so the version parks at failed rather than
// indexing garbage bytes.
func doctextProjector(ctx context.Context, doc m8core.KBDocument) ([]m8core.KBChunk, error) {
	ref := strings.TrimSpace(doc.ContentRef)
	if ref == "" || !filepath.IsAbs(ref) {
		return nil, fmt.Errorf("%w: content_ref must be an absolute path", m8app.ErrKBIndexFailed)
	}
	raw, err := doctext.ReadSource(ref)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", m8app.ErrKBIndexFailed, err)
	}
	if m8app.SourceDigest(raw) != doc.SHA256 {
		return nil, fmt.Errorf("%w: source digest changed", m8app.ErrKBIndexFailed)
	}
	extracted, xerr := doctext.ExtractContext(ctx, ref, raw, doc.MediaType)
	if xerr != nil {
		return nil, fmt.Errorf("%w: %s", m8app.ErrKBIndexFailed, ingestFailReason(xerr))
	}
	parts := m8app.SplitSearchableParts(extracted.Media, strings.TrimSpace(extracted.Text))
	if len(parts) == 0 {
		return nil, fmt.Errorf("%w: no non-empty chunks", m8app.ErrKBIndexFailed)
	}
	if len(parts) > m8core.MaxKBChunksPerVersion {
		return nil, fmt.Errorf("%w: chunk count %d exceeds cap", m8app.ErrKBIndexFailed, len(parts))
	}
	return m8app.ChunksFromParts(doc, parts)
}
