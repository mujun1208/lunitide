// M4-F command use cases: command.start/get/cancel.
//
// A command job only ever executes a product-signed CommandSpec template:
// the caller names a compiled-in template (id + version) plus typed
// parameters, the engine resolves it to a shell-free argv inside a granted
// workspace, and the job row binds the resolved spec by digest. No shell
// command line is ever accepted, stored or executed (PRD M4: 任意 shell
// 不存在).
//
// Lifecycle: command.start creates the job queued→running in one
// transaction (idempotency record + run event + audit commit together) and
// only launches the worker after commit. The worker runs the process tree
// under a Job Object / process group and the completion path writes the
// terminal state plus the JobOutputChunk event in a second transaction.
// command.cancel is first-terminal-wins: whichever transaction commits
// first decides the outcome, and the loser observes a terminal row. After
// an engine crash, committed running jobs are unprovable and reconcile to
// outcome_unknown — never blindly retried (PRD M4 信任/失败/恢复).
package agentrunapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/commandworker"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

var (
	// ErrCommandTemplateUnknown is returned when the requested template id
	// or version is not in the signed registry.
	ErrCommandTemplateUnknown = errors.New("command template not found or version mismatch")
	// ErrCommandSpecMismatch is returned when the caller-pinned spec digest
	// does not match the spec the engine resolved. The request fails closed
	// with no side effects (PRD M4: 摘要变化则无副作用).
	ErrCommandSpecMismatch = errors.New("command spec digest mismatch")
	// ErrCommandTargetInvalid is returned when the typed target parameter
	// fails whitelist validation.
	ErrCommandTargetInvalid = errors.New("command target invalid")
	// ErrCommandNotRunnable is returned when the template executable cannot
	// be resolved on this host.
	ErrCommandNotRunnable = errors.New("command executable not resolvable")
)

// commandSystemActor is the audit actor for asynchronous job completion.
const commandSystemActor = "system"

// CommandTemplate is one product-signed command specification. The registry
// is compiled into the engine binary; templates outside it cannot run.
type CommandTemplate struct {
	ID        string        // registry identity, e.g. "go.test"
	Version   string        // template version, bump on any field change
	Tool      string        // executable name resolved via PATH at start
	FixedArgs []string      // immutable argv prefix; the validated target is appended
	Target    bool          // whether a typed package-pattern parameter is accepted
	Timeout   time.Duration // hard wall-clock cap for the job
	MaxOutput int           // combined stdout+stderr cap in bytes
	EnvRefs   []string      // allowlisted host env vars copied into the child
}

// commandGoEnvRefs is the minimal environment a Go toolchain needs. The
// allowlist is the output-redaction boundary: nothing else (notably no
// credentials, which never live in the process environment) reaches the
// child.
var commandGoEnvRefs = []string{
	"PATH", "SystemRoot", "HOME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH",
	"GOPATH", "GOCACHE", "GOPROXY", "GOFLAGS", "GOMODCACHE", "TEMP", "TMP",
}

// commandRegistry is the authoritative signed template set (PRD M4.6:
// lint/test/build 模板命令). Digest-advertised via TemplateDigest.
var commandRegistry = []CommandTemplate{
	{ID: "go.vet", Version: "1.0.0", Tool: "go", FixedArgs: []string{"vet"}, Target: true, Timeout: 5 * time.Minute, MaxOutput: 256 << 10, EnvRefs: commandGoEnvRefs},
	{ID: "go.build", Version: "1.0.0", Tool: "go", FixedArgs: []string{"build"}, Target: true, Timeout: 5 * time.Minute, MaxOutput: 256 << 10, EnvRefs: commandGoEnvRefs},
	{ID: "go.test", Version: "1.0.0", Tool: "go", FixedArgs: []string{"test", "-count=1"}, Target: true, Timeout: 10 * time.Minute, MaxOutput: 256 << 10, EnvRefs: commandGoEnvRefs},
}

// CommandTemplateDigest returns the SHA-256 of the template's canonical
// JSON encoding. Struct field order is alphabetical, so json.Marshal is the
// canonical form.
func CommandTemplateDigest(t CommandTemplate) (string, error) {
	type preimage struct {
		EnvRefs   []string `json:"envRefs"`
		FixedArgs []string `json:"fixedArgs"`
		ID        string   `json:"id"`
		MaxOutput int      `json:"maxOutput"`
		Target    bool     `json:"target"`
		TimeoutMs int64    `json:"timeoutMs"`
		Tool      string   `json:"tool"`
		Version   string   `json:"version"`
	}
	body, err := json.Marshal(preimage{
		EnvRefs:   t.EnvRefs,
		FixedArgs: t.FixedArgs,
		ID:        t.ID,
		MaxOutput: t.MaxOutput,
		Target:    t.Target,
		TimeoutMs: t.Timeout.Milliseconds(),
		Tool:      t.Tool,
		Version:   t.Version,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func lookupCommandTemplate(id, version string) (CommandTemplate, error) {
	for _, t := range commandRegistry {
		if t.ID == id && t.Version == version {
			return t, nil
		}
	}
	return CommandTemplate{}, ErrCommandTemplateUnknown
}

// commandTargetCharset whitelists Go package patterns ("./...",
// "./internal/...", "fmt"): no whitespace, no shell metacharacters.
var commandTargetCharset = regexp.MustCompile(`^[a-zA-Z0-9_./=-]+$`)

// validCommandTarget enforces the typed-parameter contract: charset
// whitelist, no flag smuggling, no parent-directory escape. "..." is the Go
// wildcard and is explicitly allowed; any remaining ".." is an escape.
func validCommandTarget(target string) bool {
	if len(target) < 1 || len(target) > 128 {
		return false
	}
	if !commandTargetCharset.MatchString(target) {
		return false
	}
	if strings.HasPrefix(target, "-") || strings.HasPrefix(target, "/") {
		return false
	}
	return !strings.Contains(strings.ReplaceAll(target, "...", ""), "..")
}

// resolveCommandEnv copies the allowlisted host variables into a sorted,
// complete child environment.
func resolveCommandEnv(refs []string) []string {
	pairs := make([]string, 0, len(refs))
	for _, name := range refs {
		if value, ok := os.LookupEnv(name); ok {
			pairs = append(pairs, name+"="+value)
		}
	}
	sort.Strings(pairs)
	return pairs
}

// commandSpecPreimage is the canonical digest preimage of a fully resolved
// spec. Field order is alphabetical (canonical JSON).
type commandSpecPreimage struct {
	Args            []string `json:"args"`
	Cwd             string   `json:"cwd"`
	Env             []string `json:"env"`
	Exe             string   `json:"exe"`
	MaxOutputBytes  int      `json:"maxOutputBytes"`
	TemplateID      string   `json:"templateId"`
	TemplateVersion string   `json:"templateVersion"`
	TimeoutNanos    int64    `json:"timeoutNanos"`
}

// commandSpecDigest binds a job to the exact resolved spec.
func commandSpecDigest(p commandSpecPreimage) (string, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// CommandStartInput carries the typed parameters of command.start.
type CommandStartInput struct {
	RunID              string
	LeaseID            string
	FencingToken       int64
	TemplateID         string
	TemplateVersion    string
	Target             string // package pattern; empty uses the template default "./..."
	WorkDir            string // workspace-relative working directory; "" = root
	ExpectedSpecDigest string // optional fail-closed pin on the resolved spec
	ApprovalDigest     string // durable one-shot review authorization
}

type CommandReviewRequestResult struct {
	CommandSpecDigest string `json:"commandSpecDigest"`
	ApprovalDigest    string `json:"approvalDigest"`
}

const commandPolicy = "m4-single-agent-command-policy-v1"
const commandDescriptor = "command-spec-1.0.0"

type commandApprovalPreimage struct {
	Action            string `json:"action"`
	CommandSpecDigest string `json:"commandSpecDigest"`
	ConfigDigest      string `json:"configDigest"`
	DescriptorDigest  string `json:"descriptorDigest"`
	PolicyDigest      string `json:"policyDigest"`
	RegistrationID    string `json:"registrationId"`
	RunID             string `json:"runId"`
}

func commandApprovalDigest(p commandApprovalPreimage) (string, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// commandCwdAllowed applies execute-scope patterns to a process working
// directory. Unlike filesystem tree listing, a cwd must itself be granted:
// the workspace root is allowed only by "**", and an ancestor of a granted
// subtree is not implicitly allowed. Thus "safe/**" allows "safe" and its
// descendants, but denies both the root and e.g. "project" when only
// "project/safe/**" is granted.
func commandCwdAllowed(patterns []string, rel string) bool {
	if rel == "" {
		for _, pattern := range patterns {
			if pattern == "**" {
				return true
			}
		}
		return false
	}
	return scopeAllows(patterns, rel)
}

func resolveCommandSpec(tx Tx, in CommandStartInput, now time.Time) (commandworker.Spec, CommandTemplate, fsAccess, string, error) {
	access, err := authorizeFsLease(tx, in.LeaseID, in.FencingToken, "execute", now)
	if err != nil {
		return commandworker.Spec{}, CommandTemplate{}, fsAccess{}, "", err
	}
	tmpl, err := lookupCommandTemplate(in.TemplateID, in.TemplateVersion)
	if err != nil {
		return commandworker.Spec{}, CommandTemplate{}, fsAccess{}, "", err
	}
	target := in.Target
	if target == "" {
		target = "./..."
	}
	if !tmpl.Target && in.Target != "" {
		return commandworker.Spec{}, CommandTemplate{}, fsAccess{}, "", fmt.Errorf("%w: template %s takes no target parameter", agentrun.ErrInvalid, tmpl.ID)
	}
	if !validCommandTarget(target) {
		return commandworker.Spec{}, CommandTemplate{}, fsAccess{}, "", ErrCommandTargetInvalid
	}
	absDir, err := access.resolve(in.WorkDir)
	if err != nil {
		return commandworker.Spec{}, CommandTemplate{}, fsAccess{}, "", err
	}
	if !commandCwdAllowed(access.patterns, in.WorkDir) {
		return commandworker.Spec{}, CommandTemplate{}, fsAccess{}, "", ErrFsScopeDenied
	}
	if info, statErr := os.Stat(absDir); statErr != nil || !info.IsDir() {
		return commandworker.Spec{}, CommandTemplate{}, fsAccess{}, "", ErrFsNotFound
	}
	exe, err := exec.LookPath(tmpl.Tool)
	if err != nil {
		return commandworker.Spec{}, CommandTemplate{}, fsAccess{}, "", fmt.Errorf("%w: %s", ErrCommandNotRunnable, tmpl.Tool)
	}
	if exe, err = filepath.Abs(exe); err != nil {
		return commandworker.Spec{}, CommandTemplate{}, fsAccess{}, "", fmt.Errorf("%w: %s", ErrCommandNotRunnable, tmpl.Tool)
	}
	spec := commandworker.Spec{Exe: exe, Args: append(append([]string(nil), tmpl.FixedArgs...), target), Dir: absDir, Env: resolveCommandEnv(tmpl.EnvRefs), Timeout: tmpl.Timeout, MaxOutputBytes: tmpl.MaxOutput}
	if err := spec.Validate(); err != nil {
		return commandworker.Spec{}, CommandTemplate{}, fsAccess{}, "", err
	}
	digest, err := commandSpecDigest(commandSpecPreimage{Args: spec.Args, Cwd: spec.Dir, Env: spec.Env, Exe: spec.Exe, MaxOutputBytes: spec.MaxOutputBytes, TemplateID: tmpl.ID, TemplateVersion: tmpl.Version, TimeoutNanos: int64(spec.Timeout)})
	return spec, tmpl, access, digest, err
}

// CommandReviewRequest persists a review request bound to the final resolved
// command spec before command.start is allowed to consume an approval.
func (s *Service) CommandReviewRequest(ctx context.Context, key, actor string, request any, in CommandStartInput) (CommandReviewRequestResult, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return CommandReviewRequestResult{}, ErrIdempotencyKeyRequired
	}
	if err := s.available(); err != nil {
		return CommandReviewRequestResult{}, err
	}
	digest, err := requestDigest(request)
	if err != nil {
		return CommandReviewRequestResult{}, err
	}
	var result CommandReviewRequestResult
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency("command.review.request", key, now)
		if err != nil {
			return err
		}
		if found {
			return replay(record, digest, &result)
		}
		run, err := tx.GetRun(in.RunID)
		if err != nil {
			return err
		}
		if run.Status != agentrun.RunRunning {
			return fmt.Errorf("%w: command review requires a running run, got %s", agentrun.ErrInvalidTransition, run.Status)
		}
		_, _, access, specDigest, err := resolveCommandSpec(tx, in, now)
		if err != nil {
			return err
		}
		approval, err := commandApprovalDigest(commandApprovalPreimage{Action: "command.start", CommandSpecDigest: specDigest, ConfigDigest: accessConfigDigest(access), DescriptorDigest: commandDescriptor, PolicyDigest: digestText(commandPolicy), RegistrationID: access.registrationID, RunID: run.ID})
		if err != nil {
			return err
		}
		if _, err = tx.TransitionRun(run.ID, run.Version, agentrun.RunPausedReview, now); err != nil {
			return err
		}
		if err := appendRunEvent(tx, run.ID, agentrun.EventReviewRequested, map[string]any{"approvalDigest": approval, "action": "command.start", "resourceDigest": specDigest, "baseDigest": specDigest, "configDigest": accessConfigDigest(access), "policyDigest": digestText(commandPolicy), "descriptorDigest": commandDescriptor}, now); err != nil {
			return err
		}
		result = CommandReviewRequestResult{CommandSpecDigest: specDigest, ApprovalDigest: approval}
		meta, _ := json.Marshal(map[string]any{"runId": run.ID, "commandSpecDigest": specDigest, "approvalDigest": approval})
		if err := s.putAudit(tx, "command.review.requested", run.ID, actor, digest, meta, now); err != nil {
			return err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return tx.PutIdempotency(providerapp.Record{Operation: "command.review.request", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}

// CommandRunner executes one resolved spec. It is a field so tests can
// substitute a fake without spawning processes.
type CommandRunner func(ctx context.Context, spec commandworker.Spec, guard commandworker.StartGuard, onOutput func([]byte)) (commandworker.Outcome, error)

// CommandStart creates a command job for a signed template and launches it
// after commit. The workspace write lease authorizes the working directory
// (template commands may write build/test artifacts). When the caller pins
// ExpectedSpecDigest, a resolution mismatch fails closed with no job.
func (s *Service) CommandStart(ctx context.Context, key, actor string, request any, in CommandStartInput) (agentrun.CommandJob, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return agentrun.CommandJob{}, ErrIdempotencyKeyRequired
	}
	if err := s.available(); err != nil {
		return agentrun.CommandJob{}, err
	}
	digest, err := requestDigest(request)
	if err != nil {
		return agentrun.CommandJob{}, err
	}
	var job agentrun.CommandJob
	var spec commandworker.Spec
	var startGuard commandworker.StartGuard
	created := false
	defer func() {
		if startGuard != nil {
			_ = startGuard.Close()
		}
	}()
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency("command.start", key, now)
		if err != nil {
			return err
		}
		if found {
			return replay(record, digest, &job)
		}
		run, err := tx.GetRun(in.RunID)
		if err != nil {
			return err
		}
		if run.Status != agentrun.RunRunning {
			return fmt.Errorf("%w: command.start requires a running run, got %s", agentrun.ErrInvalidTransition, run.Status)
		}
		var access fsAccess
		var tmpl CommandTemplate
		var specDigest string
		spec, tmpl, access, specDigest, err = resolveCommandSpec(tx, in, now)
		if err != nil {
			return err
		}
		if in.ExpectedSpecDigest == "" || in.ExpectedSpecDigest != specDigest {
			return ErrCommandSpecMismatch
		}
		expectedApproval, err := commandApprovalDigest(commandApprovalPreimage{Action: "command.start", CommandSpecDigest: specDigest, ConfigDigest: accessConfigDigest(access), DescriptorDigest: commandDescriptor, PolicyDigest: digestText(commandPolicy), RegistrationID: access.registrationID, RunID: run.ID})
		if err != nil {
			return err
		}
		// Pin after final spec resolution but before consuming approval.
		startGuard, err = commandworker.PinWorkingDirectory(access.root, spec.Dir)
		if err != nil {
			return err
		}
		review, err := tx.ConsumeReview(run.ID, in.ApprovalDigest, "command.start", now)
		if err != nil {
			return err
		}
		if in.ApprovalDigest != expectedApproval || review.ResourceDigest != specDigest || review.BaseDigest != specDigest || review.ConfigDigest != accessConfigDigest(access) || review.PolicyDigest != digestText(commandPolicy) || review.DescriptorDigest != commandDescriptor {
			return agentrun.ErrReviewDigestMismatch
		}
		jobID := ulid.Make().String()
		if err := tx.PutEffect(agentrun.EffectJournal{ID: ulid.Make().String(), RunID: run.ID, EffectKey: "command.start/" + jobID, RequestDigest: specDigest, Status: agentrun.EffectPrepared, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		job = agentrun.CommandJob{
			ID:                jobID,
			RunID:             run.ID,
			CommandSpecDigest: specDigest,
			Status:            agentrun.JobQueued,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		created = true
		if err := tx.PutCommandJob(job); err != nil {
			return err
		}
		job, err = job.Transition(agentrun.JobRunning, nil, now)
		if err != nil {
			return err
		}
		if err := tx.PutCommandJob(job); err != nil {
			return err
		}
		if err := appendRunEvent(tx, run.ID, agentrun.EventCommandStartCompleted, map[string]any{
			"schemaVersion":     1,
			"runId":             run.ID,
			"commandJobId":      job.ID,
			"commandSpecDigest": specDigest,
			"templateId":        tmpl.ID,
			"templateVersion":   tmpl.Version,
			"status":            string(job.Status),
		}, now); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"runId": run.ID, "commandSpecDigest": specDigest, "templateId": tmpl.ID})
		if err := s.putAudit(tx, "command.started", job.ID, actor, digest, meta, now); err != nil {
			return err
		}
		response, err := json.Marshal(job)
		if err != nil {
			return err
		}
		return tx.PutIdempotency(providerapp.Record{Operation: "command.start", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	if err != nil {
		return agentrun.CommandJob{}, err
	}
	// Launch strictly once and only after the creating transaction commits.
	// An idempotent replay returns the stored job without repeating the effect.
	if created {
		guard := startGuard
		startGuard = nil
		s.launchCommand(job.ID, spec, guard)
	}
	return job, nil
}

// launchCommand runs the spec in the background and records the terminal
// state. The job context is registered so command.cancel can kill the tree.
func (s *Service) launchCommand(jobID string, spec commandworker.Spec, guard commandworker.StartGuard) {
	ctx, cancel := context.WithCancel(context.Background())
	s.cmdMu.Lock()
	if s.cmdCancels == nil {
		s.cmdCancels = make(map[string]context.CancelFunc)
	}
	s.cmdCancels[jobID] = cancel
	s.cmdMu.Unlock()
	go func() {
		defer guard.Close()
		defer func() {
			cancel()
			s.cmdMu.Lock()
			delete(s.cmdCancels, jobID)
			s.cmdMu.Unlock()
		}()
		var output bytes.Buffer
		outcome, runErr := s.runCommand(ctx, spec, guard, func(chunk []byte) { output.Write(chunk) })
		s.finishCommand(jobID, outcome, runErr, output.Bytes())
	}()
}

// finishCommand writes the terminal job state and the JobOutputChunk event
// in one transaction. First-terminal-wins: a concurrently cancelled row is
// terminal and the completion becomes a no-op. A persistence failure leaves
// the job running, which boot-time reconcile resolves to outcome_unknown.
func (s *Service) finishCommand(jobID string, outcome commandworker.Outcome, runErr error, output []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		job, err := tx.GetCommandJob(jobID)
		if err != nil {
			if errors.Is(err, agentrun.ErrNotFound) {
				return nil // run cascade-deleted while the process ran
			}
			return err
		}
		if job.Status.Terminal() {
			return nil // cancel committed first
		}
		to := agentrun.JobCompleted
		exit := outcome.ExitCode
		switch {
		case runErr != nil || outcome.TimedOut:
			to = agentrun.JobFailed
			exit = -1
		case outcome.ExitCode != 0:
			to = agentrun.JobFailed
		}
		next, err := job.Transition(to, &exit, now)
		if err != nil {
			return nil // defensive: never resurrect a terminal row
		}
		if err := tx.PutCommandJob(next); err != nil {
			return err
		}
		effect, err := tx.GetEffectByKey("command.start/" + job.ID)
		if err != nil {
			return err
		}
		effectStatus := agentrun.EffectCommitted
		if to == agentrun.JobFailed {
			effectStatus = agentrun.EffectFailed
		}
		resolved, err := effect.Resolve(effectStatus, job.ID, now)
		if err != nil {
			return err
		}
		if err := tx.PutEffect(resolved); err != nil {
			return err
		}
		outputSum := sha256.Sum256(output)
		if err := appendRunEvent(tx, job.RunID, agentrun.EventCommandJobOutputChunk, map[string]any{
			"schemaVersion": 1,
			"runId":         job.RunID,
			"commandJobId":  job.ID,
			"chunk":         1,
			"status":        string(next.Status),
			"exitCode":      exit,
			"outputDigest":  hex.EncodeToString(outputSum[:]),
			"truncated":     outcome.Truncated,
			"contentBase64": base64.StdEncoding.EncodeToString(output),
		}, now); err != nil {
			return err
		}
		action := "command.completed"
		if to == agentrun.JobFailed {
			action = "command.failed"
		}
		meta, _ := json.Marshal(map[string]any{"runId": job.RunID, "exitCode": exit, "truncated": outcome.Truncated})
		return s.putAudit(tx, action, job.ID, commandSystemActor, job.CommandSpecDigest, meta, now)
	})
}

// CommandGet loads a command job by ID.
func (s *Service) CommandGet(ctx context.Context, jobID string) (agentrun.CommandJob, error) {
	if err := s.available(); err != nil {
		return agentrun.CommandJob{}, err
	}
	var job agentrun.CommandJob
	err := s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		var err error
		job, err = tx.GetCommandJob(jobID)
		return err
	})
	return job, err
}

// CommandCancel transitions a queued/running job to cancelled and kills the
// process tree. Terminal jobs fail with ErrTerminal (first-terminal-wins).
func (s *Service) CommandCancel(ctx context.Context, key, actor string, request any, jobID string) (agentrun.CommandJob, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return agentrun.CommandJob{}, ErrIdempotencyKeyRequired
	}
	if err := s.available(); err != nil {
		return agentrun.CommandJob{}, err
	}
	digest, err := requestDigest(request)
	if err != nil {
		return agentrun.CommandJob{}, err
	}
	var job agentrun.CommandJob
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency("command.cancel", key, now)
		if err != nil {
			return err
		}
		if found {
			return replay(record, digest, &job)
		}
		loaded, err := tx.GetCommandJob(jobID)
		if err != nil {
			return err
		}
		job, err = loaded.Transition(agentrun.JobCancelled, nil, now)
		if err != nil {
			return err
		}
		if err := tx.PutCommandJob(job); err != nil {
			return err
		}
		if err := appendRunEvent(tx, job.RunID, agentrun.EventCommandCancelCompleted, map[string]any{
			"schemaVersion": 1,
			"runId":         job.RunID,
			"commandJobId":  job.ID,
			"status":        string(job.Status),
		}, now); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"runId": job.RunID})
		if err := s.putAudit(tx, "command.cancelled", job.ID, actor, digest, meta, now); err != nil {
			return err
		}
		response, err := json.Marshal(job)
		if err != nil {
			return err
		}
		return tx.PutIdempotency(providerapp.Record{Operation: "command.cancel", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	if err != nil {
		return agentrun.CommandJob{}, err
	}
	// Kill strictly after commit: a cancelled row always precedes the signal,
	// so the completion path can never overwrite the terminal state.
	s.cmdMu.Lock()
	cancel, ok := s.cmdCancels[jobID]
	if ok {
		delete(s.cmdCancels, jobID)
	}
	s.cmdMu.Unlock()
	if ok {
		cancel()
	}
	return job, nil
}

// ReconcileCommandJobs resolves every committed non-terminal job to
// outcome_unknown. After a crash the external effect of such a job is
// unprovable (its process tree died with the engine or was orphaned), so it
// is never retried (PRD M4: 不明副作用进入 outcome_unknown, MUST NOT 盲重试).
func (s *Service) ReconcileCommandJobs(ctx context.Context) (int, error) {
	if err := s.available(); err != nil {
		return 0, err
	}
	reconciled := 0
	err := s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		jobs, err := tx.ListActiveCommandJobs()
		if err != nil {
			return err
		}
		for _, job := range jobs {
			next, err := job.Transition(agentrun.JobOutcomeUnknown, nil, now)
			if err != nil {
				return err
			}
			if err := tx.PutCommandJob(next); err != nil {
				return err
			}
			effect, err := tx.GetEffectByKey("command.start/" + job.ID)
			if err != nil {
				return err
			}
			if effect.Status != agentrun.EffectOutcomeUnknown {
				resolved, err := effect.Resolve(agentrun.EffectOutcomeUnknown, "", now)
				if err != nil {
					return err
				}
				if err := tx.PutEffect(resolved); err != nil {
					return err
				}
			}
			meta, _ := json.Marshal(map[string]any{"runId": job.RunID, "fromStatus": string(job.Status)})
			if err := s.putAudit(tx, "command.reconciled", job.ID, commandSystemActor, job.CommandSpecDigest, meta, now); err != nil {
				return err
			}
			reconciled++
		}
		return nil
	})
	return reconciled, err
}
