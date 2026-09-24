package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/webviewhost"
)

// The reason any of this exists: a user opening a generated page in the product
// should get the page, working. That means the preview answer has to carry a URL
// the host will serve, and it has to address the document the user opened — not a
// neighbouring file, and not nothing at all.
func TestHTMLPreviewHandsTheRendererAWorkingPage(t *testing.T) {
	e := newArtifactEngine(t)
	ctx := context.Background()
	dir, err := e.tools.SessionFolder(artifactSession)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "crm"), 0o755); err != nil {
		t.Fatal(err)
	}
	page := "<html><body><nav onclick=\"switchView()\">菜单</nav><script src=\"app.js\"></script></body></html>"
	if err := os.WriteFile(filepath.Join(dir, "crm", "index.html"), []byte(page), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "crm", "app.js"), []byte("function switchView(){}"), 0o600); err != nil {
		t.Fatal(err)
	}
	resp := handleWorkspaceArtifactPreview(e, ctx, artifactRequest(`{"sessionId":"`+artifactSession+`","path":"crm/index.html"}`))
	if !resp.OK {
		t.Fatalf("html preview failed: %+v", resp)
	}
	raw, _ := json.Marshal(resp.Payload)
	var out struct {
		InteractiveURL string `json:"interactiveUrl"`
	}
	if json.Unmarshal(raw, &out) != nil || out.InteractiveURL == "" {
		t.Fatalf("html preview carries no interactive URL: %s", raw)
	}
	token, rel, ok := webviewhost.ParsePreviewRequest(out.InteractiveURL)
	if !ok {
		t.Fatalf("the host would refuse %q", out.InteractiveURL)
	}
	// The document itself, and the script it loads relative to itself, both have to
	// resolve to the files on disk — a page whose script 404s is not working.
	docPath, _, err := e.ResolvePreviewTicket(ctx, token, rel)
	if err != nil || filepath.Base(docPath) != "index.html" {
		t.Fatalf("document resolved to %q (%v)", docPath, err)
	}
	scriptPath, _, err := e.ResolvePreviewTicket(ctx, token, "app.js")
	if err != nil || filepath.Base(scriptPath) != "app.js" {
		t.Fatalf("sibling script resolved to %q (%v)", scriptPath, err)
	}
	// And the ticket must not become a key to the rest of the workspace.
	if _, _, err := e.ResolvePreviewTicket(ctx, token, "../secrets.txt"); err == nil {
		t.Fatal("a preview ticket escaped its own folder")
	}
}

func TestHTMLPreviewAtAnAbsolutePathStillRuns(t *testing.T) {
	e := newArtifactEngine(t)
	ctx := context.Background()
	dir, err := e.tools.SessionFolder(artifactSession)
	if err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(dir, "index.html")
	if err := os.WriteFile(page, []byte(`<html><body><button onclick="save()">保存</button><script>function save(){localStorage.setItem("k","v")}</script></body></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"sessionId": artifactSession, "path": page})
	resp := handleWorkspaceArtifactPreview(e, ctx, artifactRequest(string(payload)))
	if !resp.OK {
		t.Fatalf("absolute html preview failed: %+v", resp)
	}
	raw, _ := json.Marshal(resp.Payload)
	var out struct {
		InteractiveURL string `json:"interactiveUrl"`
	}
	if json.Unmarshal(raw, &out) != nil || out.InteractiveURL == "" {
		t.Fatalf("absolute path left the page static: %s", raw)
	}
	if _, rel, ok := webviewhost.ParsePreviewRequest(out.InteractiveURL); !ok || rel != "index.html" {
		t.Fatalf("ticket url=%q", out.InteractiveURL)
	}
}

// Resolving a ticket answers with an absolute path on disk. That is the host's
// business and nothing else's: if the renderer could call it, a generated page
// that found its way to the bridge could read the path of any file it had a
// ticket for.
func TestPreviewResolveIsHostPrivate(t *testing.T) {
	const method = bridge.Method("internal.preview.asset.resolve")
	if _, ok := RuntimeHandlers[method]; ok {
		t.Fatal("internal.preview.asset.resolve must not be reachable from the renderer")
	}
	if _, ok := internalRuntimeHandlers[method]; !ok {
		t.Fatal("the host has no way to resolve a preview ticket")
	}
	if dataScopedMethod(string(method)) {
		t.Fatal("host-private preview RPC stays out of renderer data scope")
	}
}

// The engine builds preview URLs and the host parses them, and they do not share
// a constant (the engine does not import the host, exactly as media playback URLs
// work). So the two spellings are pinned against each other here: a URL the
// engine mints must be one the host accepts, addressing the file we meant.
func TestMintedURLIsOneTheHostWillServe(t *testing.T) {
	e := NewEngine(nil, "preview")
	url, err := e.MintPreviewTicket("01ARZ3NDEKTSV4RRFFQ69G5FAV", "reports/crm/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, webviewhost.PreviewOrigin+webviewhost.PreviewPathPrefix) {
		t.Fatalf("minted %q, which is not on the host's preview origin", url)
	}
	token, rel, ok := webviewhost.ParsePreviewRequest(url)
	if !ok {
		t.Fatalf("the host rejects the URL the engine minted: %q", url)
	}
	// Only the file name travels in the URL; the folder comes from the ticket, so
	// the document resolves to where it actually lives.
	if rel != "index.html" {
		t.Fatalf("URL carries %q, want just the file name", rel)
	}
	_, resolved, err := e.previewTickets.lookup(token, rel)
	if err != nil || resolved != "reports/crm/index.html" {
		t.Fatalf("document resolved to %q (%v), want reports/crm/index.html", resolved, err)
	}
	// A sibling asset the page requests resolves next to the document.
	if _, asset, err := e.previewTickets.lookup(token, "assets/app.js"); err != nil || asset != "reports/crm/assets/app.js" {
		t.Fatalf("asset resolved to %q (%v)", asset, err)
	}
}

// A ticket is a capability: it makes one folder of the user's workspace readable
// from an origin that runs scripts. So the tests that matter are the ones about
// what it must NOT reach, and about it going away.
func TestPreviewTicketResolvesSiblingsAndRefusesEscapes(t *testing.T) {
	store := newPreviewTicketStore()
	token, err := store.mint("01ARZ3NDEKTSV4RRFFQ69G5FAV", "reports/crm/index.html")
	if err != nil {
		t.Fatal(err)
	}

	// The document itself.
	session, rel, err := store.lookup(token, "")
	if err != nil || rel != "reports/crm/index.html" {
		t.Fatalf("document lookup = %q %q %v", session, rel, err)
	}
	if session != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("session = %q", session)
	}

	// Its own assets, resolved inside its folder — never at the session root.
	for asset, want := range map[string]string{
		"app.js":              "reports/crm/app.js",
		"assets/style.css":    "reports/crm/assets/style.css",
		"data/customers.json": "reports/crm/data/customers.json",
	} {
		if _, got, err := store.lookup(token, asset); err != nil || got != want {
			t.Errorf("asset %q = %q (%v), want %q", asset, got, err, want)
		}
	}

	// Anything climbing out of the folder must be refused here, even though the
	// host parser refuses it too. Two gates, because only one is guaranteed to
	// have run by the time a path reaches the filesystem.
	for _, asset := range []string{"../secrets.env", "../../.env", "a/../../b", "/etc/hosts", `..\\windows`, "C:/Windows/win.ini", "x\x00.js"} {
		if _, got, err := store.lookup(token, asset); err == nil {
			t.Errorf("accepted escape %q -> %q", asset, got)
		}
	}
}

func TestPreviewTicketRootDocumentHasNoFolderPrefix(t *testing.T) {
	store := newPreviewTicketStore()
	token, err := store.mint("01ARZ3NDEKTSV4RRFFQ69G5FAV", "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, rel, err := store.lookup(token, "app.js"); err != nil || rel != "app.js" {
		t.Fatalf("root-level asset = %q (%v)", rel, err)
	}
}

func TestPreviewTicketExpiresAndIsUnguessable(t *testing.T) {
	store := newPreviewTicketStore()
	now := time.Now()
	store.now = func() time.Time { return now }
	token, err := store.mint("01ARZ3NDEKTSV4RRFFQ69G5FAV", "index.html")
	if err != nil {
		t.Fatal(err)
	}
	// 24 random bytes, base64url: long enough that guessing is not a strategy.
	if len(token) < 30 || strings.ContainsAny(token, "/+=") {
		t.Fatalf("token %q is not a url-safe high-entropy string", token)
	}
	if _, _, err := store.lookup(token, ""); err != nil {
		t.Fatalf("fresh ticket rejected: %v", err)
	}
	now = now.Add(previewTicketTTL + time.Second)
	if _, _, err := store.lookup(token, ""); err == nil {
		t.Fatal("an expired ticket still resolves")
	}
	if _, _, err := store.lookup("nosuchtokennosuchtokennosuch", ""); err == nil {
		t.Fatal("an unknown ticket resolves")
	}
}

// Minting must be bounded, or a session that previews all day grows the store
// without limit. Eviction is by expiry order, so the newest tickets survive.
func TestPreviewTicketStoreStaysBounded(t *testing.T) {
	store := newPreviewTicketStore()
	base := time.Now()
	var newest string
	for i := 0; i < previewTicketMax*2; i++ {
		at := base.Add(time.Duration(i) * time.Second)
		store.now = func() time.Time { return at }
		token, err := store.mint("01ARZ3NDEKTSV4RRFFQ69G5FAV", "index.html")
		if err != nil {
			t.Fatal(err)
		}
		newest = token
	}
	store.now = func() time.Time { return base }
	if len(store.tickets) > previewTicketMax {
		t.Fatalf("store holds %d tickets, cap is %d", len(store.tickets), previewTicketMax)
	}
	if _, _, err := store.lookup(newest, ""); err != nil {
		t.Fatalf("eviction dropped the newest ticket: %v", err)
	}
}

func TestPreviewTicketRefusesUnusableDocumentPaths(t *testing.T) {
	store := newPreviewTicketStore()
	for _, rel := range []string{"", ".", "..", "../x.html", "/abs.html", "C:/x.html"} {
		if _, err := store.mint("01ARZ3NDEKTSV4RRFFQ69G5FAV", rel); err == nil {
			t.Errorf("minted a ticket for %q", rel)
		}
	}
	if _, err := store.mint("", "index.html"); err == nil {
		t.Error("minted a ticket with no session")
	}
}
