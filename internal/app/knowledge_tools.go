package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func knowledgeToolDefinitions() []llmadapter.ToolDefinition {
	return []llmadapter.ToolDefinition{
		{Name: "kb.search", Description: "Search one expert's controlled knowledge base. expertId is that expert's ULID; omit it to use the expert mounted on this chat. An empty library returns explanation.missing=true and 未找到受控依据. Do not invent chapter numbers or figures when missing is true.", Schema: []byte(`{"type":"object","properties":{"expertId":{"type":"string","maxLength":32},"query":{"type":"string","minLength":1,"maxLength":2048},"topK":{"type":"integer","minimum":1,"maximum":12},"tailNo":{"type":"string","maxLength":64},"asOf":{"type":"string","maxLength":32},"docType":{"type":"string","maxLength":64}},"required":["query"],"additionalProperties":false}`)},
		{Name: "kb.cite", Description: "Check one quote against a kb.search hit. locator is the hit's locator JSON and must include chunkId. A failed check returns explanation.missing=true; do not keep the quote.", Schema: []byte(`{"type":"object","properties":{"expertId":{"type":"string","maxLength":32},"docId":{"type":"string","maxLength":32},"revision":{"type":"string","maxLength":64},"locator":{"type":"string","maxLength":4000},"quote":{"type":"string","minLength":1,"maxLength":2000},"score":{"type":"number"}},"required":["docId","locator","quote"],"additionalProperties":false}`)},
		{Name: "graph.expand", Description: "Expand one knowledge chunk by one graph hop. The product graph is not loaded yet: the result is explanation.missing=true with reason 未使用图谱. Do not invent neighboring nodes.", Schema: []byte(`{"type":"object","properties":{"expertId":{"type":"string","maxLength":32},"chunkId":{"type":"string","maxLength":32},"nodeId":{"type":"string","maxLength":128}},"additionalProperties":false}`)},
	}
}

func knowledgeMissing(reason string) toolruntime.Result {
	raw, _ := json.Marshal(m8app.KBSearchResult{Explanation: m8app.KBExplanation{Missing: true, Reasons: []string{reason}}})
	return toolruntime.Result{Output: string(raw)}
}

func (e *Engine) executeKnowledgeTool(ctx context.Context, session, name string, args json.RawMessage) (toolruntime.Result, error) {
	switch name {
	case "graph.expand":
		return knowledgeMissing("未使用图谱"), nil
	case "kb.search":
		return e.executeKBSearch(ctx, session, args)
	case "kb.cite":
		return e.executeKBCite(ctx, session, args)
	default:
		return knowledgeMissing("未知知识库工具"), nil
	}
}

func (e *Engine) executeKBSearch(ctx context.Context, session string, args json.RawMessage) (toolruntime.Result, error) {
	var p struct {
		ExpertID string `json:"expertId"`
		Query    string `json:"query"`
		TopK     int    `json:"topK"`
		TailNo   string `json:"tailNo"`
		AsOf     string `json:"asOf"`
		DocType  string `json:"docType"`
	}
	if json.Unmarshal(args, &p) != nil || strings.TrimSpace(p.Query) == "" {
		return toolruntime.Result{}, errString("kb.search 需要 query")
	}
	expertID := strings.TrimSpace(p.ExpertID)
	if !validCanonicalULID(expertID) {
		expertID = firstMountedExpert(e, ctx, session)
	}
	if e == nil || e.m8kb == nil {
		return knowledgeMissing("知识库未启用，未找到受控依据"), nil
	}
	if !validCanonicalULID(expertID) {
		return knowledgeMissing("未挂载专家，未找到受控依据"), nil
	}
	res, err := e.m8kb.Search(ctx, m8app.KBSearchInput{
		ExpertID: expertID, Query: strings.TrimSpace(p.Query), TopK: p.TopK,
		TailNo: p.TailNo, AsOf: p.AsOf, DocType: p.DocType,
	})
	if err != nil {
		return knowledgeMissing("未找到受控依据"), nil
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return knowledgeMissing("未找到受控依据"), nil
	}
	return toolruntime.Result{Output: string(raw)}, nil
}

func (e *Engine) executeKBCite(ctx context.Context, session string, args json.RawMessage) (toolruntime.Result, error) {
	var hit m8app.KBCitedHit
	if json.Unmarshal(args, &hit) != nil || strings.TrimSpace(hit.Quote) == "" || strings.TrimSpace(hit.Locator) == "" {
		return toolruntime.Result{}, errString("kb.cite 需要 docId、locator 和 quote")
	}
	if !validCanonicalULID(hit.ExpertID) {
		hit.ExpertID = firstMountedExpert(e, ctx, session)
	}
	if e == nil || e.m8kb == nil || !validCanonicalULID(hit.ExpertID) || !validCanonicalULID(hit.DocID) {
		return knowledgeMissing("引用校验失败，未找到受控依据"), nil
	}
	res, err := e.m8kb.Cite(ctx, hit)
	if err != nil {
		return knowledgeMissing("引用校验失败，未找到受控依据"), nil
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return knowledgeMissing("引用校验失败，未找到受控依据"), nil
	}
	return toolruntime.Result{Output: string(raw)}, nil
}

func firstMountedExpert(e *Engine, ctx context.Context, session string) string {
	if e == nil {
		return ""
	}
	for _, id := range e.sessionMountedExpertIDs(ctx, session) {
		if validCanonicalULID(id) {
			return id
		}
	}
	return ""
}

type errString string

func (e errString) Error() string { return string(e) }
