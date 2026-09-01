package stdioworker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// Runtime policy (frozen for 5B): the config digest binds launch specs and
// 5A/5B/5C evidence to one build/config identity.
type Policy struct {
	FrameCapBytes  int    `json:"frameCapBytes"`
	ProtocolVer    string `json:"protocolVer"`
	MaxQuotaProcs  uint32 `json:"maxQuotaProcs"`
	MaxQuotaMem    uint64 `json:"maxQuotaMem"`
	MaxDeadlineMS  int64  `json:"maxDeadlineMS"`
	MinHeartbeatMS int64  `json:"minHeartbeatMS"`
}

// DefaultPolicy is the production contract; changes need a 5C re-run.
func DefaultPolicy() Policy {
	return Policy{
		FrameCapBytes:  MaxFrameBytes,
		ProtocolVer:    "stdio-worker/5b-v1",
		MaxQuotaProcs:  64,
		MaxQuotaMem:    4 << 30,
		MaxDeadlineMS:  30 * 60 * 1000,
		MinHeartbeatMS: 250,
	}
}

// Digest is the canonical sha256 of the policy JSON.
func (p Policy) Digest() (string, error) {
	raw, err := CanonicalJSON(p)
	if err != nil {
		return "", err
	}
	return digestBytes(raw), nil
}

// AuditSink receives stdio.* audit events (action names of migration 0050).
type AuditSink interface {
	Emit(action, aggregateID, actor string, metadata []byte) error
}

// Audit actions (migration 0050_m6_audit_stdio_worker.sql).
const (
	AuditLaunched  = "stdio.worker.launched"
	AuditCompleted = "stdio.worker.completed"
	AuditRevoked   = "stdio.worker.revoked"
	AuditExpired   = "stdio.worker.expired"
	AuditRecovered = "stdio.worker.recovered"
)

// Gate is the production enablement switch. Default is CLOSED: only a 5C
// production-escape acceptance PASS plus a recorded Security Owner sign-off
// may open it, and that record does not exist in this repository.
type Gate struct {
	// SignedOffBy names the Security Owner after 5C; empty = closed.
	SignedOffBy string
	// EvidenceDigest is the 5A/5B/5C bound digest that justified opening.
	EvidenceDigest string
}

// Open reports whether production stdio launches are allowed.
func (g Gate) Open() bool { return g.SignedOffBy != "" }

// Run is one live (or finished) worker run.
type Run struct {
	ID         string
	Spec       LaunchSpec
	SpecDigest string
	SessionID  string

	mu       sync.Mutex
	state    string
	result   []byte
	proc     *engineProc
	lastBeat time.Time
	deadline time.Time
	waitCh   chan struct{}
	waitCode int
	waitErr  error
}

// State returns the current run state.
func (r *Run) State() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

// Result returns the final payload (nil until COMPLETED).
func (r *Run) Result() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.result
}

// Wait blocks until the run reaches any terminal state.
func (r *Run) Wait(ctx context.Context) error {
	select {
	case <-r.waitCh:
		return r.waitErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Manager owns worker runs, the journal, the gate and the audit sink.
type Manager struct {
	gate    Gate
	keys    KeyStore
	journal *Journal
	audit   AuditSink
	policy  Policy
	now     func() time.Time

	mu   sync.Mutex
	runs map[string]*Run
	seen map[string]string // endpointID+nonce → specID (replay guard)
}

// NewManager builds the runtime. journalDir receives recovery-journal.jsonl.
func NewManager(gate Gate, keys KeyStore, journalDir string, audit AuditSink) (*Manager, error) {
	j, err := OpenJournal(filepath.Join(journalDir, "recovery-journal.jsonl"))
	if err != nil {
		return nil, err
	}
	return &Manager{
		gate:    gate,
		keys:    keys,
		journal: j,
		audit:   audit,
		policy:  DefaultPolicy(),
		now:     time.Now,
		runs:    map[string]*Run{},
		seen:    map[string]string{},
	}, nil
}

// SetClock substitutes the wall clock (tests).
func (m *Manager) SetClock(now func() time.Time) { m.now = now }

// PolicyDigest exposes the frozen policy digest for spec binding.
func (m *Manager) PolicyDigest() (string, error) { return m.policy.Digest() }

// Launch verifies and starts one worker run per the signed spec. The gate
// must be open (production) — tests use TestManager to bypass it.
func (m *Manager) Launch(ctx context.Context, sp *SignedSpec) (*Run, error) {
	if !m.gate.Open() {
		return nil, ErrGateClosed
	}
	return m.launch(ctx, sp)
}

func (m *Manager) launch(ctx context.Context, sp *SignedSpec) (*Run, error) {
	if err := sp.Verify(m.keys, m.now(), m.nonceSeen); err != nil {
		return nil, err
	}
	spec := sp.Spec
	if spec.ConfigDigest != "" {
		want, err := m.PolicyDigest()
		if err != nil {
			return nil, err
		}
		if spec.ConfigDigest != want {
			return nil, fmt.Errorf("%w: config digest drift (spec %s runtime %s)", ErrSpecSignature, spec.ConfigDigest, want)
		}
	}
	if err := policyAllows(m.policy, spec.Quotas); err != nil {
		return nil, err
	}
	if err := VerifyExecutable(spec.Command, spec.ExeDigest); err != nil {
		return nil, err
	}

	digest, err := spec.Digest()
	if err != nil {
		return nil, err
	}
	runID := ulid.Make().String()
	sessionID := NewNonce()

	if err := os.MkdirAll(spec.WorkingDir, 0o755); err != nil {
		return nil, fmt.Errorf("stdioworker: working dir: %w", err)
	}

	env := []string{
		"STDIOWORKER_SESSION=" + sessionID,
		"STDIOWORKER_SPEC_DIGEST=" + digest,
	}
	if root := os.Getenv("SystemRoot"); root != "" && runtime.GOOS == "windows" {
		env = append(env, "SystemRoot="+root)
	}

	p, err := engineSpawn(spec.Command, spec.Args, spec.WorkingDir, env, spec.Quotas)
	if err != nil {
		return nil, err
	}

	r := &Run{
		ID:         runID,
		Spec:       spec,
		SpecDigest: digest,
		SessionID:  sessionID,
		state:      StateLaunched,
		proc:       p,
		deadline:   m.now().Add(time.Duration(spec.Quotas.DeadlineMS) * time.Millisecond),
		lastBeat:   m.now(),
		waitCh:     make(chan struct{}),
	}
	m.mu.Lock()
	m.runs[runID] = r
	m.seen[spec.EndpointID+"|"+spec.Nonce] = spec.SpecID
	m.mu.Unlock()

	if err := m.journal.Append(JournalRecord{
		RunID: runID, SpecID: spec.SpecID, Endpoint: spec.EndpointID,
		SpecDigest: digest, Pid: p.pid, State: StateLaunched, AtMS: m.now().UnixMilli(),
	}); err != nil {
		p.kill()
		p.close()
		// durability lost: the run never happened - drop it from the live
		// set and release the nonce so nothing can reference a ghost run
		m.mu.Lock()
		delete(m.runs, runID)
		delete(m.seen, spec.EndpointID+"|"+spec.Nonce)
		m.mu.Unlock()
		return nil, fmt.Errorf("stdioworker: journal launch: %w", err)
	}
	m.emitAudit(AuditLaunched, runID, map[string]any{
		"endpointId": spec.EndpointID, "specDigest": digest, "pid": p.pid,
		"quotas": spec.Quotas, "configDigest": spec.ConfigDigest,
	})

	go m.monitor(r)
	return r, nil
}

// nonceSeen is the replay guard wired into spec verification.
func (m *Manager) nonceSeen(endpointID, nonce string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.seen[endpointID+"|"+nonce]
	return ok
}

// monitor reads the child stream, enforces the session contract and the
// watchdogs (heartbeat, deadline), and settles the run.
func (m *Manager) monitor(r *Run) {
	defer r.proc.close()
	beatInterval := time.Duration(r.Spec.Quotas.HeartbeatMS) * time.Millisecond
	maxLapse := beatInterval * time.Duration(r.Spec.Quotas.MaxMissedBeats+1)
	validator := NewSessionValidator(r.SessionID)

	type envelope struct {
		env *Envelope
		err error
	}
	stream := make(chan envelope, 8)
	go func() {
		defer close(stream)
		for {
			env, err := ReadEnvelope(r.proc.stdout)
			stream <- envelope{env: env, err: err}
			if err != nil {
				return
			}
		}
	}()

	killTimer := time.NewTimer(time.Until(r.deadline))
	defer killTimer.Stop()
	beatTimer := time.NewTimer(maxLapse)
	defer beatTimer.Stop()

	finish := func(state string, code int, waitErr error, result []byte, detail string) {
		r.mu.Lock()
		if terminal(r.state) {
			r.mu.Unlock()
			return
		}
		r.state = state
		r.result = result
		r.waitCode = code
		r.waitErr = waitErr
		r.mu.Unlock()
		_ = r.proc.kill()
		// The journal is best-effort progress telemetry; emitAudit below is the
		// authoritative record, so a failed append must not derail finalization.
		_ = m.journal.Append(JournalRecord{
			RunID: r.ID, SpecID: r.Spec.SpecID, Endpoint: r.Spec.EndpointID,
			SpecDigest: r.SpecDigest, Pid: r.proc.pid, State: state,
			Detail: detail, AtMS: m.now().UnixMilli(),
		})
		switch state {
		case StateCompleted:
			m.emitAudit(AuditCompleted, r.ID, map[string]any{"resultBytes": len(result)})
		case StateExpired:
			m.emitAudit(AuditExpired, r.ID, map[string]any{"reason": detail})
		case StateRevoked:
			m.emitAudit(AuditRevoked, r.ID, map[string]any{"reason": detail})
		}
		close(r.waitCh)
	}

	for {
		select {
		case msg, ok := <-stream:
			if !ok {
				// stream ended (child died or closed stdout)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				code, werr := r.proc.wait(ctx)
				cancel()
				r.mu.Lock()
				st := r.state
				r.mu.Unlock()
				if terminal(st) {
					return
				}
				finish(StateExpired, code, werr, nil, fmt.Sprintf("stream closed early: exit=%d err=%v", code, werr))
				return
			}
			if msg.err != nil {
				finish(StateExpired, -1, msg.err, nil, "protocol violation: "+msg.err.Error())
				return
			}
			if err := validator.Validate(msg.env); err != nil {
				finish(StateExpired, -1, err, nil, "protocol violation: "+err.Error())
				return
			}
			r.mu.Lock()
			r.lastBeat = m.now()
			r.mu.Unlock()
			switch msg.env.Type {
			case EnvHello:
				var hello struct {
					SpecDigest string `json:"specDigest"`
					Protocol   string `json:"protocol"`
				}
				_ = json.Unmarshal(msg.env.Data, &hello)
				if hello.SpecDigest != r.SpecDigest {
					finish(StateExpired, -1, nil, nil, "hello spec digest mismatch")
					return
				}
				if hello.Protocol != m.policy.ProtocolVer {
					finish(StateExpired, -1, nil, nil, "protocol version mismatch: "+hello.Protocol)
					return
				}
			case EnvResult:
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				code, werr := r.proc.wait(ctx)
				cancel()
				finish(StateCompleted, code, werr, msg.env.Data, "")
				return
			}
			if !beatTimer.Stop() {
				select {
				case <-beatTimer.C:
				default:
				}
			}
			beatTimer.Reset(maxLapse)
		case <-beatTimer.C:
			finish(StateExpired, -1, nil, nil, "heartbeat lost")
			return
		case <-killTimer.C:
			finish(StateExpired, -1, nil, nil, "deadline exceeded")
			return
		}
	}
}

// Revoke terminates the run and freezes any late result. The state is
// claimed REVOKED before the kill so the monitor's stream-closed path
// cannot race an EXPIRED verdict onto a revoked run (M6-SBX-004).
func (m *Manager) Revoke(runID, reason string) error {
	m.mu.Lock()
	r := m.runs[runID]
	m.mu.Unlock()
	if r == nil {
		return fmt.Errorf("stdioworker: run %s not found", runID)
	}
	r.mu.Lock()
	if terminal(r.state) {
		r.mu.Unlock()
		return nil
	}
	r.state = StateRevoked
	r.waitErr = ErrRevoked
	r.mu.Unlock()
	_ = r.proc.kill()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	code, _ := r.proc.wait(ctx)
	cancel()
	close(r.waitCh)
	// Best-effort telemetry; emitAudit below is the authoritative record.
	_ = m.journal.Append(JournalRecord{
		RunID: r.ID, SpecID: r.Spec.SpecID, Endpoint: r.Spec.EndpointID,
		SpecDigest: r.SpecDigest, Pid: r.proc.pid, State: StateRevoked,
		Detail: reason, AtMS: m.now().UnixMilli(),
	})
	m.emitAudit(AuditRevoked, r.ID, map[string]any{"reason": reason, "exit": code})
	return nil
}

// Recovered is one crash-recovery verdict handed to the caller.
type Recovered struct {
	Record JournalRecord
}

// Recover scans the journal for runs the previous host process left behind
// (their Job Objects died with it), marks them CRASHED and reports them so
// the task layer can requeue via normal lease semantics (TSK-002). It runs
// once at host startup before any Launch.
func (m *Manager) Recover() ([]Recovered, error) {
	path := filepath.Join(m.journal.path)
	recs, err := UnrecoveredRuns(path)
	if err != nil {
		return nil, err
	}
	out := make([]Recovered, 0, len(recs))
	for _, rec := range recs {
		detail := "host crash: job object died with host process"
		if err := m.journal.MarkCrashed(rec, m.now().UnixMilli(), detail); err != nil {
			return out, err
		}
		m.emitAudit(AuditRecovered, rec.RunID, map[string]any{
			"specId": rec.SpecID, "endpointId": rec.Endpoint, "pid": rec.Pid,
		})
		out = append(out, Recovered{Record: rec})
	}
	return out, nil
}

// Close releases the journal.
func (m *Manager) Close() error { return m.journal.Close() }

func (m *Manager) emitAudit(action, aggregateID string, meta map[string]any) {
	if m.audit == nil {
		return
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		raw = []byte(`{}`)
	}
	_ = m.audit.Emit(action, aggregateID, "stdio-runtime", raw)
}

// policyAllows rejects quota ceilings the frozen policy does not permit.
func policyAllows(p Policy, q Quotas) error {
	if q.MaxProcs > p.MaxQuotaProcs {
		return fmt.Errorf("%w: maxProcs %d above policy %d (M6-SBX-003)", ErrQuotaPolicy, q.MaxProcs, p.MaxQuotaProcs)
	}
	if q.MemoryCapBytes > p.MaxQuotaMem {
		return fmt.Errorf("%w: memoryCap above policy (M6-SBX-003)", ErrQuotaPolicy)
	}
	if q.DeadlineMS > p.MaxDeadlineMS {
		return fmt.Errorf("%w: deadline above policy (M6-SBX-003)", ErrQuotaPolicy)
	}
	if q.HeartbeatMS < p.MinHeartbeatMS {
		return fmt.Errorf("%w: heartbeat too aggressive (M6-SBX-003)", ErrQuotaPolicy)
	}
	return nil
}
