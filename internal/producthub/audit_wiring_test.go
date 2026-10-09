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

// TestCapabilityVerbsAreWiredAndTraced pins the fix behind the three
// "步骤未按真实调用写清" cards in the 2026-10-09 report: capability.list,
// capability.roles.get and capability.roles.set are real registered bridge
// verbs with static handlers. Once the runtime verb table is registered (as
// wire.go does at app start) the auto cards must trace to their handlers, and
// the wiring audit must not flag them — a capability.* prefix is no reason to
// skip a claimed entry.
func TestCapabilityVerbsAreWiredAndTraced(t *testing.T) {
	SetBridgeMethods([]string{"capability.list", "capability.roles.get", "capability.roles.set"})
	defer SetBridgeMethods(nil)

	cards := clarifyTemplateChains(Merge(Seed(), LiveCatalog(), nil))
	handlers := map[string]string{
		"feature.foundation.bridge.capability-list":      "handleCapabilityList",
		"feature.foundation.bridge.capability-roles-get": "handleCapabilityRolesGet",
		"feature.foundation.bridge.capability-roles-set": "handleCapabilityRolesSet",
	}
	for key, handler := range handlers {
		var card Card
		found := false
		for _, c := range cards {
			if c.StableKey == key {
				card, found = c, true
				break
			}
		}
		if !found {
			t.Fatalf("动词表已注册，自动卡缺失：%s", key)
		}
		if len(card.Chain.Steps) == 0 || card.Chain.Steps[0].Name != handler {
			t.Fatalf("%s 链路应从处理函数 %s 写出，实际步骤：%+v", key, handler, card.Chain.Steps)
		}
		if !strings.Contains(card.Chain.Steps[0].Description, "这次从源码读到的处理函数") {
			t.Fatalf("%s 第一步应写明来自源码，实际：%s", key, card.Chain.Steps[0].Description)
		}
	}
	for _, f := range auditWiring(cards) {
		if _, claimed := handlers[f.StableKey]; claimed {
			t.Fatalf("真实桥方法不应报问题条目：%+v", f)
		}
	}
}
