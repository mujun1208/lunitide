package stdioworker

import (
	"context"
	"io"
)

// IsolatedProc is one short-lived child process launched under the same
// 5B/5C-hardened spawn engine the stdio worker runtime uses: a fresh Job
// Object per run (kill-on-close, active-process and commit quotas, no
// parent-environment inheritance), suspended start → assign → resume.
//
// It exists for the MCP stdio transport (M6-MCP-004 gate opened after the
// 5C red-team PASS): each probe/describe/invoke session spawns the MCP
// server, speaks newline-delimited JSON-RPC over the pipes and then kills
// the whole tree — no long-lived privileged channel, no state carry-over
// between calls. The signed-spec/journal/heartbeat machinery of Manager is
// intentionally not involved: these runs are seconds-lived, quota-bounded
// and audited by the mcp6 registry layer instead.
type IsolatedProc struct {
	inner *engineProc
}

// StdioQuotas is the frozen quota envelope for one MCP stdio session: the
// process tree is capped at 32 procs / 1 GiB commit. The wall-clock bound
// is the caller's context deadline (the registry enforces 30 s).
func StdioQuotas() Quotas {
	return Quotas{
		MaxProcs:        32,
		MemoryCapBytes:  1 << 30,
		DeadlineMS:      60_000,
		HeartbeatMS:     250,
		MaxMissedBeats:  120,
	}
}

// SpawnIsolated launches cmd under a fresh Job Object with an explicit
// environment block (the parent environment never leaks) and returns the
// pipe handles. The caller owns the proc: Close kills the tree.
func SpawnIsolated(cmd string, args []string, dir string, env []string, q Quotas) (*IsolatedProc, error) {
	p, err := engineSpawn(cmd, args, dir, env, q)
	if err != nil {
		return nil, err
	}
	return &IsolatedProc{inner: p}, nil
}

// Stdin is the child's stdin pipe (caller writes requests).
func (p *IsolatedProc) Stdin() io.Writer {
	if p == nil || p.inner == nil {
		return nil
	}
	in, _, _ := p.inner.stdioHandles()
	return in
}

// Stdout is the child's stdout pipe (caller reads responses).
func (p *IsolatedProc) Stdout() io.Reader {
	if p == nil || p.inner == nil {
		return nil
	}
	_, out, _ := p.inner.stdioHandles()
	return out
}

// PID is the root child process id (0 when unknown).
func (p *IsolatedProc) PID() int {
	if p == nil || p.inner == nil {
		return 0
	}
	_, _, pid := p.inner.stdioHandles()
	return pid
}

// Kill terminates the whole process tree (Job Object terminate).
func (p *IsolatedProc) Kill() error {
	if p == nil || p.inner == nil {
		return nil
	}
	return p.inner.kill()
}

// Wait blocks for the root process exit; ctx cancellation kills the tree.
func (p *IsolatedProc) Wait(ctx context.Context) (int, error) {
	if p == nil || p.inner == nil {
		return -1, nil
	}
	return p.inner.wait(ctx)
}

// Close releases the OS handles; kill-on-close reaps any survivor.
func (p *IsolatedProc) Close() {
	if p == nil || p.inner == nil {
		return
	}
	p.inner.close()
}
