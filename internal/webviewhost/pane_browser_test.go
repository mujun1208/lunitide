package webviewhost

import (
	"strings"
	"testing"
)

func TestPaneOpensBaiduAsItsOwnDocument(t *testing.T) {
	if !PaneNavigationAllowed("https://www.baidu.com") {
		t.Fatal("baidu must open in the side browser")
	}
	if PaneNavigationAllowed("https://app.lunitide.local/index.html") || PaneNavigationAllowed("https://preview.lunitide.local/p/ticket/index.html") {
		t.Fatal("the side browser must not become the app or a preview document")
	}
	if PaneNavigationAllowed("javascript:alert(1)") || PaneNavigationAllowed("http://www.baidu.com") {
		t.Fatal("a non-https address was accepted")
	}
	cmd, ok := ParsePaneMessage(`{"source":"lunitide-pane","op":"show","url":"https://www.baidu.com/","x":12,"y":40,"width":640,"height":480,"key":1}`)
	if !ok || cmd.Op != PaneShow || !strings.HasPrefix(cmd.URL, "https://www.baidu.com") || cmd.X != 12 || cmd.Y != 40 || cmd.Width != 640 || cmd.Height != 480 || cmd.Key != 1 {
		t.Fatalf("show command = %+v ok=%v", cmd, ok)
	}
	if _, ok := ParsePaneMessage(`{"v":1,"kind":"request","method":"browser.open","payload":{}}`); ok {
		t.Fatal("bridge traffic must not be consumed as a pane command")
	}
	if cmd, ok := ParsePaneMessage(`{"source":"lunitide-pane","op":"show","url":"javascript:alert(1)","x":0,"y":0,"width":10,"height":10}`); !ok || cmd.Op != PaneNone {
		t.Fatalf("script URL was not dropped: %+v %v", cmd, ok)
	}
	script, ok := PaneClickScript(`新闻";alert(1)`)
	if !ok || strings.Contains(script, `want="新闻";alert`) || !strings.Contains(script, `新闻`) {
		t.Fatalf("click script = %s", script)
	}
	text, ok := PaneCiteText(`{"source":"lunitide-pane-page","type":"cite","text":"赢单率 75%"}`)
	if !ok || text != "赢单率 75%" {
		t.Fatalf("cite = %q %v", text, ok)
	}
	if _, ok := PaneCiteText(`{"source":"lunitide-pane","type":"cite","text":"no"}`); ok {
		t.Fatal("an app message was treated as a page selection")
	}
}

func TestPaneNavigatesWhenTheAddressChanges(t *testing.T) {
	navigate, reload := paneShouldNavigate("https://www.baidu.com/", "https://www.1905.com/", "https://www.1905.com/", 2, 1)
	if !navigate || reload {
		t.Fatalf("a new address must replace the page, navigate=%v reload=%v", navigate, reload)
	}
	navigate, reload = paneShouldNavigate("https://www.baidu.com/", "https://www.baidu.com/", "https://www.1905.com/", 3, 2)
	if !navigate || reload {
		t.Fatalf("a missed load must navigate again, navigate=%v reload=%v", navigate, reload)
	}
	navigate, reload = paneShouldNavigate("https://www.baidu.com/", "https://www.baidu.com/", "https://www.baidu.com/", 4, 3)
	if navigate || !reload {
		t.Fatalf("refresh reloads the page that is already showing, navigate=%v reload=%v", navigate, reload)
	}
}

func TestPaneOpensALocalHTMLFile(t *testing.T) {
	const want = "file:///E:/Lunitide-Project/poc/it-crm/index.html"
	if !PaneNavigationAllowed(want) {
		t.Fatal("a resolved local page must open in the side browser")
	}
	if got, ok := paneFileURL("file:///e:/Lunitide-Project/poc/it-crm/index.html"); !ok || got != want {
		t.Fatalf("canonical file URL = %q %v", got, ok)
	}
	for _, raw := range []string{
		"file:///E:/proj/../Windows/notepad.exe",
		"file:///E:/proj/index.html:payload",
		"file://server/share/index.html",
		"file:///E:/proj/%2e%2e/secret.html",
		"https://preview.lunitide.local/p/ticket/index.html",
	} {
		if PaneNavigationAllowed(raw) {
			t.Fatalf("accepted %s", raw)
		}
	}
	cmd, ok := ParsePaneMessage(`{"source":"lunitide-pane","op":"show","url":"file:///E:/Lunitide-Project/poc/it-crm/index.html","x":1,"y":2,"width":800,"height":600,"key":3}`)
	if !ok || cmd.Op != PaneShow || cmd.URL != want || cmd.Width != 800 || cmd.Height != 600 {
		t.Fatalf("show command = %+v ok=%v", cmd, ok)
	}
}
