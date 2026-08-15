package m5

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeJob is a scriptable jobInterface: tests control logs in stages,
// backgrounding, the Wait outcome, successive pidTokens and liveness.
type fakeJob struct {
	mu           sync.Mutex
	logs         string
	bg           bool
	bgReason     string
	exit         int
	waitErr      error
	blockWait    chan struct{}
	waitReturned chan struct{}
	waitOnce     sync.Once
	pidScript    []string // successive PidToken() answers, last repeats
	pidCalls     int
	alive        bool
	cancelCount  int
	cancelErr    error
}

func newFakeJob() *fakeJob {
	return &fakeJob{pidScript: []string{"pid-1"}, alive: true, waitReturned: make(chan struct{})}
}

func (f *fakeJob) setLogs(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = s
}

func (f *fakeJob) Logs() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logs
}

func (f *fakeJob) Backgrounded() (bool, string) { return f.bg, f.bgReason }

func (f *fakeJob) Wait() (int, error) {
	if f.blockWait != nil {
		<-f.blockWait
	}
	f.waitOnce.Do(func() { close(f.waitReturned) })
	return f.exit, f.waitErr
}

func (f *fakeJob) Cancel() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelCount++
	return f.cancelErr
}

func (f *fakeJob) PidToken() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.pidCalls
	if i >= len(f.pidScript) {
		i = len(f.pidScript) - 1
	}
	f.pidCalls++
	return f.pidScript[i]
}

func (f *fakeJob) Alive() bool { return f.alive }

type fakeLauncher struct {
	job      *fakeJob
	err      error
	calls    int
	lastArgv []string
	lastCwd  string
	lastEnv  []string
	lastOpts LauncherOpts
}

func (l *fakeLauncher) launch(argv []string, cwd string, env []string, opts LauncherOpts) (jobInterface, error) {
	l.calls++
	l.lastArgv = argv
	l.lastCwd = cwd
	l.lastEnv = env
	l.lastOpts = opts
	if l.err != nil {
		return nil, l.err
	}
	return l.job, nil
}

func startInput(spec string) CommandInput {
	return CommandInput{
		Op: "start", RunID: "run-1", SessionID: "sess-1", WorkspaceID: "ws-1",
		SpecID: spec, Args: map[string]string{"b": "2", "a": "1"}, Env: map[string]string{"LANG": "en"},
	}
}

func waitForState(t *testing.T, svc *CommandService, taskID, want string) CommandResult {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(2 * time.Second)
	for {
		res, err := svc.Op(ctx, CommandInput{Op: "status", TaskID: taskID})
		if err != nil {
			t.Fatal(err)
		}
		if res.State == want {
			return res
		}
		if taskTerminal(res.State) {
			t.Fatalf("task reached %s before %s", res.State, want)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for state %s, last %+v", want, res)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestCommandReconnect covers T-5.3.4 reconnect semantics: status returns
// only the log bytes appended after LogCursor and advances NextLogCursor.
func TestCommandReconnect(t *testing.T) {
	ctx := context.Background()
	job := newFakeJob()
	job.setLogs("line1\n")
	l := &fakeLauncher{job: job}
	svc := NewCommandService(nil, l.launch)

	started, err := svc.Op(ctx, startInput("spec.echo"))
	if err != nil {
		t.Fatal(err)
	}
	if started.TaskID == "" || started.State != TaskRunning || started.PidToken != "pid-1" {
		t.Fatalf("start = %+v", started)
	}
	if got := strings.Join(l.lastArgv, " "); got != "spec.echo a=1 b=2" {
		t.Fatalf("argv = %q", got)
	}
	if len(l.lastEnv) != 1 || l.lastEnv[0] != "LANG=en" {
		t.Fatalf("env = %v", l.lastEnv)
	}

	// Full replay for a fresh client (cursor 0).
	first, err := svc.Op(ctx, CommandInput{Op: "status", TaskID: started.TaskID})
	if err != nil {
		t.Fatal(err)
	}
	if first.LogTail != "line1\n" || first.NextLogCursor != len("line1\n") {
		t.Fatalf("first status = %+v", first)
	}

	// The job appends a second log segment; only the delta comes back.
	job.setLogs("line1\nline2\n")
	second, err := svc.Op(ctx, CommandInput{Op: "status", TaskID: started.TaskID, LogCursor: first.NextLogCursor})
	if err != nil {
		t.Fatal(err)
	}
	if second.LogTail != "line2\n" || second.NextLogCursor != len("line1\nline2\n") {
		t.Fatalf("delta status = %+v", second)
	}

	// Caught-up client gets an empty tail and a stable cursor.
	third, err := svc.Op(ctx, CommandInput{Op: "status", TaskID: started.TaskID, LogCursor: second.NextLogCursor})
	if err != nil {
		t.Fatal(err)
	}
	if third.LogTail != "" || third.NextLogCursor != second.NextLogCursor {
		t.Fatalf("caught-up status = %+v", third)
	}

	// A cursor beyond the buffer clamps instead of failing.
	over, err := svc.Op(ctx, CommandInput{Op: "status", TaskID: started.TaskID, LogCursor: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if over.LogTail != "" || over.NextLogCursor != second.NextLogCursor {
		t.Fatalf("over-cursor status = %+v", over)
	}
}

// TestCommandCancelIdempotent covers cancel idempotence: the second cancel
// of a cancelled task replays the snapshot with no error and never reaches
// the job again.
func TestCommandCancelIdempotent(t *testing.T) {
	ctx := context.Background()
	job := newFakeJob()
	job.blockWait = make(chan struct{})
	svc := NewCommandService(nil, (&fakeLauncher{job: job}).launch)

	started, err := svc.Op(ctx, startInput("spec.sleep"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.Op(ctx, CommandInput{Op: "cancel", TaskID: started.TaskID})
	if err != nil {
		t.Fatal(err)
	}
	if first.State != TaskCancelled {
		t.Fatalf("first cancel state = %s", first.State)
	}
	second, err := svc.Op(ctx, CommandInput{Op: "cancel", TaskID: started.TaskID})
	if err != nil {
		t.Fatalf("second cancel must be idempotent: %v", err)
	}
	if second.State != TaskCancelled || second.TaskID != started.TaskID {
		t.Fatalf("second cancel = %+v", second)
	}
	job.mu.Lock()
	calls := job.cancelCount
	job.mu.Unlock()
	if calls != 1 {
		t.Fatalf("job.Cancel called %d times, want 1", calls)
	}
	// A late reap must not overwrite the cancelled state: unblock Wait,
	// let the reaper run its beat, then assert cancelled survived.
	close(job.blockWait)
	<-job.waitReturned
	time.Sleep(20 * time.Millisecond)
	final := waitForState(t, svc, started.TaskID, TaskCancelled)
	if final.ExitCode != nil {
		t.Fatalf("cancelled task exitCode = %v, want nil", *final.ExitCode)
	}
}

// TestCommandOrphaned covers orphan fencing: a Wait that cannot prove the
// exit code fences the task as orphaned, both when the pid token no longer
// matches the registration and when liveness probes report a dead process
// before Wait finished.
func TestCommandOrphaned(t *testing.T) {
	ctx := context.Background()

	t.Run("wait error with mismatched pid token", func(t *testing.T) {
		job := newFakeJob()
		job.pidScript = []string{"pid-live", "pid-reused"}
		job.waitErr = errors.New("process lost")
		svc := NewCommandService(nil, (&fakeLauncher{job: job}).launch)
		started, err := svc.Op(ctx, startInput("spec.orphan"))
		if err != nil {
			t.Fatal(err)
		}
		res := waitForState(t, svc, started.TaskID, TaskOrphaned)
		if res.ExitCode != nil || res.PidToken != "pid-live" {
			t.Fatalf("orphan snapshot = %+v", res)
		}
	})

	t.Run("dead process probe while wait is stuck", func(t *testing.T) {
		job := newFakeJob()
		job.blockWait = make(chan struct{})
		job.alive = false
		defer close(job.blockWait)
		svc := NewCommandService(nil, (&fakeLauncher{job: job}).launch)
		started, err := svc.Op(ctx, startInput("spec.stuck"))
		if err != nil {
			t.Fatal(err)
		}
		res, err := svc.Op(ctx, CommandInput{Op: "status", TaskID: started.TaskID})
		if err != nil {
			t.Fatal(err)
		}
		if res.State != TaskOrphaned {
			t.Fatalf("state = %s, want orphaned", res.State)
		}
	})
}

// TestCommandBackgroundFlag covers backgrounding: a job that reports itself
// detached surfaces Backgrounded=true and the backgrounded state.
func TestCommandBackgroundFlag(t *testing.T) {
	ctx := context.Background()
	job := newFakeJob()
	job.bg = true
	job.bgReason = "user detached"
	job.blockWait = make(chan struct{})
	defer close(job.blockWait)
	svc := NewCommandService(nil, (&fakeLauncher{job: job}).launch)

	started, err := svc.Op(ctx, startInput("spec.shell"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.Op(ctx, CommandInput{Op: "status", TaskID: started.TaskID})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Backgrounded || res.State != TaskBackgrounded {
		t.Fatalf("backgrounded status = %+v", res)
	}
	if res.PidToken != "pid-1" {
		t.Fatalf("pidToken = %q", res.PidToken)
	}
}

// TestCommandValidation covers CMD-001/TASK-001/CMD-002 refusals and the
// done path with an exit code and a terminal log artifact reference.
func TestCommandValidation(t *testing.T) {
	ctx := context.Background()

	t.Run("start without spec", func(t *testing.T) {
		svc := NewCommandService(nil, (&fakeLauncher{job: newFakeJob()}).launch)
		in := startInput("")
		if _, err := svc.Op(ctx, in); !errors.Is(err, ErrCmdSpecRequired) {
			t.Fatalf("err = %v, want ErrCmdSpecRequired", err)
		}
	})

	t.Run("bogus op", func(t *testing.T) {
		svc := NewCommandService(nil, (&fakeLauncher{job: newFakeJob()}).launch)
		if _, err := svc.Op(ctx, CommandInput{Op: "spawn", TaskID: "t"}); !errors.Is(err, ErrCmdInvalidOp) {
			t.Fatalf("err = %v, want ErrCmdInvalidOp", err)
		}
	})

	t.Run("unknown task", func(t *testing.T) {
		svc := NewCommandService(nil, (&fakeLauncher{job: newFakeJob()}).launch)
		if _, err := svc.Op(ctx, CommandInput{Op: "status", TaskID: "nope"}); !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("err = %v, want ErrTaskNotFound", err)
		}
	})

	t.Run("no launcher wired", func(t *testing.T) {
		svc := NewCommandService(nil, nil)
		if _, err := svc.Op(ctx, startInput("spec.x")); !errors.Is(err, ErrLauncherUnavailable) {
			t.Fatalf("err = %v, want ErrLauncherUnavailable", err)
		}
	})

	t.Run("launcher failure propagates", func(t *testing.T) {
		boom := errors.New("spawn refused")
		svc := NewCommandService(nil, (&fakeLauncher{err: boom}).launch)
		if _, err := svc.Op(ctx, startInput("spec.x")); !errors.Is(err, boom) {
			t.Fatalf("err = %v, want %v", err, boom)
		}
	})

	t.Run("duplicate live start is CMD-002", func(t *testing.T) {
		job := newFakeJob()
		job.blockWait = make(chan struct{})
		defer close(job.blockWait)
		svc := NewCommandService(nil, (&fakeLauncher{job: job}).launch)
		in := startInput("spec.once")
		in.TaskID = "task-dup"
		if _, err := svc.Op(ctx, in); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Op(ctx, in); !errors.Is(err, ErrTaskStateBad) {
			t.Fatalf("err = %v, want ErrTaskStateBad", err)
		}
	})

	t.Run("terminal task replays snapshot on restart", func(t *testing.T) {
		job := newFakeJob()
		job.setLogs("done-log\n")
		svc := NewCommandService(nil, (&fakeLauncher{job: job}).launch)
		in := startInput("spec.once")
		in.TaskID = "task-replay"
		if _, err := svc.Op(ctx, in); err != nil {
			t.Fatal(err)
		}
		res := waitForState(t, svc, "task-replay", TaskDone)
		if res.ExitCode == nil || *res.ExitCode != 0 {
			t.Fatalf("exitCode = %v, want 0", res.ExitCode)
		}
		if res.LogArtifactID == "" {
			t.Fatalf("terminal task must reference its log artifact: %+v", res)
		}
		replay, err := svc.Op(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if replay.State != TaskDone || replay.LogTail != "done-log\n" || replay.LogArtifactID != res.LogArtifactID {
			t.Fatalf("replay = %+v, want stable done snapshot", replay)
		}
	})
}
