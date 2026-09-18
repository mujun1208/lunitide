package mcp6

import "testing"

func TestPaidCredentialPresetsStayOffTheMarket(t *testing.T) {
	for _, id := range []string{
		"tushare", "juhe-query", "gdrive", "postgres", "redis", "google-maps",
		"brave-search", "gitlab", "sentry", "everart", "aws-kb", "amap", "tavily",
		"firecrawl", "notion", "lark", "huggingface", "neon", "supabase", "qdrant",
		"elasticsearch", "linear", "mongodb", "browsermcp", "markdownify",
	} {
		if _, ok := PresetByID(id); ok {
			t.Fatalf("credential or fill-in preset %s must not ship in the one-click catalog", id)
		}
	}
}
