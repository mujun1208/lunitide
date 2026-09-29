package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/provider"
)

type stubProviderService struct {
	items []provider.Provider
}

func (p stubProviderService) Get(context.Context, string) (provider.Provider, error) {
	return provider.Provider{}, nil
}

func (p stubProviderService) List(context.Context, provider.Filter) ([]provider.Provider, error) {
	return p.items, nil
}

func (p stubProviderService) CreateRequest(context.Context, string, string, any, provider.Provider) (provider.Provider, error) {
	return provider.Provider{}, nil
}

func (p stubProviderService) UpdateRequest(context.Context, string, string, any, string, int64, func(provider.Provider) (provider.Provider, error)) (provider.Provider, error) {
	return provider.Provider{}, nil
}

func (p stubProviderService) DeleteRequest(context.Context, string, string, any, string, int64) (provider.Provider, error) {
	return provider.Provider{}, nil
}

func TestParseLandscapeQuotesDropsHallucinatedExcerpts(t *testing.T) {
	page := "Cursor keeps your code on your machine. Rules and MCP are public paths."
	raw := `[
		{"axis":"local","quote":"Cursor keeps your code on your machine."},
		{"axis":"local","quote":"Cursor never uploads anything, ever."},
		{"axis":"pricing","quote":"Cursor keeps your code on your machine."},
		{"axis":"hub","quote":"   "}
	]`
	got := parseLandscapeQuotes(raw, page, "Cursor", "https://cursor.com/changelog", "2026-09-29")
	if len(got) != 1 {
		t.Fatalf("only the verbatim survivor may pass: %#v", got)
	}
	q := got[0]
	if q.Name != "Cursor" || q.Axis != "local" || q.Quote != "Cursor keeps your code on your machine." {
		t.Fatalf("quote %#v", q)
	}
	if q.URL != "https://cursor.com/changelog" || q.Date != "2026-09-29" {
		t.Fatalf("url and date must be stamped by the program, not the model: %#v", q)
	}
}

func TestParseLandscapeQuotesToleratesFencedOutput(t *testing.T) {
	page := "Local-first by default."
	raw := "Sure, here is the JSON:\n```json\n[{\"axis\":\"local\",\"quote\":\"Local-first by default.\"}]\n```\nDone."
	got := parseLandscapeQuotes(raw, page, "Cursor", "https://cursor.com/changelog", "2026-09-29")
	if len(got) != 1 || got[0].Quote != "Local-first by default." {
		t.Fatalf("got %#v", got)
	}
}

func TestParseLandscapeQuotesRejectsBrokenJSON(t *testing.T) {
	if got := parseLandscapeQuotes("not json at all", "page", "Cursor", "https://cursor.com/changelog", "2026-09-29"); got != nil {
		t.Fatalf("broken JSON must yield nothing: %#v", got)
	}
}

func TestResearchLandscapeExplainsMissingChannels(t *testing.T) {
	var nilEngine *Engine
	if _, skips := nilEngine.ResearchLandscape(context.Background(), []string{"Cursor"}); len(skips) != 1 || !strings.Contains(skips[0], "采集通道未装配") {
		t.Fatalf("nil engine skips %#v", skips)
	}
	bare := &Engine{}
	if _, skips := bare.ResearchLandscape(context.Background(), []string{"Cursor"}); len(skips) != 1 || !strings.Contains(skips[0], "采集通道未装配") {
		t.Fatalf("no provider service skips %#v", skips)
	}
	noLLM := &Engine{providers: stubProviderService{}}
	if _, skips := noLLM.ResearchLandscape(context.Background(), []string{"Cursor"}); len(skips) != 1 || !strings.Contains(skips[0], "没有启用中的文本模型") {
		t.Fatalf("no enabled LLM skips %#v", skips)
	}
}

func TestResearchLandscapeNeverGuessesUnregisteredAddresses(t *testing.T) {
	e := &Engine{providers: stubProviderService{items: []provider.Provider{{
		ID: "p1", Status: provider.StatusEnabled, CredentialState: provider.CredentialConfigured, CredentialRef: "ref",
		Models: []provider.Model{{ModelID: "m1"}},
	}}}}
	quotes, skips := e.ResearchLandscape(context.Background(), []string{"Totally Unknown"})
	if len(quotes) != 0 || len(skips) != 1 {
		t.Fatalf("quotes %#v skips %#v", quotes, skips)
	}
	if !strings.Contains(skips[0], "没有登记官方公开页") || !strings.Contains(skips[0], "不猜地址") {
		t.Fatalf("skip %q", skips[0])
	}
}
