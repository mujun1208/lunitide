package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestDesktopQuitRequiresExactNameAndApproval(t *testing.T) {
	old := quitDesktopProcesses
	calls := 0
	quitDesktopProcesses = func(context.Context, []string) (int, error) { calls++; return 3, nil }
	t.Cleanup(func() { quitDesktopProcesses = old })
	for _, raw := range []string{`{"name":"微信助手","force":true}`, `{"name":"微","force":true}`, `{"name":"微信","force":false}`} {
		if _, err := executeDesktopQuit(context.Background(), json.RawMessage(raw), true); err == nil {
			t.Fatalf("allowed %s", raw)
		}
	}
	if _, err := executeDesktopQuit(context.Background(), json.RawMessage(`{"name":"微信","force":true}`), false); err == nil {
		t.Fatal("missing approval")
	}
	if calls != 0 {
		t.Fatal("quit without exact approved request")
	}
	out, err := executeDesktopQuit(context.Background(), json.RawMessage(`{"name":"微信","force":true}`), true)
	if err != nil || calls != 1 || !strings.Contains(out.Output, "已彻底退出微信") {
		t.Fatalf("%+v %v", out, err)
	}
	quitDesktopProcesses = func(context.Context, []string) (int, error) { return 1, errors.New("still running") }
	if _, err := executeDesktopQuit(context.Background(), json.RawMessage(`{"name":"微信","force":true}`), true); err == nil {
		t.Fatal("must not report successful exit")
	}
}

func TestDesktopBrowseUsesRealBrowserAndEscapesQuery(t *testing.T) {
	old := openDesktopURL
	var opened string
	openDesktopURL = func(address string) error { opened = address; return nil }
	t.Cleanup(func() { openDesktopURL = old })
	for _, raw := range []string{`{"url":"file:///C:/secret.txt"}`, `{"url":"javascript:alert(1)"}`, `{"url":"https://user:pass@example.com"}`, `{"url":"https://example.com","query":"x"}`} {
		if _, err := executeDesktopBrowse(json.RawMessage(raw), true); err == nil {
			t.Fatalf("allowed %s", raw)
		}
	}
	if opened != "" {
		t.Fatal(opened)
	}
	_, err := executeDesktopBrowse(json.RawMessage(`{"query":"汽水 & 音乐 #中文"}`), true)
	u, _ := url.Parse(opened)
	if err != nil || u.Host != "www.bing.com" || u.Query().Get("q") != "汽水 & 音乐 #中文" {
		t.Fatalf("%s %v", opened, err)
	}
}
