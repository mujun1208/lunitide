package browserapp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/webviewhost"
)

type fakeHost struct {
	mu      sync.Mutex
	cancel  bool
	started chan struct{}
	done    chan struct{}
	runErr  error
}

func newFakeHost() *fakeHost {
	return &fakeHost{started: make(chan struct{}), done: make(chan struct{})}
}
func (f *fakeHost) Run(ctx context.Context) error {
	close(f.started)
	<-ctx.Done()
	f.mu.Lock()
	f.cancel = true
	f.mu.Unlock()
	close(f.done)
	return f.runErr
}
func (f *fakeHost) Close() error { return nil }

func TestOpenNormalizesBeforeReplacingAndUsesSeparateProfiles(t *testing.T) {
	var hosts []*fakeHost
	var options []webviewhost.BrowserHostOptions
	m, _ := New(`C:\browser`, `C:\main`, func(o webviewhost.BrowserHostOptions) (BrowserHost, error) {
		options = append(options, o)
		h := newFakeHost()
		hosts = append(hosts, h)
		return h, nil
	})
	got, err := m.Open(context.Background(), "https://EXAMPLE.com:443/path")
	if err != nil || got != "https://example.com/path" {
		t.Fatalf("open=(%q,%v)", got, err)
	}
	<-hosts[0].started
	if _, err = m.Open(context.Background(), "http://unsafe.example"); err == nil {
		t.Fatal("unsafe replacement accepted")
	}
	select {
	case <-hosts[0].done:
		t.Fatal("old host drained for invalid replacement")
	default:
	}
	if _, err = m.Open(context.Background(), "https://second.example/"); err != nil {
		t.Fatal(err)
	}
	if options[1].UserDataFolder == options[1].MainUserDataFolder || options[1].UserDataFolder != `C:\browser` {
		t.Fatalf("profiles not isolated: %+v", options[1])
	}
	select {
	case <-hosts[0].done:
	case <-time.After(time.Second):
		t.Fatal("old host was not drained")
	}
}

func TestCloseIsIdempotentAndShutdownRejectsOpen(t *testing.T) {
	m, _ := New("browser", "main", func(webviewhost.BrowserHostOptions) (BrowserHost, error) { return newFakeHost(), nil })
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Open(context.Background(), "https://example.com/"); err == nil {
		t.Fatal("open accepted after shutdown")
	}
}

func TestAsyncRunErrorIsReported(t *testing.T) {
	sentinel := errors.New("boom")
	m, _ := New("browser", "main", func(webviewhost.BrowserHostOptions) (BrowserHost, error) {
		h := newFakeHost()
		h.runErr = sentinel
		return h, nil
	})
	if _, err := m.Open(context.Background(), "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	h := m.current
	m.mu.Unlock()
	h.cancel()
	select {
	case err := <-m.Errors():
		if !errors.Is(err, sentinel) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("async error not reported")
	}
}
