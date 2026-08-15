package m5

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/browser"
	"github.com/lunitide/lunitide/internal/domain/m5workspace"
)

// fakePageDriver scripts the host-side page driver.
type fakePageDriver struct {
	mu        sync.Mutex
	navigates []string
	clicks    []string
	types     []browserTypeCall
	readText  string
	snapshot  []byte
	current   string
}

type browserTypeCall struct {
	refID, text string
}

func (f *fakePageDriver) Navigate(u string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.navigates = append(f.navigates, u)
	f.current = u
	return "Example Domain", nil
}

func (f *fakePageDriver) Read() (string, error) { return f.readText, nil }

func (f *fakePageDriver) Click(refID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clicks = append(f.clicks, refID)
	return nil
}

func (f *fakePageDriver) Type(refID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.types = append(f.types, browserTypeCall{refID: refID, text: text})
	return nil
}

func (f *fakePageDriver) Snapshot() ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshot, nil
}

func (f *fakePageDriver) CurrentURL() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current
}

// fakeSink journals Register calls and hashes data the way the real
// registry does, so tests can compare the recorded sha256 against the
// bytes the driver produced.
type fakeSink struct {
	mu    sync.Mutex
	calls []fakeSinkCall
}

type fakeSinkCall struct {
	runID, mime, generator, sha string
	data                        []byte
}

func (f *fakeSink) Register(ctx context.Context, runID, mime, generator string, data []byte) (m5workspace.Artifact, error) {
	sum := sha256.Sum256(data)
	call := fakeSinkCall{
		runID: runID, mime: mime, generator: generator,
		sha: hex.EncodeToString(sum[:]), data: append([]byte(nil), data...),
	}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
	return m5workspace.Artifact{
		ID: "art-snap-1", RunID: runID, Mime: mime,
		Size: int64(len(data)), SHA256: call.sha, Generator: generator,
	}, nil
}

func publicBrowserResolver(host string) ([]net.IP, error) {
	return []net.IP{net.ParseIP("93.184.216.34")}, nil
}

// TestBrowserAct covers T-5.4.3: the URL policy re-enforced at the bridge,
// the four page ops, snapshot artifact journalling, read truncation and
// the refusal of sensitive ops (BRW-001).
func TestBrowserAct(t *testing.T) {
	ctx := context.Background()
	driver := &fakePageDriver{}
	sink := &fakeSink{}
	svc := NewBrowserService(driver, sink)
	svc.SetResolver(publicBrowserResolver)

	t.Run("navigate ok advances events", func(t *testing.T) {
		res, err := svc.Op(ctx, BrowserInput{Op: "navigate", URL: "https://example.com", RunID: "run-1"})
		if err != nil {
			t.Fatal(err)
		}
		if !res.OK || res.Title != "Example Domain" || res.URL != "https://example.com" || res.EventSeq == 0 {
			t.Fatalf("navigate = %+v", res)
		}
		first := res.EventSeq
		res2, err := svc.Op(ctx, BrowserInput{Op: "navigate", URL: "https://example.org/index", RunID: "run-1"})
		if err != nil {
			t.Fatal(err)
		}
		if res2.EventSeq <= first {
			t.Fatalf("event seq did not advance: %d then %d", first, res2.EventSeq)
		}
		if len(driver.navigates) != 2 {
			t.Fatalf("driver navigates = %v", driver.navigates)
		}
	})

	t.Run("navigate file blocked at bridge", func(t *testing.T) {
		_, err := svc.Op(ctx, BrowserInput{Op: "navigate", URL: "file:///C:/Windows/system32/config.sys"})
		if !errors.Is(err, browser.ErrProtocolBlocked) {
			t.Fatalf("file navigate err = %v, want browser.ErrProtocolBlocked", err)
		}
	})

	t.Run("navigate loopback blocked at bridge", func(t *testing.T) {
		_, err := svc.Op(ctx, BrowserInput{Op: "navigate", URL: "http://127.0.0.1:8080/"})
		if !errors.Is(err, browser.ErrLoopbackBlocked) {
			t.Fatalf("loopback navigate err = %v, want browser.ErrLoopbackBlocked", err)
		}
	})

	t.Run("navigate rebinding blocked", func(t *testing.T) {
		svc.SetResolver(func(host string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("10.0.0.7")}, nil
		})
		t.Cleanup(func() { svc.SetResolver(publicBrowserResolver) })
		_, err := svc.Op(ctx, BrowserInput{Op: "navigate", URL: "https://evil.example/"})
		if !errors.Is(err, browser.ErrPrivateAddress) {
			t.Fatalf("rebind navigate err = %v, want browser.ErrPrivateAddress", err)
		}
	})

	t.Run("snapshot journals png artifact", func(t *testing.T) {
		png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3}
		driver.mu.Lock()
		driver.snapshot = png
		driver.mu.Unlock()

		res, err := svc.Op(ctx, BrowserInput{Op: "snapshot", RunID: "run-1"})
		if err != nil {
			t.Fatal(err)
		}
		if !res.OK || res.SnapshotArtifactID != "art-snap-1" {
			t.Fatalf("snapshot = %+v", res)
		}

		sink.mu.Lock()
		defer sink.mu.Unlock()
		if len(sink.calls) != 1 {
			t.Fatalf("sink calls = %d", len(sink.calls))
		}
		c := sink.calls[0]
		if c.runID != "run-1" || c.mime != "image/png" || c.generator != "browser.snapshot" {
			t.Fatalf("sink call = %+v", c)
		}
		want := sha256.Sum256(png)
		if c.sha != hex.EncodeToString(want[:]) {
			t.Fatalf("sha mismatch: recorded %s, data %s", c.sha, hex.EncodeToString(want[:]))
		}
		if len(c.data) != len(png) {
			t.Fatalf("recorded data len = %d, want %d", len(c.data), len(png))
		}
	})

	t.Run("click requires selector", func(t *testing.T) {
		if _, err := svc.Op(ctx, BrowserInput{Op: "click"}); !errors.Is(err, ErrBrowserSelectorRequired) {
			t.Fatalf("click without selector err = %v", err)
		}
	})

	t.Run("click and type by selector", func(t *testing.T) {
		if _, err := svc.Op(ctx, BrowserInput{Op: "click", Selector: "#submit"}); err != nil {
			t.Fatal(err)
		}
		res, err := svc.Op(ctx, BrowserInput{Op: "type", Selector: "#q", Text: "lunitide"})
		if err != nil || !res.OK {
			t.Fatalf("type = %+v, %v", res, err)
		}
		if len(driver.clicks) != 1 || driver.clicks[0] != "#submit" {
			t.Fatalf("clicks = %v", driver.clicks)
		}
		if len(driver.types) != 1 || driver.types[0].refID != "#q" || driver.types[0].text != "lunitide" {
			t.Fatalf("types = %+v", driver.types)
		}
	})

	t.Run("read truncates at 256kib", func(t *testing.T) {
		driver.mu.Lock()
		driver.readText = strings.Repeat("a", 300*1024)
		driver.mu.Unlock()

		res, err := svc.Op(ctx, BrowserInput{Op: "read"})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.TextContent) != BrowserReadMaxBytes {
			t.Fatalf("read len = %d, want %d", len(res.TextContent), BrowserReadMaxBytes)
		}
	})

	t.Run("sensitive op refused", func(t *testing.T) {
		for _, op := range []string{"download", "upload", "login", "pay", ""} {
			if _, err := svc.Op(ctx, BrowserInput{Op: op}); !errors.Is(err, ErrBrowserOpInvalid) {
				t.Fatalf("op %q err = %v, want ErrBrowserOpInvalid", op, err)
			}
		}
	})
}
