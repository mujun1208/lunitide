package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/mcp6"
)

func TestMcpSearchUnderstandsCommonChineseQueries(t *testing.T) {
	for query, description := range map[string]string{"查询合肥天气": "get_weather Weather forecast for a city", "查明天火车票": "train availability", "查询航班价格": "flight offers", "股票行情": "stock quote"} {
		if mcpSearchScore(query, description) == 0 {
			t.Fatalf("Chinese query missed English service: %s", query)
		}
		if mcpSearchScore(query, "filesystem read_file") == 0 {
			continue
		}
		t.Fatalf("unrelated service matched: %s", query)
	}
	if mcpSearchScore("天气", "天气") <= mcpSearchScore("天气", "weather") {
		t.Fatal("exact match should rank first")
	}
	if string(mcpInputSchema(nil)) == "null" || !json.Valid(mcpInputSchema(nil)) {
		t.Fatal("fallback schema invalid")
	}
	if _, _, ok := parseMcpToolName("01ARZ3NDEKTSV4RRFFQ69G5FAV_weather"); ok {
		t.Fatal("missing MCP namespace accepted")
	}
}

func TestMcpSearchFiltersScopeBeforeLimit(t *testing.T) {
	ctx := context.Background()
	registry := mcp6.NewRegistry(func(context.Context, *mcp6.Endpoint) error { return nil }, nil, fakeMcpLease{})
	registry.SetDescribeFunc(func(_ context.Context, ep *mcp6.Endpoint) (map[string]mcp6.ToolSchema, error) {
		if strings.Contains(ep.URL, "allowed") {
			return map[string]mcp6.ToolSchema{"weather": {Description: "Weather forecast", InputSchema: json.RawMessage(`{"type":"object","required":["city"]}`)}}, nil
		}
		catalog := map[string]mcp6.ToolSchema{}
		for i := 0; i < 20; i++ {
			catalog[fmt.Sprintf("weather_%02d", i)] = mcp6.ToolSchema{Description: "weather forecast"}
		}
		return catalog, nil
	})
	for _, url := range []string{"https://outside.invalid/mcp", "https://allowed.invalid/mcp"} {
		ep, err := registry.Register(ctx, mcp6.EndpointInput{Transport: "https", URL: url, Pin: mcp6.BootstrapPin(url)})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(url, "allowed") {
			e := NewEngine(nil, "test")
			e.SetM6Services(nil, registry, nil)
			e.rememberMcpPreset(ep.ID, "weather-service")
			out, err := e.searchMcpToolsScoped(json.RawMessage(`{"query":"查询合肥天气"}`), []string{"weather-service"}, true)
			if err != nil || !strings.Contains(out, ep.ID) || strings.Contains(out, "weather_00") {
				t.Fatalf("scoped result=%s err=%v", out, err)
			}
			if !strings.Contains(out, `"required":["city"]`) {
				t.Fatalf("missing real schema: %s", out)
			}
		}
	}
}
