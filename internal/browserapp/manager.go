package browserapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/webviewhost"
)

type BrowserHost interface {
	Run(context.Context) error
	Close() error
}

type Factory func(webviewhost.BrowserHostOptions) (BrowserHost, error)

type managedHost struct {
	host   BrowserHost
	cancel context.CancelFunc
	done   chan error
}

// Manager owns the process-wide isolated browser window. Every operation is
// serialized, and every host lifecycle context is owned and cancelled here.
type Manager struct {
	mu             sync.Mutex
	factory        Factory
	browserProfile string
	mainProfile    string
	current        *managedHost
	shutdown       bool
	errors         chan error
}

func New(browserProfile, mainProfile string, factory Factory) (*Manager, error) {
	if browserProfile == "" || mainProfile == "" {
		return nil, errors.New("browser and main profile paths are required")
	}
	if factory == nil {
		factory = func(o webviewhost.BrowserHostOptions) (BrowserHost, error) { return webviewhost.NewBrowserHost(o) }
	}
	return &Manager{factory: factory, browserProfile: browserProfile, mainProfile: mainProfile, errors: make(chan error, 1)}, nil
}

// Errors reports asynchronous host failures without blocking the lifecycle.
func (m *Manager) Errors() <-chan error { return m.errors }

func (m *Manager) HandleHost(ctx context.Context, request bridge.Request) bridge.Response {
	switch bridge.Method(request.Method) {
	case bridge.MethodBrowserOpen:
		var payload struct {
			URL string `json:"url"`
		}
		if err := decodeStrict(request.Payload, &payload); err != nil {
			return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "浏览器请求格式无效", false)
		}
		url, err := m.Open(ctx, payload.URL)
		if err != nil {
			return bridge.Failure(request.ID, request.TraceID, "BROWSER_OPEN_FAILED", err.Error(), false)
		}
		// BrowserHost has no readiness signal. Opening means accepted, not ready.
		return bridge.Success(request.ID, map[string]string{"status": "opening", "url": url})
	case bridge.MethodBrowserClose:
		var payload struct{}
		if err := decodeStrict(request.Payload, &payload); err != nil {
			return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "浏览器请求格式无效", false)
		}
		if err := m.Close(ctx); err != nil {
			return bridge.Failure(request.ID, request.TraceID, "BROWSER_CLOSE_FAILED", err.Error(), true)
		}
		return bridge.Success(request.ID, map[string]string{"status": "closed"})
	default:
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_METHOD_NOT_ALLOWED", "浏览器方法不受支持", false)
	}
}

func (m *Manager) Open(ctx context.Context, rawURL string) (string, error) {
	// Validate before taking down a working host.
	url, err := webviewhost.NormalizeBrowserURL(rawURL)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shutdown {
		return "", errors.New("browser manager is shut down")
	}
	next, err := m.factory(webviewhost.BrowserHostOptions{InitialURL: url, UserDataFolder: m.browserProfile, MainUserDataFolder: m.mainProfile})
	if err != nil {
		return "", err
	}
	if err = m.drainLocked(ctx); err != nil {
		return "", err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	entry := &managedHost{host: next, cancel: cancel, done: make(chan error, 1)}
	m.current = entry
	go func() {
		err := next.Run(runCtx)
		entry.done <- err
		close(entry.done)
		m.mu.Lock()
		current := m.current == entry
		if current {
			m.current = nil
		}
		m.mu.Unlock()
		if current && err != nil && !errors.Is(err, context.Canceled) {
			select {
			case m.errors <- err:
			default:
			}
		}
	}()
	return url, nil
}

func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.drainLocked(ctx)
}
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shutdown = true
	return m.drainLocked(ctx)
}

func (m *Manager) drainLocked(ctx context.Context) error {
	old := m.current
	if old == nil {
		return nil
	}
	m.current = nil
	old.cancel()
	closeErr := old.host.Close()
	select {
	case runErr := <-old.done:
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			return fmt.Errorf("browser host stopped: %w", runErr)
		}
		return nil
	case <-ctx.Done():
		if closeErr != nil {
			return fmt.Errorf("%w (close request: %v)", ctx.Err(), closeErr)
		}
		return ctx.Err()
	}
}

func decodeStrict(raw []byte, dst any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) < 2 || trimmed[0] != '{' {
		return errors.New("payload must be a JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("payload must contain one JSON object")
	}
	return nil
}
