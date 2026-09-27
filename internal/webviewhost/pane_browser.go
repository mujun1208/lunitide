package webviewhost

import (
	"encoding/json"
	"math"
	"net/url"
	"strings"
)

// The side browser is its own document, not an iframe inside the app.
// Sites such as Baidu send frame-ancestors that name only themselves, and the
// app document's frame-src is just as narrow, so an iframe of a normal site
// paints the blocked-page icon. A top-level WebView is the document a browser
// would open. HTTPS never goes to this app, the preview origin, or the media
// origin. A local HTML file is the other address it accepts: file:///X:/...
// with no traversal, so a page that already has a resolved path can run.

type PaneOp int

const (
	PaneNone PaneOp = iota
	PaneShow
	PaneHide
	PaneRead
	PaneClick
)

type PaneCommand struct {
	Op            PaneOp
	URL           string
	Text          string
	Key           int
	X, Y          int32
	Width, Height int32
}

// PaneNavigationAllowed is the address bar of the side browser: ordinary
// HTTPS, and a local HTML file the app already resolved. It never becomes
// this app, the preview origin, or the media origin.
func PaneNavigationAllowed(raw string) bool {
	if _, ok := paneFileURL(raw); ok {
		return true
	}
	canonical, err := NormalizeBrowserURL(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	u, err := url.Parse(canonical)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case TrustedVirtualHost, PreviewVirtualHost, "media.lunitide.local":
		return false
	default:
		return u.Hostname() != ""
	}
}

// canonicalPaneURL is the address the side browser will navigate to.
func canonicalPaneURL(raw string) (string, bool) {
	if file, ok := paneFileURL(raw); ok {
		return file, true
	}
	if !PaneNavigationAllowed(raw) {
		return "", false
	}
	canonical, err := NormalizeBrowserURL(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	return canonical, true
}

// paneFileURL accepts file:///X:/path/file with no traversal and no extra colon.
// The returned URL is the form Navigate and later comparisons both use.
func paneFileURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 12 || !strings.EqualFold(raw[:8], "file:///") {
		return "", false
	}
	rest := raw[8:]
	if strings.ContainsAny(rest, "?#\x00") {
		return "", false
	}
	decoded, err := url.PathUnescape(rest)
	if err != nil || decoded == "" {
		return "", false
	}
	decoded = strings.ReplaceAll(decoded, `\`, "/")
	if strings.Contains(decoded, "\x00") || strings.Count(decoded, ":") != 1 {
		return "", false
	}
	if len(decoded) < 4 || decoded[1] != ':' || decoded[2] != '/' {
		return "", false
	}
	letter := decoded[0]
	if letter >= 'a' && letter <= 'z' {
		letter -= 'a' - 'A'
	}
	if letter < 'A' || letter > 'Z' {
		return "", false
	}
	parts := strings.Split(decoded[3:], "/")
	encoded := make([]string, 0, len(parts))
	for _, segment := range parts {
		if segment == "" || segment == "." || segment == ".." {
			return "", false
		}
		encoded = append(encoded, url.PathEscape(segment))
	}
	return "file:///" + string(letter) + ":/" + strings.Join(encoded, "/"), true
}

// ParsePaneMessage reads a message from the application document. A bridge
// request does not match and is left for the gateway. A pane message is
// consumed even when its URL or bounds are unusable, so it cannot fall
// through into a bridge call.
func ParsePaneMessage(raw string) (PaneCommand, bool) {
	var msg struct {
		Source string  `json:"source"`
		Op     string  `json:"op"`
		URL    string  `json:"url"`
		Text   string  `json:"text"`
		Key    int     `json:"key"`
		X      float64 `json:"x"`
		Y      float64 `json:"y"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	}
	if err := json.Unmarshal([]byte(raw), &msg); err != nil || msg.Source != "lunitide-pane" {
		return PaneCommand{}, false
	}
	switch msg.Op {
	case "hide":
		return PaneCommand{Op: PaneHide}, true
	case "read":
		return PaneCommand{Op: PaneRead}, true
	case "click":
		text := strings.TrimSpace(msg.Text)
		if text == "" || len([]rune(text)) > 80 || strings.Contains(text, "\x00") {
			return PaneCommand{}, true
		}
		return PaneCommand{Op: PaneClick, Text: text}, true
	case "show":
		canonical, allowed := canonicalPaneURL(msg.URL)
		if !allowed {
			return PaneCommand{}, true
		}
		x, xOK := panePx(msg.X)
		y, yOK := panePx(msg.Y)
		w, wOK := panePx(msg.Width)
		h, hOK := panePx(msg.Height)
		if !xOK || !yOK || !wOK || !hOK || w < 1 || h < 1 {
			return PaneCommand{}, true
		}
		return PaneCommand{Op: PaneShow, URL: canonical, Key: msg.Key, X: x, Y: y, Width: w, Height: h}, true
	default:
		return PaneCommand{}, true
	}
}

// paneShouldNavigate reports whether the side browser must load a new
// address or refresh the one already on screen. A refresh must not reload
// a page the address bar has already left.
func paneShouldNavigate(next, shown, document string, key, shownKey int) (navigate bool, reload bool) {
	if next == "" {
		return false, false
	}
	if next != document && (next != shown || key != shownKey) {
		return true, false
	}
	if next == document && key != shownKey {
		return false, true
	}
	return false, false
}

func panePx(v float64) (int32, bool) {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 10000 {
		return 0, false
	}
	return int32(math.Round(v)), true
}

// PaneCiteText is the only thing a page inside the side browser may send
// back: the words the user selected.
func PaneCiteText(raw string) (string, bool) {
	var msg struct {
		Source string `json:"source"`
		Type   string `json:"type"`
		Text   string `json:"text"`
	}
	if json.Unmarshal([]byte(raw), &msg) != nil || msg.Source != "lunitide-pane-page" || msg.Type != "cite" {
		return "", false
	}
	text := strings.Join(strings.Fields(msg.Text), " ")
	if text == "" {
		return "", false
	}
	runes := []rune(text)
	if len(runes) > 800 {
		text = string(runes[:800])
	}
	return text, true
}

const paneReadScript = `(function(){var t=(document.body&&document.body.innerText)||'';return t.replace(/\s+/g,' ').trim().slice(0,1500)})()`

const paneTitleScript = `(function(){return (document.title||'').replace(/\s+/g,' ').trim().slice(0,80)})()`

const paneCiteScript = `(function(){if(window.__lunitidePaneCite)return;window.__lunitidePaneCite=true;document.addEventListener('mouseup',function(){var s=window.getSelection&&window.getSelection();var t=s?String(s).replace(/\s+/g,' ').trim():'';if(!t||!window.chrome||!window.chrome.webview)return;try{window.chrome.webview.postMessage({source:'lunitide-pane-page',type:'cite',text:t.slice(0,800)})}catch(e){}})})()`

// paneDocumentScript runs before a local file's own scripts. If disk storage
// throws, setItem is remembered in memory so a page that saves and then draws
// still draws. HTTPS pages leave their storage alone.
const paneDocumentScript = `(function(){if(location.protocol!=='file:')return;var mem={};function remember(k,v){mem[String(k)]=String(v)}function recall(k){return Object.prototype.hasOwnProperty.call(mem,k)?mem[k]:null}function forget(k){delete mem[k]}function wipe(){mem={}}try{var proto=window.Storage&&Storage.prototype;if(proto&&!proto.__lunitidePatched){var set=proto.setItem,get=proto.getItem,rem=proto.removeItem,clr=proto.clear;proto.setItem=function(k,v){remember(k,v);try{return set.call(this,k,String(v))}catch(e){}};proto.getItem=function(k){try{var v=get.call(this,k);if(v!=null)return v}catch(e){}return recall(k)};proto.removeItem=function(k){forget(k);try{return rem.call(this,k)}catch(e){}};proto.clear=function(){wipe();try{return clr.call(this)}catch(e){}};proto.__lunitidePatched=1}}catch(e){}var api={getItem:function(k){return recall(k)},setItem:function(k,v){remember(k,v)},removeItem:forget,clear:wipe,key:function(i){return Object.keys(mem)[i]||null},get length(){return Object.keys(mem).length}};function broken(){try{var s=window.localStorage;var k='__lunitide_probe__';s.setItem(k,'1');s.removeItem(k);return false}catch(e){return true}}if(!broken())return;try{Object.defineProperty(window,'localStorage',{configurable:true,get:function(){return api}})}catch(e){}try{Object.defineProperty(window,'sessionStorage',{configurable:true,get:function(){return api}})}catch(e){}})()`

// PaneClickScript clicks the first link or button whose text contains the
// label. The label is a JSON string, so a quote in it cannot close the script.
func PaneClickScript(label string) (string, bool) {
	label = strings.TrimSpace(label)
	if label == "" || len([]rune(label)) > 80 || strings.Contains(label, "\x00") {
		return "", false
	}
	encoded, err := json.Marshal(label)
	if err != nil {
		return "", false
	}
	return `(function(){var want=` + string(encoded) + `;var nodes=document.querySelectorAll('a,button,[data-action],[role=button],.nav-item');for(var i=0;i<nodes.length;i++){var text=(nodes[i].textContent||'').replace(/\s+/g,' ').trim();if(text&&text.indexOf(want)>=0){nodes[i].click();return;}}})()`, true
}
