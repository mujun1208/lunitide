// T-5.3.4 command bridge: command.start / command.cancel / command.status.
//
// The service keeps an in-memory task registry keyed by TaskID and delegates
// process creation to an injected JobLauncher. It deliberately does NOT
// import internal/command: until that package lands, the composition root
// wires a launcher adapter around internal/command StartJob (spec loading
// via LoadManifest, ValidateStart and RenderArgv all live there). This file
// only needs the narrow jobInterface surface below to manage lifecycle,
// reconnectable log cursors (CMD semantics) and orphan detection.
//
// Wire error mapping: CMD-001 invalid start parameters (ErrCmdInvalidOp,
// ErrCmdSpecRequired), CMD-002 illegal task state transition
// (ErrTaskStateBad), TASK-001 unknown task (ErrTaskNotFound).
package m5

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/m5workspace"
	"github.com/lunitide/lunitide/internal/workspace"
	"github.com/oklog/ulid/v2"
)

var (
	// ErrCmdInvalidOp maps to CMD-001: op must be start, cancel or status.
	ErrCmdInvalidOp = errors.New("m5: op must be start, cancel or status")
	// ErrCmdSpecRequired maps to CMD-001: start requires a specId.
	ErrCmdSpecRequired = errors.New("m5: specId is required")
	// ErrTaskStateBad maps to CMD-002: the task state does not allow the transition.
	ErrTaskStateBad = errors.New("m5: task state does not allow this transition")
	// ErrTaskNotFound maps to TASK-001: no task registered under taskID.
	ErrTaskNotFound = errors.New("m5: task not found")
	// ErrLauncherUnavailable is a wiring bug: no launcher was injected.
	ErrLauncherUnavailable = errors.New("m5: command launcher unavailable")
)

// Managed task states.
const (
	TaskRunning      = "running"
	TaskBackgrounded = "backgrounded"
	TaskDone         = "done"
	TaskFailed       = "failed"
	TaskCancelled    = "cancelled"
	TaskOrphaned     = "orphaned"
)

func taskTerminal(state string) bool {
	switch state {
	case TaskDone, TaskFailed, TaskCancelled, TaskOrphaned:
		return true
	}
	return false
}

// jobInterface is the bridge-local view of internal/command Job. The
// composition adapter maps StartJob's Job onto it once the package lands.
type jobInterface interface {
	// Backgrounded reports whether the job detached into a shell along with
	// a short reason.
	Backgrounded() (bool, string)
	// Logs returns the full accumulated output since start (append-only).
	Logs() string
	// Wait blocks until the process exits and returns its exit code; a
	// non-nil error means the exit code could not be reaped.
	Wait() (int, error)
	Cancel() error
	// PidToken is the process identity issued at start; a later mismatch
	// means the handle no longer refers to the registered process.
	PidToken() string
}

// aliveProbe is an optional jobInterface extension: a job that can report
// liveness lets Status mark orphaned processes that died before Wait
// finished reaping them.
type aliveProbe interface{ Alive() bool }

// LauncherOpts carries the identity context the real launcher forwards to
// internal/command JobOptions.
type LauncherOpts struct {
	RunID       string
	SessionID   string
	WorkspaceID string
	TaskID      string
}

// JobLauncher starts a process. The production adapter wraps
// internal/command StartJob; tests inject fakes.
type JobLauncher func(argv []string, cwd string, env []string, opts LauncherOpts) (jobInterface, error)

// CommandInput is the unified command op request.
type CommandInput struct {
	Op          string            // start | cancel | status
	RunID       string            `json:"runId"`
	SessionID   string            `json:"sessionId"`
	WorkspaceID string            `json:"workspaceId"`
	SpecID      string            `json:"specId"`
	Args        map[string]string `json:"args"`
	Env         map[string]string `json:"env"`
	TaskID      string            `json:"taskId"`
	LogCursor   int               `json:"logCursor"`
}

// CommandResult is the task snapshot; LogTail carries only the bytes after
// LogCursor and NextLogCursor is the position a reconnecting client should
// send next.
type CommandResult struct {
	TaskID        string `json:"taskId"`
	State         string `json:"state"` // running|backgrounded|done|failed|cancelled|orphaned
	Backgrounded  bool   `json:"backgrounded"`
	ExitCode      *int   `json:"exitCode,omitempty"`
	LogTail       string `json:"logTail"`
	NextLogCursor int    `json:"nextLogCursor"`
	LogArtifactID string `json:"logArtifactId,omitempty"`
	PidToken      string `json:"pidToken,omitempty"`
}

type managedTask struct {
	taskID    string
	specID    string
	runID     string
	job       jobInterface
	pidToken  string
	state     string
	exitCode  *int
	logBuf    strings.Builder
	logArtID  string
	startedAt time.Time
}

// CommandService implements command.start/cancel/status over an in-memory
// task registry. uow may be nil (memory-only mode: persistence wiring lands
// with the internal/command integration).
type CommandService struct {
	uow      agentrunapp.UnitOfWork
	launcher JobLauncher
	clock    Clock

	mu    sync.Mutex
	tasks map[string]*managedTask
}

func NewCommandService(uow agentrunapp.UnitOfWork, launcher JobLauncher) *CommandService {
	return &CommandService{uow: uow, launcher: launcher, clock: systemClock{}, tasks: map[string]*managedTask{}}
}

// SetClock substitutes the wall clock (tests).
func (s *CommandService) SetClock(c Clock) { s.clock = c }

// Op dispatches to Start, Cancel or Status after op validation.
func (s *CommandService) Op(ctx context.Context, in CommandInput) (CommandResult, error) {
	switch in.Op {
	case "start":
		return s.Start(ctx, in)
	case "cancel":
		return s.Cancel(ctx, in)
	case "status":
		return s.Status(ctx, in)
	default:
		return CommandResult{}, ErrCmdInvalidOp
	}
}

// workdirFor resolves the workspace root through the path-safety core when a
// uow is wired; memory-only mode (nil uow / empty workspace) skips it.
func (s *CommandService) workdirFor(ctx context.Context, workspaceID string) (string, error) {
	if s.uow == nil || workspaceID == "" {
		return "", nil
	}
	var root string
	err := s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, ok := tx.(wsStore)
		if !ok {
			return workspace.ErrUOWUnavailable
		}
		w, err := st.GetM5Workspace(workspaceID)
		if err != nil {
			return err
		}
		if w.State == m5workspace.StateDeleted {
			return ErrFsWorkspaceGone
		}
		root = w.RootCanonical
		return nil
	})
	return root, err
}

func renderArgv(specID string, args map[string]string) []string {
	argv := []string{specID}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		argv = append(argv, k+"="+args[k])
	}
	return argv
}

func renderEnv(env map[string]string) []string {
	out := make([]string, 0, len(env))
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

// Start launches the spec and registers the task. A client-supplied TaskID
// that already exists is idempotent when the task is terminal (the snapshot
// replays) and answers CMD-002 while the task is still live.
func (s *CommandService) Start(ctx context.Context, in CommandInput) (CommandResult, error) {
	if in.SpecID == "" {
		return CommandResult{}, ErrCmdSpecRequired
	}
	if s.launcher == nil {
		return CommandResult{}, ErrLauncherUnavailable
	}
	if in.TaskID != "" {
		s.mu.Lock()
		mt, ok := s.tasks[in.TaskID]
		s.mu.Unlock()
		if ok {
			if !taskTerminal(mt.state) {
				return CommandResult{}, fmt.Errorf("%w: %s is %s", ErrTaskStateBad, mt.taskID, mt.state)
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			s.syncLogsLocked(mt)
			return s.snapshotLocked(mt, in.LogCursor), nil
		}
	}
	cwd, err := s.workdirFor(ctx, in.WorkspaceID)
	if err != nil {
		return CommandResult{}, err
	}
	taskID := in.TaskID
	if taskID == "" {
		taskID = ulid.Make().String()
	}
	job, err := s.launcher(renderArgv(in.SpecID, in.Args), cwd, renderEnv(in.Env), LauncherOpts{
		RunID: in.RunID, SessionID: in.SessionID, WorkspaceID: in.WorkspaceID, TaskID: taskID,
	})
	if err != nil {
		return CommandResult{}, err
	}
	mt := &managedTask{
		taskID:    taskID,
		specID:    in.SpecID,
		runID:     in.RunID,
		job:       job,
		pidToken:  job.PidToken(),
		state:     TaskRunning,
		startedAt: s.clock.Now().UTC(),
	}
	s.mu.Lock()
	s.tasks[taskID] = mt
	s.syncLogsLocked(mt)
	out := s.snapshotLocked(mt, 0)
	s.mu.Unlock()
	go s.reap(taskID, job)
	return out, nil
}

// reap waits for the process and records the terminal state. Wait errors or
// a pidToken mismatch mean the exit code could not be proven for the
// registered process: the task is fenced as orphaned. A concurrent cancel
// wins: reap never overwrites an already-terminal state.
func (s *CommandService) reap(taskID string, job jobInterface) {
	code, waitErr := job.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	mt := s.tasks[taskID]
	if mt == nil || taskTerminal(mt.state) {
		return
	}
	s.syncLogsLocked(mt)
	switch {
	case waitErr == nil:
		c := code
		mt.exitCode = &c
		if code == 0 {
			mt.state = TaskDone
		} else {
			mt.state = TaskFailed
		}
	case job.PidToken() != mt.pidToken:
		mt.state = TaskOrphaned
	default:
		mt.state = TaskOrphaned
	}
}

// Cancel is idempotent: a live task transitions to cancelled, an
// already-terminal task replays its snapshot with no error.
func (s *CommandService) Cancel(ctx context.Context, in CommandInput) (CommandResult, error) {
	if in.TaskID == "" {
		return CommandResult{}, ErrTaskNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	mt := s.tasks[in.TaskID]
	if mt == nil {
		return CommandResult{}, fmt.Errorf("%w: %s", ErrTaskNotFound, in.TaskID)
	}
	if taskTerminal(mt.state) {
		s.syncLogsLocked(mt)
		return s.snapshotLocked(mt, in.LogCursor), nil
	}
	if err := mt.job.Cancel(); err != nil {
		return CommandResult{}, fmt.Errorf("m5: cancel %s: %w", mt.taskID, err)
	}
	mt.state = TaskCancelled
	s.syncLogsLocked(mt)
	return s.snapshotLocked(mt, in.LogCursor), nil
}

// Status returns the task snapshot. LogCursor=0 replays the full log tail;
// a cursor N returns only the bytes appended after N along with the
// NextLogCursor position (reconnect semantics). A live task is also probed
// for backgrounding and, when the job supports it, liveness: a dead process
// whose Wait has not finished reaping is marked orphaned immediately.
func (s *CommandService) Status(ctx context.Context, in CommandInput) (CommandResult, error) {
	if in.TaskID == "" {
		return CommandResult{}, ErrTaskNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	mt := s.tasks[in.TaskID]
	if mt == nil {
		return CommandResult{}, fmt.Errorf("%w: %s", ErrTaskNotFound, in.TaskID)
	}
	if mt.state == TaskRunning {
		if bg, _ := mt.job.Backgrounded(); bg {
			mt.state = TaskBackgrounded
		}
	}
	if !taskTerminal(mt.state) {
		if ap, ok := mt.job.(aliveProbe); ok && !ap.Alive() {
			mt.state = TaskOrphaned
		}
	}
	s.syncLogsLocked(mt)
	return s.snapshotLocked(mt, in.LogCursor), nil
}

// syncLogsLocked pulls the job's accumulated output into the registry
// buffer; Logs is append-only so only growth beyond the buffered prefix is
// appended. Callers must hold s.mu.
func (s *CommandService) syncLogsLocked(mt *managedTask) {
	if mt.job == nil {
		return
	}
	current := mt.job.Logs()
	if len(current) > mt.logBuf.Len() {
		mt.logBuf.WriteString(current[mt.logBuf.Len():])
	}
}

// snapshotLocked renders the reconnect view. In memory-only mode the log
// artifact id is a local placeholder; once the artifact registry is wired
// the terminal log buffer is persisted to CAS and the real artifact id is
// issued. Callers must hold s.mu.
func (s *CommandService) snapshotLocked(mt *managedTask, cursor int) CommandResult {
	buf := mt.logBuf.String()
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(buf) {
		cursor = len(buf)
	}
	if mt.logArtID == "" && taskTerminal(mt.state) && buf != "" {
		mt.logArtID = "loglocal-" + ulid.Make().String()
	}
	return CommandResult{
		TaskID:        mt.taskID,
		State:         mt.state,
		Backgrounded:  mt.state == TaskBackgrounded,
		ExitCode:      mt.exitCode,
		LogTail:       buf[cursor:],
		NextLogCursor: len(buf),
		LogArtifactID: mt.logArtID,
		PidToken:      mt.pidToken,
	}
}
