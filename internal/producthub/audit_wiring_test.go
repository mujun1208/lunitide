package producthub

import (
	"strings"
	"testing"
)

func TestAuditNamesAnUnwiredBridgeAndDropsItWhenTheMethodExists(t *testing.T) {
	bad := auditWiring([]Card{{
		StableKey: "feature.bad", Name: "不存在的入口",
		Scaffold: Scaffold{Bridge: []string{"missing.method", "message.append"}},
	}})
	if len(bad) != 1 || bad[0].ErrorCode != "PH_021" || !strings.Contains(bad[0].Evidence, "missing.method") || strings.Contains(bad[0].Evidence, "message.append") {
		t.Fatalf("%#v", bad)
	}
	if strings.Contains(bad[0].Fix, "FunASR") || strings.Contains(bad[0].Fix, "SenseVoice") {
		t.Fatal(bad[0].Fix)
	}
	good := auditWiring([]Card{{
		StableKey: "feature.bad", Name: "不存在的入口",
		Scaffold: Scaffold{Bridge: []string{"message.append"}},
	}})
	if len(good) != 0 {
		t.Fatalf("fixed bridge still flagged: %#v", good)
	}
	ed := Edition{Features: []Card{{
		StableKey: "feature.bad", Name: "不存在的入口",
		Methods:  []Method{{Type: "menu", Entry: "settings"}},
		Chain:    Chain{Steps: []Step{{Index: 1, Name: "走", Detail: "一步", Description: "往下"}}},
		Scaffold: Scaffold{Bridge: []string{"missing.method"}},
	}}, Findings: bad}
	md, _ := RenderReport(ed)
	if !strings.Contains(md, "### 逐条核对") || !strings.Contains(md, "missing.method") || strings.Contains(strings.Split(md, "### 逐条核对")[1], "message.append\n") {
		t.Fatalf("report did not list the unwired entry: %s", md[strings.Index(md, "## 5."):])
	}
}

func TestAuditNamesAPluginMissingFromTheHarness(t *testing.T) {
	got := auditWiring([]Card{
		{StableKey: "feature.assets.plugin.llm", Name: "插件：LLM", Attributes: Attributes{Tools: []string{"llm"}}},
		{StableKey: "feature.assets.plugin.nope", Name: "插件：没有", Attributes: Attributes{Tools: []string{"not-a-plugin"}}},
	})
	if len(got) != 1 || got[0].ErrorCode != "PH_022" || !strings.Contains(got[0].Evidence, "nope") {
		t.Fatalf("%#v", got)
	}
}
