// Local speech recognition bridge handlers: voice.status / voice.install /
// voice.start / voice.append / voice.finish / voice.stop.
//
// Audio arrives one frame at a time and each frame's response carries back
// whatever the recognizer has heard so far. That is deliberately not a
// streaming channel: frames already flow at ten per second, so the reply to
// frame N is a free ride for the partial transcript produced by frame N-1,
// and it arrives at exactly the cadence a caption can redraw at. A separate
// event stream would add a second ordering to reason about and buy nothing.
package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/voice"
	"github.com/lunitide/lunitide/internal/voice/volcsauc"
)

// VoiceService owns the recognizer and the sessions in flight.
type VoiceService struct {
	backend   voice.Backend
	refiner   *voice.Refiner
	installer *voice.Installer
	modelID   string

	mu       sync.Mutex
	sessions map[string]voice.Session
	// progress is the state of the current download, read by voice.install
	// while a transfer is running.
	progress voice.Progress
	state    string
	lastErr  string
	// installing guards against a second download of the same model, which
	// would race the first over the same files.
	installing bool

	counter atomic.Uint64

	// One load at a time per engine. The status call that triggers a warm-up
	// is made by every screen that mentions voice, and a model load holds a
	// mutex for seconds, so without this they would queue up behind each
	// other and keep loading long after the first one succeeded.
	warmingStream  warmOnce
	warmingRefiner warmOnce
}

// warmOnce runs at most one warm-up at a time, and lets a later caller try
// again once it has finished. Not sync.Once: a load that failed because the
// model was still downloading has to be retried when it is not.
type warmOnce struct{ running atomic.Bool }

func (w *warmOnce) run(fn func()) {
	if !w.running.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer w.running.Store(false)
		fn()
	}()
}

// NewVoiceService wires a recognizer rooted at a directory holding bundles.
//
// Two recognizers, not one: the streaming model draws the caption while the
// user is talking and the non-streaming refiner re-reads the finished
// utterance, and it is the refiner's text that is sent. See voice.DefaultModel
// for why the accurate one cannot be the streaming one.
func NewVoiceService(root, modelID string) *VoiceService {
	if modelID == "" {
		modelID = voice.DefaultModel
	}
	refiner := &voice.Refiner{Root: root}
	return &VoiceService{
		backend:   &voice.SherpaBackend{Root: root, ModelID: modelID, Refiner: refiner},
		refiner:   refiner,
		installer: &voice.Installer{Root: root},
		modelID:   modelID,
		sessions:  map[string]voice.Session{},
		state:     "idle",
	}
}

// Close releases every open session and stops the child processes.
func (s *VoiceService) Close() {
	s.mu.Lock()
	sessions := s.sessions
	s.sessions = map[string]voice.Session{}
	s.mu.Unlock()
	for _, session := range sessions {
		_ = session.Close()
	}
	if backend, ok := s.backend.(*voice.SherpaBackend); ok {
		backend.Shutdown()
	}
	if s.refiner != nil {
		s.refiner.Shutdown()
	}
}

// selectModel points the caption recognizer at a different model and retires
// everything running on the old one.
func (s *VoiceService) selectModel(modelID string) {
	s.mu.Lock()
	if s.modelID == modelID {
		s.mu.Unlock()
		return
	}
	s.modelID = modelID
	sessions := s.sessions
	s.sessions = map[string]voice.Session{}
	s.mu.Unlock()

	for _, session := range sessions {
		_ = session.Close()
	}
	if backend, ok := s.backend.(*voice.SherpaBackend); ok {
		backend.ModelID = modelID
		// The child process holds the previous weights and cannot be told
		// about new ones, so it goes and the next session starts a new one.
		backend.Shutdown()
	}
}

// warmEngines starts both recognizers' processes in the background, detached
// from the request that triggered it.
//
// Both, not just the refiner. Loading the streaming model is what a session
// blocks on, so leaving it until the microphone is activated puts the whole
// load between the user pressing the button and anything being recorded.
func (s *VoiceService) warmEngines() {
	// Its own context for each: the bridge request that triggered this is
	// answered in milliseconds and would cancel the load long before a
	// model finishes.
	if streaming, ok := s.backend.(*voice.SherpaBackend); ok {
		s.warmingStream.run(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := streaming.Warm(ctx); err != nil {
				log.Printf("voice: warm recognizer: %v", err)
			}
		})
	}
	if s.refiner == nil {
		return
	}
	refiner := s.refiner
	s.warmingRefiner.run(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := refiner.Warm(ctx); err != nil {
			log.Printf("voice: warm refiner: %v", err)
		}
	})
}

// ready reports whether a turn could start right now.
//
// Both recognizers have to be present. With only the streaming one the
// feature technically runs, and produces exactly the mis-heard transcripts
// that made this work necessary — so that state is reported as not ready
// rather than as a quietly worse version of ready.
func (s *VoiceService) ready(ctx context.Context) bool {
	if s.backend.Ready(ctx) != nil {
		return false
	}
	return s.refiner == nil || s.refiner.Ready(ctx) == nil
}

// bundles lists everything this service needs on disk: the engine, the
// streaming model the user chose, and the refiner.
func (s *VoiceService) bundles() []voice.Bundle {
	out := []voice.Bundle{voice.Runtime()}
	if model, err := voice.LookupBundle(s.modelID); err == nil {
		out = append(out, model)
	}
	if refiner, err := voice.LookupBundle(voice.DefaultRefiner); err == nil && refiner.ID != s.modelID {
		out = append(out, refiner)
	}
	return out
}

// SetVoiceService wires local speech recognition.
func (e *Engine) SetVoiceService(svc *VoiceService) { e.voice = svc }

func handleVoiceStatus(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.voice == nil {
		return bridge.Success(r.ID, map[string]any{
			"supported": false, "ready": false, "modelId": "", "modelTitle": "",
			"downloadBytes": 0, "backend": "",
		})
	}
	model, err := voice.LookupBundle(e.voice.modelID)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "VOICE-001", "本地识别模型未知", false)
	}
	// Deliberately does not warm the engines.
	//
	// It did briefly, to get the model loading before the microphone was
	// activated. But every screen that mentions voice asks for status,
	// including the companion stage on mount, and having the models on disk
	// does not mean they are the recognizer in use — so someone on the cloud
	// recognizer had two child processes and several hundred megabytes
	// started behind them, and a firewall prompt with them, every time they
	// opened voice mode. Status is a question, not an instruction.
	ready := e.voice.ready(ctx)
	var downloadBytes int64
	for _, bundle := range e.voice.bundles() {
		downloadBytes += bundle.TotalBytes()
	}
	// The title names the recognizer whose text the user reads, which is the
	// refiner. Naming the streaming model here would tell someone reading
	// the settings screen that they installed the 24 MB one and leave them
	// wondering where the other 232 MB went.
	title := model.Title
	if refiner, err := voice.LookupBundle(voice.DefaultRefiner); err == nil {
		title = refiner.Title
	}
	models := make([]map[string]any, 0, len(voice.StreamingModels()))
	for _, bundle := range voice.StreamingModels() {
		models = append(models, map[string]any{
			"id":    bundle.ID,
			"title": bundle.Title,
			// What this choice costs on disk, which is the only reason to
			// prefer one over the other once both work.
			"sizeBytes": bundle.TotalBytes(),
			"installed": e.voice.installer.Installed(bundle),
		})
	}
	return bridge.Success(r.ID, map[string]any{
		"supported":  true,
		"ready":      ready,
		"modelId":    model.ID,
		"modelTitle": title,
		"models":     models,
		// What a first-time user is about to be asked to download. Reported
		// even once installed, because the settings screen shows it.
		"downloadBytes": downloadBytes,
		"backend":       e.voice.backend.Name(),
	})
}

// handleVoiceInstall starts a download if none is running and reports where
// the current one has got to.
//
// It returns immediately either way. A 226 MB transfer cannot be held open
// inside one bridge request — the deadline would expire long before the
// bytes did — so the renderer starts it once and then asks again for
// progress, which is also exactly what a progress bar needs.
func handleVoiceInstall(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.voice == nil {
		return bridge.Failure(r.ID, r.TraceID, "VOICE-002", "本地识别不可用", true)
	}
	var p struct {
		ModelID string `json:"modelId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "voice.install 参数无效", false)
	}
	modelID := p.ModelID
	if modelID == "" {
		modelID = e.voice.modelID
	}
	if _, err := voice.LookupBundle(modelID); err != nil {
		return bridge.Failure(r.ID, r.TraceID, "VOICE-001", "本地识别模型未知", false)
	}

	e.voice.begin(modelID)
	return bridge.Success(r.ID, e.voice.snapshot())
}

// begin launches the download unless one is already running or everything is
// already present.
func (s *VoiceService) begin(modelID string) {
	s.mu.Lock()
	if s.installing {
		s.mu.Unlock()
		return
	}
	bundles := []voice.Bundle{voice.Runtime()}
	if model, err := voice.LookupBundle(modelID); err == nil {
		bundles = append(bundles, model)
	}
	if refiner, err := voice.LookupBundle(voice.DefaultRefiner); err == nil && refiner.ID != modelID {
		bundles = append(bundles, refiner)
	}
	outstanding := false
	for _, bundle := range bundles {
		if !s.installer.Installed(bundle) {
			outstanding = true
		}
	}
	if !outstanding {
		s.state, s.lastErr = "ready", ""
		s.mu.Unlock()
		return
	}
	s.installing, s.state, s.lastErr = true, "downloading", ""
	s.mu.Unlock()

	// Detached from the request that started it: the transfer outlives any
	// one bridge call, and cancelling it is voice.stop's job, not a
	// deadline's.
	go s.run(bundles)
}

func (s *VoiceService) run(bundles []voice.Bundle) {
	defer func() {
		s.mu.Lock()
		s.installing = false
		s.mu.Unlock()
	}()
	for _, bundle := range bundles {
		err := s.installer.Install(context.Background(), bundle, func(p voice.Progress) {
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

func (s *VoiceService) snapshot() map[string]any {
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

func handleVoiceStart(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.voice == nil {
		return bridge.Failure(r.ID, r.TraceID, "VOICE-002", "本地识别不可用", true)
	}
	var p struct {
		Language   string `json:"language"`
		Backend    string `json:"backend"`
		ProviderID string `json:"providerId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "voice.start 参数无效", false)
	}
	if p.Backend == "volc" {
		return startVolcVoice(e, ctx, r, p.Language, p.ProviderID)
	}

	session, err := e.voice.backend.Start(ctx, voice.SessionOptions{Language: p.Language})
	if err != nil {
		if errors.Is(err, voice.ErrModelMissing) {
			return bridge.Failure(r.ID, r.TraceID, "VOICE-003", "本地识别模型尚未下载", true)
		}
		return bridge.Failure(r.ID, r.TraceID, "VOICE-004", "本地识别引擎启动失败："+truncate(err.Error(), 256), true)
	}

	// Load the refiner's model while the user is still talking. It is not
	// waited on: a session that opens must open now, and a refiner that is
	// not ready by the time they stop simply does not refine that turn.
	e.voice.warmEngines()

	id := fmt.Sprintf("v%d", e.voice.counter.Add(1))
	e.voice.mu.Lock()
	e.voice.sessions[id] = session
	e.voice.mu.Unlock()
	return bridge.Success(r.ID, map[string]any{"sessionId": id})
}

func startVolcVoice(e *Engine, ctx context.Context, r bridge.Request, language, providerID string) bridge.Response {
	if providerID == "" || e.providers == nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "voice.start 参数无效", false)
	}
	p, err := e.providers.Get(ctx, providerID)
	if err != nil {
		return providerFailure(r, err)
	}
	if failure := providerReadyFailure(r, p); failure != nil {
		return *failure
	}
	if p.Protocol != provider.ProtocolVolcSpeech {
		return bridge.Failure(r.ID, r.TraceID, "VOICE-004", "所选供应商不是火山语音", false)
	}
	modelID := volcListenModelID(p)
	if modelID == "" {
		return bridge.Failure(r.ID, r.TraceID, "VOICE-004", "没有可用的火山听写模型", false)
	}
	var session voice.Session
	err = e.withProviderLease(ctx, p, secretlease.OperationProviderTest, func(opCtx context.Context, secret []byte) error {
		backend := volcsauc.New(volcsauc.ConfigFromSecret(p.BaseURL, modelID, string(secret)))
		opened, startErr := backend.Start(opCtx, voice.SessionOptions{Language: language})
		if startErr != nil {
			return startErr
		}
		session = opened
		return nil
	})
	if err != nil {
		msg := volcsauc.SanitizeProbeError(err)
		if msg == "" {
			msg = "火山语音启动失败"
		}
		return bridge.Failure(r.ID, r.TraceID, "VOICE-004", msg, true)
	}
	id := fmt.Sprintf("v%d", e.voice.counter.Add(1))
	e.voice.mu.Lock()
	e.voice.sessions[id] = session
	e.voice.mu.Unlock()
	return bridge.Success(r.ID, map[string]any{"sessionId": id})
}

func handleVoiceAppend(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
		PCM       string `json:"pcm"`
	}
	if decodePayload(r.Payload, &p) != nil || p.SessionID == "" || p.PCM == "" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "voice.append 参数无效", false)
	}
	pcm, err := base64.StdEncoding.DecodeString(p.PCM)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "voice.append 音频编码无效", false)
	}
	session, ok := e.voiceSession(p.SessionID)
	if !ok {
		return bridge.Failure(r.ID, r.TraceID, "VOICE-005", "识别会话不存在", false)
	}
	if err := session.Append(ctx, pcm); err != nil {
		return bridge.Failure(r.ID, r.TraceID, "VOICE-006", "音频写入失败："+truncate(err.Error(), 256), true)
	}
	// Partials ride back on the reply rather than through a second channel.
	text, final := "", false
	if reader, ok := session.(interface{ Latest() (string, bool) }); ok {
		text, final = reader.Latest()
	}
	return bridge.Success(r.ID, map[string]any{"text": text, "final": final})
}

func handleVoiceFinish(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil || p.SessionID == "" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "voice.finish 参数无效", false)
	}
	session, ok := e.voiceSession(p.SessionID)
	if !ok {
		return bridge.Failure(r.ID, r.TraceID, "VOICE-005", "识别会话不存在", false)
	}
	text, err := session.Finish(ctx)
	e.dropVoiceSession(p.SessionID)

	// The text is returned even alongside a failure. Half a sentence the
	// user actually said is worth more to them than an empty box, and the
	// renderer decides whether it is enough to send.
	if err != nil && text == "" {
		return bridge.Failure(r.ID, r.TraceID, "VOICE-007", "识别失败："+truncate(err.Error(), 256), true)
	}
	return bridge.Success(r.ID, map[string]any{"text": text})
}

// handleVoiceSelect switches which model draws the caption.
//
// The running recognizer is stopped rather than reconfigured: a sherpa server
// is started with its model on the command line and holds those weights for
// its lifetime, so a new model means a new process. Sessions in flight go
// with it — the alternative is a turn whose first half was transcribed by one
// model and whose second half was transcribed by another.
func handleVoiceSelect(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ModelID string `json:"modelId"`
	}
	if decodePayload(r.Payload, &p) != nil || p.ModelID == "" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "voice.select 参数无效", false)
	}
	if e.voice == nil {
		return bridge.Failure(r.ID, r.TraceID, "VOICE-002", "本地识别不可用", false)
	}
	if !voice.IsStreamingModel(p.ModelID) {
		return bridge.Failure(r.ID, r.TraceID, "VOICE-001", "本地识别模型未知", false)
	}
	e.voice.selectModel(p.ModelID)
	return bridge.Success(r.ID, map[string]any{
		"modelId": p.ModelID,
		"ready":   e.voice.ready(ctx),
	})
}

func handleVoiceStop(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "voice.stop 参数无效", false)
	}
	if p.SessionID != "" {
		e.dropVoiceSession(p.SessionID)
	} else if e.voice != nil {
		// No id means "stop everything", which is what a window closing or
		// a mode switch wants.
		e.voice.Close()
	}
	return bridge.Success(r.ID, map[string]any{"notice": "VOICE_SESSION_CLOSED"})
}

func (e *Engine) voiceSession(id string) (voice.Session, bool) {
	if e.voice == nil {
		return nil, false
	}
	e.voice.mu.Lock()
	defer e.voice.mu.Unlock()
	session, ok := e.voice.sessions[id]
	return session, ok
}

func (e *Engine) dropVoiceSession(id string) {
	if e.voice == nil {
		return
	}
	e.voice.mu.Lock()
	session, ok := e.voice.sessions[id]
	delete(e.voice.sessions, id)
	e.voice.mu.Unlock()
	if ok {
		_ = session.Close()
	}
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}
