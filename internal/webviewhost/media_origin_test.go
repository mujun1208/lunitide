package webviewhost

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/mediaapp"
)

func TestParseMediaAssetTicket(t *testing.T) {
	token := strings.Repeat("ab", 16)
	good := PlaybackURL(token)
	got, ok := ParseMediaAssetTicket(good)
	if !ok || got != token {
		t.Fatalf("good ticket %q ok=%v got=%q", good, ok, got)
	}
	for _, raw := range []string{
		"http://media.lunitide.local/v1/assets/" + token,
		"https://media.lunitide.local.evil/v1/assets/" + token,
		"https://user:pass@media.lunitide.local/v1/assets/" + token,
		"https://media.lunitide.local:444/v1/assets/" + token,
		"https://media.lunitide.local/v1/assets/" + token + "?x=1",
		"https://media.lunitide.local/v1/assets/" + token + "#frag",
		"https://media.lunitide.local/v1/assets/" + token + "/extra",
		"https://media.lunitide.local/v1/assets/",
		"https://app.lunitide.local/v1/assets/" + token,
		"https://media.lunitide.local/v1/assets/../" + token,
	} {
		if _, parsed := ParseMediaAssetTicket(raw); parsed {
			t.Fatalf("accepted illegal media URL %q", raw)
		}
	}
}

func TestDeliverMediaResponseCompletesAndReleasesInsideWait(t *testing.T) {
	var inside bool
	var leaked []string
	wait := func(fn func() bool) bool {
		inside = true
		defer func() { inside = false }()
		return fn()
	}
	deliverMediaResponse(wait, func() {
		if !inside {
			leaked = append(leaked, "complete")
		}
	}, func() {
		if !inside {
			leaked = append(leaked, "release")
		}
	})
	if len(leaked) > 0 {
		t.Fatalf("COM work escaped UI wait: %v", leaked)
	}
}

func TestDeliverMediaResponseStillFinishesWhenWaitRejected(t *testing.T) {
	var completed, released bool
	deliverMediaResponse(func(func() bool) bool { return false }, func() { completed = true }, func() { released = true })
	if !completed || !released {
		t.Fatal("shutdown path must still complete and release")
	}
}

func TestMediaResourceFilterInterceptsAllContexts(t *testing.T) {
	if MediaResourceContextAll != 0 {
		t.Fatalf("ALL context must be 0, got %d", MediaResourceContextAll)
	}
	if MediaResourceContextMedia == MediaResourceContextAll {
		t.Fatal("media context must stay distinct from ALL")
	}
}

func TestMediaResourceAllowedRequiresAppDocumentAndMediaContext(t *testing.T) {
	token := strings.Repeat("cd", 16)
	req := PlaybackURL(token)
	if !MediaResourceAllowed(TrustedOrigin+"/index.html", MediaResourceContextMedia, req) {
		t.Fatal("trusted media request rejected")
	}
	if MediaResourceAllowed(TrustedOrigin+"/index.html", 1, req) {
		t.Fatal("document context must fail closed")
	}
	if MediaResourceAllowed("https://evil.example/", MediaResourceContextMedia, req) {
		t.Fatal("untrusted source must fail closed")
	}
}

func TestMediaResponseHeadersAreNosniffNoStore(t *testing.T) {
	headers := MediaResponseHeaders(mediaapp.RangeResult{
		Status:        http.StatusPartialContent,
		ContentType:   "audio/mpeg",
		AcceptRanges:  "bytes",
		ContentRange:  "bytes 0-9/100",
		ContentLength: 10,
	})
	for _, want := range []string{
		"Content-Type: audio/mpeg",
		"Content-Length: 10",
		"Accept-Ranges: bytes",
		"Content-Range: bytes 0-9/100",
		"X-Content-Type-Options: nosniff",
		"Cache-Control: no-store",
	} {
		if !strings.Contains(headers, want) {
			t.Fatalf("missing %q in %q", want, headers)
		}
	}
}

func PlaybackURL(token string) string {
	return "https://" + MediaVirtualHost + MediaAssetPathPrefix + token
}
