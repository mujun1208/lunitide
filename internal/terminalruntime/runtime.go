package terminalruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrUnsupported = errors.New("terminal runtime is unsupported on this platform")
	ErrClosed      = errors.New("terminal runtime is shut down")
	ErrNotFound    = errors.New("terminal session not found")
	ErrLimit       = errors.New("terminal runtime limit exceeded")
	ErrInvalid     = errors.New("invalid terminal request")
)

type EventType string

const (
	EventStarted EventType = "started"
	EventOutput  EventType = "output"
	EventExit    EventType = "exit"
	EventError   EventType = "error"
)

type Event struct {
	Type      EventType
	SessionID string
	Data      []byte
	ExitCode  uint32
	Err       error
}

type Config struct {
	Workspace      string
	AuditPath      string
	MaxSessions    int
	MaxInputBytes  int
	MaxOutputBytes int64
	EventBuffer    int
}

type Runtime struct {
	mu       sync.Mutex
	cfg      Config
	root     string
	sessions map[string]*session
	events   chan Event
	closed   bool
}

type session struct {
	id           string
	p            platformSession
	output       int64
	outputDigest hashState
}

type hashState struct {
	mu   sync.Mutex
	data []byte
}

func (h *hashState) add(p []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	x := sha256.Sum256(p)
	h.data = append(h.data, x[:]...)
}
func (h *hashState) digest() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	x := sha256.Sum256(h.data)
	return hex.EncodeToString(x[:])
}

type platformSession interface {
	write([]byte) error
	resize(uint16, uint16) error
	close() error
}

func New(cfg Config) (*Runtime, error) {
	if cfg.Workspace == "" || !filepath.IsAbs(cfg.Workspace) {
		return nil, fmt.Errorf("%w: workspace must be absolute", ErrInvalid)
	}
	real, err := filepath.EvalSymlinks(cfg.Workspace)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(real)
	if err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%w: workspace", ErrInvalid)
	}
	if cfg.MaxSessions == 0 {
		cfg.MaxSessions = 4
	}
	if cfg.MaxInputBytes == 0 {
		cfg.MaxInputBytes = 64 << 10
	}
	if cfg.MaxOutputBytes == 0 {
		cfg.MaxOutputBytes = 4 << 20
	}
	if cfg.EventBuffer == 0 {
		cfg.EventBuffer = 128
	}
	if cfg.MaxSessions < 1 || cfg.MaxSessions > 32 || cfg.MaxInputBytes < 1 || cfg.MaxInputBytes > 1<<20 || cfg.MaxOutputBytes < 1 || cfg.MaxOutputBytes > 64<<20 || cfg.EventBuffer < 1 || cfg.EventBuffer > 4096 {
		return nil, ErrInvalid
	}
	return &Runtime{cfg: cfg, root: filepath.Clean(real), sessions: make(map[string]*session), events: make(chan Event, cfg.EventBuffer)}, nil
}

func (r *Runtime) Events() <-chan Event { return r.events }

func validID(s string) bool {
	if len(s) < 1 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
func validSize(cols, rows int) bool { return cols >= 1 && cols <= 500 && rows >= 1 && rows <= 500 }

func (r *Runtime) Start(ctx context.Context, id string, cols, rows int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !validID(id) || !validSize(cols, rows) {
		return ErrInvalid
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrClosed
	}
	if len(r.sessions) >= r.cfg.MaxSessions {
		r.mu.Unlock()
		return ErrLimit
	}
	if _, ok := r.sessions[id]; ok {
		r.mu.Unlock()
		return ErrInvalid
	}
	s := &session{id: id}
	r.sessions[id] = s
	r.mu.Unlock()
	p, err := startPlatform(r.root, uint16(cols), uint16(rows), func(b []byte) { r.output(s, b) }, func(code uint32, e error) { r.exited(s, code, e) })
	if err != nil {
		r.mu.Lock()
		delete(r.sessions, id)
		r.mu.Unlock()
		r.audit("start_failed", id, nil, err)
		return err
	}
	r.mu.Lock()
	if r.sessions[id] != s {
		r.mu.Unlock()
		_ = p.close()
		return ErrClosed
	}
	s.p = p
	r.mu.Unlock()
	r.emit(Event{Type: EventStarted, SessionID: id})
	r.audit("start", id, nil, nil)
	select {
	case <-ctx.Done():
		_ = r.Close(id)
		return ctx.Err()
	default:
		return nil
	}
}

func (r *Runtime) Write(id string, data []byte) error {
	if len(data) == 0 || len(data) > r.cfg.MaxInputBytes {
		return ErrInvalid
	}
	r.mu.Lock()
	s := r.sessions[id]
	var p platformSession
	if s != nil {
		p = s.p
	}
	r.mu.Unlock()
	if p == nil {
		return ErrNotFound
	}
	err := p.write(data)
	r.audit("write", id, data, err)
	return err
}
func (r *Runtime) Resize(id string, cols, rows int) error {
	if !validSize(cols, rows) {
		return ErrInvalid
	}
	r.mu.Lock()
	s := r.sessions[id]
	var p platformSession
	if s != nil {
		p = s.p
	}
	r.mu.Unlock()
	if p == nil {
		return ErrNotFound
	}
	err := p.resize(uint16(cols), uint16(rows))
	r.audit("resize", id, []byte(fmt.Sprintf("%dx%d", cols, rows)), err)
	return err
}
func (r *Runtime) Close(id string) error {
	r.mu.Lock()
	s := r.sessions[id]
	if s != nil {
		delete(r.sessions, id)
	}
	r.mu.Unlock()
	if s == nil {
		return ErrNotFound
	}
	err := s.p.close()
	r.audit("close", id, []byte(s.outputDigest.digest()), err)
	return err
}
func (r *Runtime) Shutdown() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	all := make([]*session, 0, len(r.sessions))
	for _, s := range r.sessions {
		all = append(all, s)
	}
	r.sessions = make(map[string]*session)
	r.mu.Unlock()
	var first error
	for _, s := range all {
		if e := s.p.close(); e != nil && first == nil {
			first = e
		}
		r.audit("shutdown_close", s.id, []byte(s.outputDigest.digest()), nil)
	}
	close(r.events)
	return first
}
func (r *Runtime) output(s *session, b []byte) {
	r.mu.Lock()
	cur, ok := r.sessions[s.id]
	if !ok || cur != s {
		r.mu.Unlock()
		return
	}
	remain := r.cfg.MaxOutputBytes - s.output
	if remain <= 0 {
		r.mu.Unlock()
		return
	}
	if int64(len(b)) > remain {
		b = b[:remain]
	}
	s.output += int64(len(b))
	r.mu.Unlock()
	b = append([]byte(nil), b...)
	s.outputDigest.add(b)
	r.emit(Event{Type: EventOutput, SessionID: s.id, Data: b})
}
func (r *Runtime) exited(s *session, code uint32, err error) {
	r.mu.Lock()
	if r.sessions[s.id] == s {
		delete(r.sessions, s.id)
	}
	r.mu.Unlock()
	typ := EventExit
	if err != nil {
		typ = EventError
	}
	r.emit(Event{Type: typ, SessionID: s.id, ExitCode: code, Err: err})
	r.audit("exit", s.id, []byte(s.outputDigest.digest()), err)
}
func (r *Runtime) emit(e Event) {
	defer func() { _ = recover() }()
	select {
	case r.events <- e:
	default:
	}
}

type auditRecord struct {
	At        time.Time `json:"at"`
	Action    string    `json:"action"`
	SessionID string    `json:"session_id"`
	Digest    string    `json:"digest"`
	Error     string    `json:"error,omitempty"`
}

func (r *Runtime) audit(action, id string, data []byte, err error) {
	if r.cfg.AuditPath == "" {
		return
	}
	h := sha256.Sum256(data)
	rec := auditRecord{At: time.Now().UTC(), Action: action, SessionID: id, Digest: hex.EncodeToString(h[:])}
	if err != nil {
		rec.Error = err.Error()
	}
	b, _ := json.Marshal(rec)
	b = append(b, '\n')
	f, e := os.OpenFile(r.cfg.AuditPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e == nil {
		_, _ = f.Write(b)
		_ = f.Close()
	}
}

func sanitizedEnvironment(systemRoot string, workspace ...string) []string {
	systemRoot = filepath.Clean(systemRoot)
	profile := filepath.Join(systemRoot, "Temp")
	if len(workspace) > 0 {
		profile = workspace[0]
	}
	temp := filepath.Join(profile, ".terminal-tmp")
	env := []string{"SystemRoot=" + systemRoot, "WINDIR=" + systemRoot, "SystemDrive=" + filepath.VolumeName(systemRoot), "COMSPEC=" + filepath.Join(systemRoot, "System32", "cmd.exe"), "PATH=" + strings.Join([]string{filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0"), filepath.Join(systemRoot, "System32")}, string(os.PathListSeparator)), "PATHEXT=.COM;.EXE;.BAT;.CMD", "OS=Windows_NT", "USERPROFILE=" + profile, "HOME=" + profile, "LOCALAPPDATA=" + filepath.Join(profile, "AppData", "Local"), "APPDATA=" + filepath.Join(profile, "AppData", "Roaming"), "TEMP=" + temp, "TMP=" + temp, "POWERSHELL_TELEMETRY_OPTOUT=1"}
	for _, key := range []string{"PROCESSOR_ARCHITECTURE", "PROCESSOR_IDENTIFIER", "PROCESSOR_LEVEL", "PROCESSOR_REVISION", "NUMBER_OF_PROCESSORS"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	return env
}
