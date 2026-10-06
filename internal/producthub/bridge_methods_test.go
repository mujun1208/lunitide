package producthub

import (
	"strings"
	"testing"
)

// TestBridgeMethodAutoCards proves the live catalog covers the whole runtime
// verb surface once the app registers its dispatch tables: every uncovered
// user-facing verb gets one template card, a verb already claimed by a
// hand-written card is not double-carded, and engine plumbing gets no card.
func TestBridgeMethodAutoCards(t *testing.T) {
	SetBridgeMethods([]string{"media.play", "mro.order.get", "fs.append", "internal.provider.resolve"})
	defer SetBridgeMethods(nil)

	var bridgeCards []string
	seen := map[string]bool{}
	var mro Candidate
	for _, c := range LiveCatalog() {
		seen[c.StableKey] = true
		if strings.Contains(c.StableKey, ".bridge.") {
			bridgeCards = append(bridgeCards, c.StableKey)
		}
		if c.StableKey == "feature.office.bridge.mro-order-get" {
			mro = c
		}
	}
	if len(bridgeCards) != 1 || bridgeCards[0] != "feature.office.bridge.mro-order-get" {
		t.Fatalf("应只自动建 1 张卡（mro.order.get），实际：%v", bridgeCards)
	}
	if mro.StableKey == "" {
		t.Fatalf("未覆盖的用户动词 mro.order.get 应得到自动卡 feature.office.bridge.mro-order-get")
	}
	if !strings.Contains(mro.Summary, "自动登记") {
		t.Fatalf("自动卡简介应写明由动词表自动登记，实际：%s", mro.Summary)
	}
	if len(mro.Scaffold.Bridge) != 1 || mro.Scaffold.Bridge[0] != "mro.order.get" {
		t.Fatalf("自动卡应声明动词 mro.order.get 作为脚手架入口，实际：%v", mro.Scaffold.Bridge)
	}
	if mro.Domain != "office" || mro.Module != "mro" {
		t.Fatalf("mro.order.get 应归 office/mro，实际：%s/%s", mro.Domain, mro.Module)
	}
	if seen["feature.office.bridge.media-play"] {
		t.Fatalf("media.play 已挂在手写媒体卡上，不应再建自动卡")
	}
	if seen["feature.foundation.bridge.fs-append"] || seen["feature.foundation.bridge.internal-provider-resolve"] {
		t.Fatalf("引擎管道动词（fs.*、internal.*）不建卡，出现：%v", seen)
	}
}

// TestBridgeMethodUserFacing pins the classification boundary: user verbs on
// one side, engine plumbing and unrecognised prefixes on the other.
func TestBridgeMethodUserFacing(t *testing.T) {
	user := []string{"mro.order.get", "media.play", "agentHub.file.open", "workflow.run"}
	for _, m := range user {
		if !BridgeMethodIsUserFacing(m) {
			t.Fatalf("%s 应判为用户功能动词", m)
		}
	}
	internal := []string{"fs.append", "internal.provider.resolve", "run.task.now", "totally.unknown.verb", "node.tick"}
	for _, m := range internal {
		if BridgeMethodIsUserFacing(m) {
			t.Fatalf("%s 应判为运行时内部（含未识别前缀的安全默认），不建卡", m)
		}
	}
}

// TestBridgeMethodCountsAndLine checks the report stats line and that an
// unregistered table keeps older reports byte-identical.
func TestBridgeMethodCountsAndLine(t *testing.T) {
	SetBridgeMethods([]string{"media.play", "mro.order.get", "fs.append", "internal.provider.resolve"})
	defer SetBridgeMethods(nil)

	c := bridgeMethodCounts()
	if c.Registered != 4 || c.UserFace != 2 || c.Carded != 1 || c.Covered != 1 || c.Internal != 2 {
		t.Fatalf("动词计数错误：注册 %+v", c)
	}
	line := BridgeMethodLine()
	for _, want := range []string{"运行时动词 4 项", "用户功能 2", "自动建卡 1", "1 项已挂在手写功能卡上", "运行时内部 2 项"} {
		if !strings.Contains(line, want) {
			t.Fatalf("统计行应含 %q，实际：%s", want, line)
		}
	}
	SetBridgeMethods(nil)
	if BridgeMethodLine() != "" {
		t.Fatalf("未注册动词表时统计行应为空，保持旧报告不变")
	}
}
