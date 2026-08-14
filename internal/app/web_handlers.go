package app

import (
	"context"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/networkpolicy"
)

// M4-G handlers: web.fetch/web.search.
// Both methods run one read-only external fetch through the SSRF-pinned
// transport, then commit evidence + run event + audit + idempotency record in
// a single transaction. Evidence carries the provenance triple (source URI,
// capture time, content digest) required by the M4 RPC/事件 contract row.

type evidenceDTO struct {
	ID            string    `json:"id"`
	RunID         string    `json:"runId"`
	Kind          string    `json:"kind"`
	SourceURI     string    `json:"sourceUri"`
	ContentDigest string    `json:"contentDigest"`
	CapturedAt    time.Time `json:"capturedAt"`
	CreatedAt     time.Time `json:"createdAt"`
}

func newEvidenceDTO(e agentrun.Evidence) evidenceDTO {
	return evidenceDTO{
		ID:            e.ID,
		RunID:         e.RunID,
		Kind:          e.Kind,
		SourceURI:     e.SourceURI,
		ContentDigest: e.ContentDigest,
		CapturedAt:    e.CapturedAt,
		CreatedAt:     e.CreatedAt,
	}
}

type webFetchResultDTO struct {
	Evidence      evidenceDTO `json:"evidence"`
	FinalURL      string      `json:"finalUrl"`
	Status        int         `json:"status"`
	ContentType   string      `json:"contentType"`
	Title         string      `json:"title"`
	Text          string      `json:"text"`
	TextTruncated bool        `json:"textTruncated"`
	FetchedBytes  int64       `json:"fetchedBytes"`
}

type webSearchResultItemDTO struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type webSearchResultDTO struct {
	Evidence evidenceDTO              `json:"evidence"`
	Query    string                   `json:"query"`
	Results  []webSearchResultItemDTO `json:"results"`
}

func webFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, agentrunapp.ErrIdempotencyKeyRequired):
		return bridge.Failure(r.ID, r.TraceID, "IDEMPOTENCY_KEY_REQUIRED", "写操作需要幂等键", false)
	case errors.Is(err, agentrunapp.ErrIdempotencyConflict):
		return bridge.Failure(r.ID, r.TraceID, "IDEMPOTENCY_CONFLICT", "幂等键已用于不同请求", false)
	case errors.Is(err, agentrun.ErrNotFound):
		return bridge.Failure(r.ID, r.TraceID, "AGENT_RUN_NOT_FOUND", "Agent 运行不存在", false)
	case errors.Is(err, agentrun.ErrInvalidTransition):
		return bridge.Failure(r.ID, r.TraceID, "AGENT_RUN_TRANSITION_INVALID", "Agent 运行状态不允许该操作", false)
	case errors.Is(err, agentrun.ErrInvalid):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "请求参数无效", false)
	case errors.Is(err, agentrunapp.ErrUnsupportedContent):
		return bridge.Failure(r.ID, r.TraceID, "WEB_CONTENT_UNSUPPORTED", "目标内容类型不支持文本提取", false)
	}
	switch networkpolicy.ErrorCode(err) {
	case networkpolicy.CodeSSRFBlocked, networkpolicy.CodeRedirectBlocked:
		return bridge.Failure(r.ID, r.TraceID, "WEB_SSRF_DENIED", "目标地址被网络策略拒绝", false)
	case networkpolicy.CodeResponseTooLarge:
		return bridge.Failure(r.ID, r.TraceID, "WEB_RESPONSE_TOO_LARGE", "目标响应超出大小上限", false)
	case networkpolicy.CodeDNSError, networkpolicy.CodeConnectionRefused, networkpolicy.CodeTLSError:
		return bridge.Failure(r.ID, r.TraceID, "WEB_FETCH_FAILED", "目标地址暂时无法连接", true)
	case networkpolicy.CodeTimeout:
		return bridge.Failure(r.ID, r.TraceID, "WEB_FETCH_FAILED", "目标地址响应超时", true)
	case networkpolicy.CodeCancelled:
		return bridge.Failure(r.ID, r.TraceID, "WEB_FETCH_FAILED", "请求已取消", true)
	}
	return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "Web 数据暂时不可用", true)
}

func handleWebFetch(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID string `json:"runId"`
		URL   string `json:"url"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RunID) || len(p.URL) < 1 || len(p.URL) > 2048 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "web.fetch 参数无效", false)
	}
	if e.agentRuns == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "Web 数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	result, err := e.agentRuns.WebFetch(ctx, r.IdempotencyKey, agentRunMutationActor, p, agentrunapp.WebFetchInput{
		RunID: p.RunID,
		URL:   p.URL,
	})
	if err != nil {
		return webFailure(r, err)
	}
	return bridge.Success(r.ID, webFetchResultDTO{
		Evidence:      newEvidenceDTO(result.Evidence),
		FinalURL:      result.FinalURL,
		Status:        result.Status,
		ContentType:   result.ContentType,
		Title:         result.Title,
		Text:          result.Text,
		TextTruncated: result.TextTruncated,
		FetchedBytes:  result.FetchedBytes,
	})
}

func handleWebSearch(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID      string `json:"runId"`
		Query      string `json:"query"`
		MaxResults int    `json:"maxResults,omitempty"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RunID) || len(p.Query) < 1 || len(p.Query) > 256 ||
		p.MaxResults < 0 || p.MaxResults > 10 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "web.search 参数无效", false)
	}
	if e.agentRuns == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "Web 数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	result, err := e.agentRuns.WebSearch(ctx, r.IdempotencyKey, agentRunMutationActor, p, agentrunapp.WebSearchInput{
		RunID:      p.RunID,
		Query:      p.Query,
		MaxResults: p.MaxResults,
	})
	if err != nil {
		return webFailure(r, err)
	}
	items := make([]webSearchResultItemDTO, 0, len(result.Results))
	for _, item := range result.Results {
		items = append(items, webSearchResultItemDTO{Title: item.Title, URL: item.URL, Snippet: item.Snippet})
	}
	return bridge.Success(r.ID, webSearchResultDTO{
		Evidence: newEvidenceDTO(result.Evidence),
		Query:    result.Query,
		Results:  items,
	})
}
