package remotegateway

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// TestE2EServeForBrowser 手动端到端壳：起真实网关（真实 web/dist 静态产物
// + fake bridge handler），打印配对 URL 后常驻数分钟，供外部浏览器自动化
// （Playwright 等）访问验证。默认 skip；设 LUNITIDE_E2E=1 启用：
//
//	go test ./internal/remotegateway -run TestE2EServeForBrowser -v -timeout 8m
//
//	浏览器侧验证点：
//	1) /pair 回退到 pair.html（0.16.2 修复）
//	2) 配对页渲染、hash 解析、语言写入
//	3) POST /api/pair 真实配对、凭据落 localStorage
//	4) 成功页安装引导（Android UA → 快捷方式分支文案）
//	5) 进入主界面后 WSS 桥接（system.health 走通）
func TestE2EServeForBrowser(t *testing.T) {
	if os.Getenv("LUNITIDE_E2E") != "1" {
		t.Skip("manual e2e shell; set LUNITIDE_E2E=1")
	}
	dist := os.Getenv("LUNITIDE_E2E_DIST")
	if dist == "" {
		dist = "../../web/dist"
	}
	if _, err := os.Stat(dist + "/pair.html"); err != nil {
		t.Fatalf("dist has no pair.html: %v", err)
	}
	ctx := context.Background()
	svc, err := New(ctx, newTestRoot(t), &fakeHandler{}, "0.0.0-e2e")
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	svc.rendererDirOverride = dist
	svc.port = 0 // 本机 47651 常被正在运行的桌面实例占用；e2e 用随机端口。
	if err := svc.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	info, err := svc.IssuePairCode(ctx, "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	// IssuePairCode 的 URL 按固定 DefaultPort 拼接；随机端口下替换为实际端口。
	addr := svc.listenerAddr()
	_, port, _ := net.SplitHostPort(addr)
	url := info.URL
	if port != "" && port != fmt.Sprint(DefaultPort) {
		url = strings.Replace(url, fmt.Sprintf(":%d", DefaultPort), ":"+port, 1)
	}
	fmt.Printf("E2E_PAIR_URL=%s\n", url)
	fmt.Printf("E2E_CODE=%s\n", info.Code)
	fmt.Printf("E2E_ADDR=%s\n", addr)
	fmt.Printf("E2E_DIST=%s\n", dist)
	fmt.Println("E2E_READY=1 (holding 6 minutes for the browser run)")
	time.Sleep(6 * time.Minute)
}
