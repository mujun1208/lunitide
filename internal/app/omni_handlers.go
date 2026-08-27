package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/omni"
	"github.com/lunitide/lunitide/internal/voice"
)

// OmniService is the MiniCPM-o 4.5 duplex channel. Isolated from TTS and ASR.
type OmniService struct {
	host      *omni.Host
	installer *voice.Installer

	mu         sync.Mutex
	sessions   map[string]*omni.Session
	counter    atomic.Uint64
	installing bool
	progress   voice.Progress
	state      string
	lastErr    string
}

// NewOmniService wires downloads and the loopback llama-omni-server under root.
func NewOmniService(root string) *OmniService {
	return &OmniService{
		host:      omni.NewHost(root),
		installer: &voice.Installer{Root: root},
		sessions:  map[string]*omni.Session{},
		state:     "idle",
	}
}

// Close stops sessions and the hosted server. Safe twice.
func (s *OmniService) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	for id, session := range s.sessions {
		session.Close()
		delete(s.sessions, id)
	}
	s.mu.Unlock()
	s.host.Stop()
}

func (s *OmniService) closeSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, session := range s.sessions {
		session.Close()
		delete(s.sessions, id)
	}
}

// SetOmniService wires the MiniCPM-o duplex path.
func (e *Engine) SetOmniService(svc *OmniService) { e.omni = svc }

func handleOmniStatus(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.omni == nil {
		return bridge.Success(r.ID, map[string]any{
			"supported": false, "ready": false, "installed": false, "runtimeFound": false,
			"hostState": omni.HostIdle, "downloadBytes": 0, "title": "",
			"percent": 0, "doneBytes": 0, "totalBytes": 0,
		})
	}
	return bridge.Success(r.ID, e.omni.status())
}

func handleOmniInstall(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.omni == nil {
		return bridge.Failure(r.ID, r.TraceID, "OMNI-001", "本机 MiniCPM-o 不可用", true)
	}
	e.omni.beginInstall()
	return bridge.Success(r.ID, e.omni.installSnapshot())
}

func handleOmniEnsure(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.omni == nil {
		return bridge.Failure(r.ID, r.TraceID, "OMNI-001", "本机 MiniCPM-o 不可用", true)
	}
	state, err := e.omni.host.Ensure()
	last := ""
	if err != nil {
		last = truncate(err.Error(), 512)
	}
	return bridge.Success(r.ID, map[string]any{
		"hostState": state,
		"ready":     e.omni.host.Healthy(),
		"lastError": last,
	})
}

func handleOmniStart(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.omni == nil {
		return bridge.Failure(r.ID, r.TraceID, "OMNI-001", "本机 MiniCPM-o 不可用", true)
	}
	var p struct {
		PersonaID string `json:"personaId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "omni.start 参数无效", false)
	}
	session, err := e.omni.start(ctx, p.PersonaID)
	if err != nil {
		switch {
		case errors.Is(err, omni.ErrMissingModel):
			return bridge.Failure(r.ID, r.TraceID, "OMNI-001", "请先在设置里下载 MiniCPM-o 4.5 Q4", true)
		case errors.Is(err, omni.ErrMissingRuntime):
			return bridge.Failure(r.ID, r.TraceID, "OMNI-002", "未找到 llama-omni-server，请放到月汐数据目录 omni/runtime/", true)
		default:
			return bridge.Failure(r.ID, r.TraceID, "OMNI-003", "MiniCPM-o 启动失败："+truncate(err.Error(), 256), true)
		}
	}
	id := fmt.Sprintf("o%d", e.omni.counter.Add(1))
	e.omni.mu.Lock()
	e.omni.sessions[id] = session
	e.omni.mu.Unlock()
	return bridge.Success(r.ID, map[string]any{"sessionId": id})
}

func handleOmniAppend(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
		PCM       string `json:"pcm"`
	}
	if decodePayload(r.Payload, &p) != nil || p.SessionID == "" || p.PCM == "" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "omni.append 参数无效", false)
	}
	pcm, err := base64.StdEncoding.DecodeString(p.PCM)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "omni.append 音频编码无效", false)
	}
	session, ok := e.omniSession(p.SessionID)
	if !ok {
		return bridge.Failure(r.ID, r.TraceID, "OMNI-004", "语音会话已结束", false)
	}
	turn, err := session.Append(ctx, pcm)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "OMNI-005", "MiniCPM-o 推理失败："+truncate(err.Error(), 256), true)
	}
	wavs := turn.WAVs
	if wavs == nil {
		wavs = []string{}
	}
	return bridge.Success(r.ID, map[string]any{
		"text":      turn.Text,
		"listening": turn.Listening,
		"wavs":      wavs,
	})
}

func handleOmniStop(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "omni.stop 参数无效", false)
	}
	if e.omni == nil {
		return bridge.Success(r.ID, map[string]any{"notice": "OMNI_SESSION_CLOSED"})
	}
	if p.SessionID == "" {
		e.omni.closeSessions()
		return bridge.Success(r.ID, map[string]any{"notice": "OMNI_SESSION_CLOSED"})
	}
	if session, ok := e.takeOmniSession(p.SessionID); ok {
		session.Close()
	}
	return bridge.Success(r.ID, map[string]any{"notice": "OMNI_SESSION_CLOSED"})
}

func (e *Engine) omniSession(id string) (*omni.Session, bool) {
	if e.omni == nil {
		return nil, false
	}
	e.omni.mu.Lock()
	defer e.omni.mu.Unlock()
	session, ok := e.omni.sessions[id]
	return session, ok
}

func (e *Engine) takeOmniSession(id string) (*omni.Session, bool) {
	if e.omni == nil {
		return nil, false
	}
	e.omni.mu.Lock()
	defer e.omni.mu.Unlock()
	session, ok := e.omni.sessions[id]
	delete(e.omni.sessions, id)
	return session, ok
}

func (s *OmniService) start(ctx context.Context, personaID string) (*omni.Session, error) {
	if !s.host.Installed() {
		return nil, omni.ErrMissingModel
	}
	if !s.host.Healthy() {
		if _, err := s.host.Ensure(); err != nil {
			return nil, err
		}
		if err := s.host.WaitReady(ctx.Done()); err != nil {
			return nil, err
		}
	}
	return omni.OpenSession(ctx, s.host, personaID)
}

func (s *OmniService) status() map[string]any {
	out := s.host.Snapshot()
	s.mu.Lock()
	defer s.mu.Unlock()
	out["percent"] = s.progress.Percent()
	out["doneBytes"] = s.progress.Done
	out["totalBytes"] = s.progress.Total
	if s.progress.File != "" {
		out["file"] = s.progress.File
	}
	if s.state == "downloading" {
		out["hostState"] = omni.HostDownloading
	}
	if s.lastErr != "" {
		out["lastError"] = truncate(s.lastErr, 512)
	}
	return out
}

func (s *OmniService) installSnapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]any{
		"state":      s.state,
		"percent":    s.progress.Percent(),
		"doneBytes":  s.progress.Done,
		"totalBytes": s.progress.Total,
	}
	if s.progress.File != "" {
		out["file"] = s.progress.File
	}
	if s.lastErr != "" {
		out["lastError"] = truncate(s.lastErr, 512)
	}
	return out
}

func (s *OmniService) beginInstall() {
	s.mu.Lock()
	if s.installing {
		s.mu.Unlock()
		return
	}
	modelOK := s.installer.Present(omni.ModelBundle())
	runtimeOK := s.host.RuntimePath() != ""
	if modelOK && runtimeOK {
		s.state, s.lastErr = "ready", ""
		s.mu.Unlock()
		return
	}
	s.installing, s.state, s.lastErr = true, "downloading", ""
	s.mu.Unlock()
	go s.runInstall()
}

func (s *OmniService) runInstall() {
	defer func() {
		s.mu.Lock()
		s.installing = false
		s.mu.Unlock()
	}()
	ctx := context.Background()
	if !s.installer.Present(omni.ModelBundle()) {
		err := s.installer.Install(ctx, omni.ModelBundle(), func(p voice.Progress) {
			s.mu.Lock()
			s.progress = p
			s.mu.Unlock()
		})
		if err != nil {
			s.mu.Lock()
			s.state, s.lastErr = "failed", err.Error()
			s.mu.Unlock()
			return
		}
	}
	if s.host.RuntimePath() == "" {
		err := omni.InstallRuntime(ctx, s.host.Root, s.installer, func(p voice.Progress) {
			s.mu.Lock()
			s.progress = p
			s.mu.Unlock()
		})
		if err != nil {
			s.mu.Lock()
			s.state, s.lastErr = "failed", err.Error()
			s.mu.Unlock()
			return
		}
	}
	s.mu.Lock()
	s.state, s.lastErr = "ready", ""
	s.mu.Unlock()
}
