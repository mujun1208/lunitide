package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/egressproxy"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/producthub"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/webfetch"
)

// Stage-2 landscape collection. The channel is deliberately narrow:
//   - only names that already sit in the saved landscape list are collected;
//   - only pages registered in landscapeSources are fetched (read-only GET,
//     SSRF-pinned transport), so the scope never spreads on its own;
//   - the LLM may only answer axis + verbatim quote. The URL and the date
//     are stamped by the program, never by the model;
//   - a quote that is not a character-for-character substring of the fetched
//     page text is dropped, not repaired.
var landscapeSources = map[string]string{
	"cursor":          "https://cursor.com/changelog",
	"copilot":         "https://github.blog/changelog/label/copilot/",
	"chatgpt desktop": "https://openai.com/chatgpt/download/",
	"claude desktop":  "https://claude.com/download",
	"windsurf":        "https://windsurf.com/changelog",
	"trae":            "https://www.trae.ai/blog",
}

const landscapePageBudget = 24 << 10

// ResearchLandscape implements producthub.LandscapeResearcher.
func (e *Engine) ResearchLandscape(ctx context.Context, names []string) ([]producthub.LandscapeQuote, []string) {
	if e == nil || e.providers == nil {
		return nil, []string{"采集通道未装配：模型供应商服务不可用，不能编造摘录。"}
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return nil, []string{"供应商目录暂时不可用，这一轮没有采集。"}
	}
	catalog := provider.CatalogForKind(items, provider.KindLLM)
	if len(catalog) == 0 {
		return nil, []string{"没有启用中的文本模型。先在设置里配置供应商、凭据和模型，再点采集。"}
	}
	entry := catalog[0]
	var quotes []producthub.LandscapeQuote
	var skips []string
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		pageURL, ok := landscapeSources[strings.ToLower(name)]
		if !ok {
			skips = append(skips, "「"+name+"」没有登记官方公开页。采集只用登记过公开源的产品，不猜地址。")
			continue
		}
		page, fetchErr := networkpolicy.Fetch(ctx, pageURL, networkpolicy.FetchOptions{
			Policy: networkpolicy.Policy{AllowHTTP: true},
			Proxy:  egressproxy.Resolver(),
		})
		if fetchErr != nil || page.Status != 200 {
			skips = append(skips, "「"+name+"」的公开页这一轮没有取到，跳过；不编造摘录。")
			continue
		}
		extracted, ok := webfetch.ExtractText(page.ContentType, page.Body, webfetch.MaxTextBytes)
		if !ok || strings.TrimSpace(extracted.Text) == "" {
			skips = append(skips, "「"+name+"」的公开页没有可提取的正文，跳过。")
			continue
		}
		got, llmErr := e.landscapeQuotesFromPage(ctx, entry, name, pageURL, extracted.Text)
		if llmErr != nil {
			skips = append(skips, "「"+name+"」这一轮摘录失败（模型没有返回可解析的结果），跳过。")
			continue
		}
		if len(got) == 0 {
			skips = append(skips, "「"+name+"」没有一条摘录通过原文一致性校验，全部丢弃。")
			continue
		}
		quotes = append(quotes, got...)
	}
	return quotes, skips
}

// landscapeQuotesFromPage runs one LLM extraction over one fetched page and
// applies the verbatim gate against the page text the model actually saw.
func (e *Engine) landscapeQuotesFromPage(ctx context.Context, entry provider.CatalogEntry, name, pageURL, pageText string) ([]producthub.LandscapeQuote, error) {
	if len(pageText) > landscapePageBudget {
		pageText = pageText[:landscapePageBudget]
	}
	var raw string
	leaseErr := e.withProviderLease(ctx, entry.Provider, secretlease.OperationChat, func(op context.Context, secret []byte) error {
		adapter, adapterErr := e.adapter(op, entry.Provider)
		if adapterErr != nil {
			return adapterErr
		}
		resp, completeErr := adapter.Complete(op, secret, llmadapter.Request{
			Model: entry.Model.ModelID, MaxTokens: 900, MaxAttempts: 1, DisableReasoning: true,
			Messages: []llmadapter.Message{
				{Role: llmadapter.RoleSystem, Content: landscapeExtractPrompt()},
				{Role: llmadapter.RoleUser, Content: pageText},
			},
		})
		if completeErr != nil {
			return completeErr
		}
		raw = resp.Message.Content
		return nil
	})
	if leaseErr != nil {
		return nil, leaseErr
	}
	return parseLandscapeQuotes(raw, pageText, name, pageURL, time.Now().Format("2006-01-02")), nil
}

func landscapeExtractPrompt() string {
	return strings.Join([]string{
		"You extract verbatim evidence from one public web page for a product comparison.",
		"Reply with exactly one JSON array and nothing else:",
		`[{"axis":"local|media|assets|hub","quote":"..."}]`,
		"Axis meanings:",
		"local = the engine and data run primarily on the user's machine;",
		"media = playback success is verified against real player state;",
		"assets = skills, MCP and plugins are managed as one asset domain;",
		"hub = the product describes itself with a structured knowledge hub or diagnostics.",
		"Hard rules:",
		"- Every quote MUST be copied character-for-character from the page text. Never translate, paraphrase, shorten with ellipses, or fix typos.",
		"- Return at most 3 quotes, only ones that clearly support one of the four axes.",
		"- If nothing on the page clearly supports any axis, return [].",
	}, "\n")
}

// parseLandscapeQuotes is the verbatim gate: a quote survives only when it is
// a substring of the page text the model was shown. Everything else is
// dropped silently — hallucinated evidence must never reach a draft.
func parseLandscapeQuotes(llmOutput, pageText, name, pageURL, date string) []producthub.LandscapeQuote {
	trimmed := strings.TrimSpace(llmOutput)
	if start := strings.Index(trimmed, "["); start >= 0 {
		if end := strings.LastIndex(trimmed, "]"); end > start {
			trimmed = trimmed[start : end+1]
		}
	}
	var parsed []struct {
		Axis  string `json:"axis"`
		Quote string `json:"quote"`
	}
	if json.Unmarshal([]byte(trimmed), &parsed) != nil {
		return nil
	}
	valid := map[string]bool{"local": true, "media": true, "assets": true, "hub": true}
	out := make([]producthub.LandscapeQuote, 0, len(parsed))
	for _, item := range parsed {
		quote := strings.TrimSpace(item.Quote)
		if !valid[item.Axis] || len(quote) == 0 || len(quote) > 600 {
			continue
		}
		if !strings.Contains(pageText, quote) {
			continue
		}
		out = append(out, producthub.LandscapeQuote{
			Name: name, Axis: item.Axis, Quote: quote, URL: pageURL, Date: date,
		})
	}
	return out
}
