package voice

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Refiner re-recognizes a finished utterance with a non-streaming model.
//
// It is a second child process holding a second model, which is not free, and
// the reason it earns its place is in DefaultRefiner: the streaming model
// mis-hears ordinary words often enough to derail a conversation, and no
// streaming model can fix that, because the mistake is made before the
// evidence that would correct it has been spoken.
//
// The process is started on the first utterance and kept, like the streaming
// one. Starting it per turn would put four seconds of model loading in front
// of every reply.
type Refiner struct {
	// Root is the directory bundles were installed into.
	Root string
	// ModelID selects the non-streaming model. Empty uses DefaultRefiner.
	ModelID string
	// Startup bounds the wait for the server to accept connections.
	Startup time.Duration
	// Budget bounds one decode. Past it the caller keeps the streaming
	// text: a slow answer that is right is still worse than a fast answer
	// that is nearly right, because the user is sitting in silence for the
	// difference.
	Budget time.Duration

	mu     sync.Mutex
	server *sherpaServer
	// warming keeps a failed turn from starting a second model load behind
	// the one already running.
	warming atomic.Bool
}

func (r *Refiner) modelID() string {
	if r.ModelID == "" {
		return DefaultRefiner
	}
	return r.ModelID
}

func (r *Refiner) startup() time.Duration {
	if r.Startup <= 0 {
		return 60 * time.Second
	}
	return r.Startup
}

func (r *Refiner) budget() time.Duration {
	if r.Budget <= 0 {
		// A minute of speech decodes in about two seconds at the measured
		// rate, and a turn is normally a few seconds long. Five is not a
		// target; it is the point past which something is wrong.
		return 5 * time.Second
	}
	return r.Budget
}

// Ready reports whether the refiner can run without downloading anything.
func (r *Refiner) Ready(context.Context) error {
	installer := &Installer{Root: r.Root}
	if !installer.Installed(Runtime()) {
		return fmt.Errorf("%w: runtime", ErrModelMissing)
	}
	model, err := LookupBundle(r.modelID())
	if err != nil {
		return err
	}
	if !installer.Installed(model) {
		return fmt.Errorf("%w: %s", ErrModelMissing, model.ID)
	}
	return nil
}

// Transcribe decodes one whole utterance.
//
// The connection is per utterance rather than pooled. The server treats a
// connection as one utterance — it decodes when it has the number of bytes
// the preamble promised and then closes — so there is nothing to reuse, and a
// localhost TCP handshake is not a cost worth engineering around.
func (r *Refiner) Transcribe(ctx context.Context, pcm []byte) (string, error) {
	// A cold refiner is skipped, not waited for.
	//
	// Loading 232 MB of weights takes seconds, and the caller is a user who
	// has just stopped speaking and is waiting for an answer. Spending that
	// silence on a model load — on the very first turn, when the user is
	// forming their first impression of whether this thing works — buys a
	// slightly better transcript at a price nobody would agree to. So the
	// first turn keeps the streamed text and pays for the load in the
	// background, and every turn after it is refined.
	server, hot := r.hot()
	if !hot {
		r.warmInBackground()
		return "", fmt.Errorf("%w: refiner still loading its model", ErrBackendUnavailable)
	}

	ctx, cancel := context.WithTimeout(ctx, r.budget())
	defer cancel()

	text, err := transcribeOn(ctx, fmt.Sprintf("ws://127.0.0.1:%d", server.port), pcm)
	if err != nil {
		// The process's own output is the only thing that explains a server
		// that started and then refused to decode.
		return "", fmt.Errorf("%w%s", err, server.log.suffix())
	}
	return text, nil
}

// Warm starts the child process so the next utterance can be refined.
//
// Called when a listening session opens, which is the one moment when there
// is time to spare: the user has just started talking and will not want an
// answer for several seconds.
func (r *Refiner) Warm(ctx context.Context) error {
	if err := r.Ready(ctx); err != nil {
		return err
	}
	_, err := r.ensureServer(ctx)
	return err
}

// hot returns the running server, or reports that there is not one yet.
//
// TryLock rather than Lock: a warm in progress holds the mutex for as long as
// the model takes to load, and blocking here would reintroduce exactly the
// wait this exists to avoid.
func (r *Refiner) hot() (*sherpaServer, bool) {
	if !r.mu.TryLock() {
		return nil, false
	}
	defer r.mu.Unlock()
	if r.server != nil && r.server.alive() {
		return r.server, true
	}
	return nil, false
}

// warmInBackground loads the model without holding up the turn that noticed
// it was missing. At most one load runs at a time.
func (r *Refiner) warmInBackground() {
	if !r.warming.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer r.warming.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), r.startup())
		defer cancel()
		if err := r.Warm(ctx); err != nil {
			log.Printf("voice: warm refiner: %v", err)
		}
	}()
}

// transcribeOn is the protocol itself, against an address.
//
// Split from Transcribe so the wire format can be tested against a stand-in
// server: everything above this line is process management, and everything
// below is the part that has to agree byte for byte with sherpa.
func transcribeOn(ctx context.Context, endpoint string, pcm []byte) (string, error) {
	request, err := offlineRequest(pcm)
	if err != nil {
		return "", err
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("%w: connect to refiner: %v", ErrBackendUnavailable, err)
	}
	defer conn.Close()

	// The deadline is what stops a wedged server from holding the turn open
	// past the budget; the context alone does not interrupt a blocking read.
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetWriteDeadline(deadline)
		_ = conn.SetReadDeadline(deadline)
	}

	for _, chunk := range offlineChunks(request) {
		if err := conn.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
			return "", fmt.Errorf("%w: send utterance: %v", ErrBackendUnavailable, err)
		}
	}

	// One text frame carries the whole result: the server has all the audio
	// and decodes it in one pass, so unlike the streaming socket there are no
	// partials to skip past.
	kind, frame, err := conn.ReadMessage()
	if err != nil {
		return "", fmt.Errorf("%w: read result: %v", ErrBackendUnavailable, err)
	}
	if kind != websocket.TextMessage {
		return "", fmt.Errorf("%w: refiner sent a %d frame, expected text", ErrBackendUnavailable, kind)
	}
	text, err := parseOfflineResult(frame)
	if err != nil {
		return "", err
	}

	// Courtesy: it lets the server close the connection itself rather than
	// discovering a hangup. A failure here has no bearing on the transcript,
	// which is already in hand.
	_ = conn.WriteMessage(websocket.TextMessage, []byte(doneRequest))
	return strings.TrimSpace(text), nil
}

func (r *Refiner) ensureServer(ctx context.Context) (*sherpaServer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.server != nil && r.server.alive() {
		return r.server, nil
	}
	r.server = nil

	installer := &Installer{Root: r.Root}
	model, err := LookupBundle(r.modelID())
	if err != nil {
		return nil, err
	}
	arch, err := Architecture(model.ID)
	if err != nil {
		return nil, err
	}
	if arch.Streaming() {
		return nil, fmt.Errorf("voice: %s is a streaming model and cannot refine", model.ID)
	}

	port, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBackendUnavailable, err)
	}
	args, err := offlineServerArgs(arch, installer.BundleDir(model.ID), port)
	if err != nil {
		return nil, err
	}

	server, err := spawnServer(offlineServerExecutable(installer.BundleDir(Runtime().ID)), args, port)
	if err != nil {
		return nil, err
	}
	if err := server.waitUntilListening(ctx, r.startup()); err != nil {
		server.stop()
		return nil, err
	}
	r.server = server
	return server, nil
}

// Shutdown stops the child process.
func (r *Refiner) Shutdown() {
	r.mu.Lock()
	server := r.server
	r.server = nil
	r.mu.Unlock()
	if server != nil {
		server.stop()
	}
}
