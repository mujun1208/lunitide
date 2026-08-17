// M10+ efficiency wave (P0-1): parallel execution of independent tool calls
// within one model turn. Same-turn MCP calls and read-only engine tools are
// pre-started on bounded background goroutines (the proven subagent-futures
// pattern from chat_subagent.go); the main loop still consumes results in
// original call order, keeping the event stream and tool-message order
// deterministic.
//
// Concurrency safety contract (verified 2026-08-17):
//   - Read-only engine tools (workspace.list/read/search, web.fetch/search,
//     excel.parse) never hit the approval gate (Execute gates only mutating
//     tools) and hold no shared mutable state; hooks evaluation is RLock
//     guarded and hook-event writes go through the serialized SQLite layer.
//   - MCP invokes ride Registry.Invoke: state/breaker/pinning rechecks are
//     mutex-serialized per call while the actual I/O runs outside the lock;
//     both transports dial a fresh client/session per invocation (stdio
//     spawns an isolated child per call, HTTP builds a new client), so there
//     is no per-endpoint shared connection to corrupt.
//   - Mutating tools (workspace.write/edit, command.run, todo.write, office
//     generators), cc.* machine-actuation and plan/subagent tools stay
//     serial: side-effect ordering and the approval/confirmation gates
//     must not be interleaved.
package app

import (
	"context"
	"fmt"

	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

// maxParallelToolCalls bounds background tool goroutines per model turn.
const maxParallelToolCalls = 6

// parallelReadOnlyTools are engine tools with no side effects and no
// approval path; they are safe to overlap within one turn.
var parallelReadOnlyTools = map[string]bool{
	"workspace.list":   true,
	"workspace.read":   true,
	"workspace.search": true,
	"web.fetch":        true,
	"web.search":       true,
	"excel.parse":      true,
}

// parallelToolEligible reports whether one tool call may run on a
// background goroutine. MCP tools qualify wholesale (the registry owns
// per-call gating); engine tools must be on the read-only allowlist.
func parallelToolEligible(name string) bool {
	if parallelReadOnlyTools[name] {
		return true
	}
	// 严格遵循读写分离：
	// 对于 MCP 工具或未知的本地工具，由于无法在分发层判定其是否包含写操作（例如并发写文件、执行命令），
	// 为避免 SQLite 写锁冲突 (database is locked) 或产生脏写，强制降级为串行执行。
	return false
}

// parallelToolFuture carries one background tool outcome to the main loop.
// Exactly one of the result shapes is meaningful: MCP calls answer a flat
// summary, engine tools answer a toolruntime.Result (which may carry an
// artifact). err mirrors the inline execution error path.
type parallelToolFuture struct {
	summary   string
	result    toolruntime.Result
	err       error
	isRuntime bool
}

// startParallelToolFutures pre-starts up to maxParallelToolCalls eligible
// calls from one model turn on background goroutines so independent
// lookups overlap instead of queueing. Results flow through buffered
// channels consumed by the main loop in original call order; ineligible
// calls and calls beyond the bound fall back to inline execution.
func startParallelToolFutures(op context.Context, e *Engine, mode executionMode, sessionID string, calls []gateway.ToolCall) map[string]chan parallelToolFuture {
	futures := make(map[string]chan parallelToolFuture)
	started := 0
	for _, call := range calls {
		if !parallelToolEligible(call.Name) || started >= maxParallelToolCalls {
			continue
		}
		started++
		ch := make(chan parallelToolFuture, 1)
		futures[call.ID] = ch
		go func(call gateway.ToolCall) {
			// Same panic discipline as startSubagentFutures: a panicking
			// tool must degrade to an error result, never kill the Engine
			// process (which would sever the event pipe for every session).
			defer func() {
				if r := recover(); r != nil {
					ch <- parallelToolFuture{err: fmt.Errorf("tool %s panicked: %v", call.Name, r)}
				}
			}()
			if endpointID, mcpTool, isMcp := parseMcpToolName(call.Name); isMcp {
				summary, err := e.invokeMcpTool(op, endpointID, mcpTool, call.Arguments)
				ch <- parallelToolFuture{summary: summary, err: err}
				return
			}
			result, err := e.executeUserTool(op, mode, sessionID, call.Name, call.Arguments)
			ch <- parallelToolFuture{result: result, err: err, isRuntime: true}
		}(call)
	}
	return futures
}

// drainParallelToolFutures waits for unconsumed background results when the
// turn aborts early (duplicate call ID, invalid args, send failure). The
// lease context cancellation bounds the wait, mirroring the subagent
// futures drain contract.
func drainParallelToolFutures(op context.Context, futures map[string]chan parallelToolFuture) {
	for _, ch := range futures {
		select {
		case <-ch:
		case <-op.Done():
			return
		}
	}
}
