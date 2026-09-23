package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

var errTurnGenerationBudget = errors.New("turn generation budget exhausted")

const (
	turnGenerationMaxBytes     = 512 << 10
	turnGenerationMaxTokens    = 131072
	turnGenerationMaxTime      = 10 * time.Minute
	turnGenerationBudgetNotice = "\n\n（本轮已达到生成总预算，已停止继续生成并保留此前收到的内容。你可以发送“继续”接着完成。）\n"

	// Reasoning is discarded content: never spoken, never stored as the
	// reply, never shown as the answer. Charging it to the same bucket as the
	// deliverable meant one deep-thinking pass could spend the whole turn's
	// allowance before any work happened, so a reasoning model hit "预算已满"
	// on tasks a non-reasoning model finished comfortably. It gets its own
	// ceiling, still hard — a runaway thinker is stopped, not truncated.
	turnReasoningMaxBytes = 2 << 20

	// A turn that keeps executing real tools has demonstrated it is doing
	// work rather than looping on itself, so each productive batch earns one
	// more allowance up to these hard ceilings. This mirrors the adaptive
	// step limit (extendToolLoopLimit): the cap still exists, it just stops
	// cutting off long legitimate jobs midway.
	turnGenerationHardBytes  = 2 << 20
	turnReasoningHardBytes   = 8 << 20
	turnGenerationHardTokens = 4 * turnGenerationMaxTokens
	turnGenerationHardTime   = 30 * time.Minute
	turnGenerationByteChunk  = 512 << 10
	turnReasoningByteChunk   = 2 << 20
	turnGenerationTokenChunk = turnGenerationMaxTokens
	turnGenerationTimeChunk  = 10 * time.Minute

	// Closing reserve: once the allowance is spent, the turn still owes the
	// user an answer about the work it already did. The reserve funds exactly
	// that one no-tools summary pass, so exhaustion ends in a report instead
	// of a dead end.
	turnGenerationReserveBytes = 32 << 10
	turnGenerationReserveTime  = 2 * time.Minute
)

// The budget covers every model pass in this turn, including reasoning and
// tool arguments. Provider time excludes time waiting for human approval.
type turnGenerationBudget struct {
	mu            sync.Mutex
	bytes, tokens int
	reasoning     int
	elapsed       time.Duration
	exhausted     bool
	reserveOpened bool
	continueWaves int
	// Ceilings start at 0 meaning "the default"; a productive turn raises
	// them in place so the zero value is still a usable budget.
	byteCap, reasoningCap, tokenCap int
	timeCap                         time.Duration
	usage                           llmadapter.Usage
	usageCalls                      int
	cacheReportedCalls              int
}

// A zero cap means "use the default". A nonzero cap is honoured exactly, so
// the closing reserve can narrow an allowance as well as widen it.
func (b *turnGenerationBudget) byteLimit() int {
	if b.byteCap == 0 {
		return turnGenerationMaxBytes
	}
	return b.byteCap
}

func (b *turnGenerationBudget) reasoningLimit() int {
	if b.reasoningCap == 0 {
		return turnReasoningMaxBytes
	}
	return b.reasoningCap
}

func (b *turnGenerationBudget) tokenLimit() int {
	if b.tokenCap == 0 {
		return turnGenerationMaxTokens
	}
	return b.tokenCap
}

func (b *turnGenerationBudget) timeLimit() time.Duration {
	if b.timeCap == 0 {
		return turnGenerationMaxTime
	}
	return b.timeCap
}

func growAllowance[T int | time.Duration](current, used, chunk, hard T) T {
	if current >= hard {
		return current
	}
	// Only the dimension actually running out earns more room. Growing an
	// untouched dimension would turn the ceiling into a formality.
	if used < current-chunk/2 {
		return current
	}
	if next := current + chunk; next < hard {
		return next
	}
	return hard
}

// noteToolProgress records that the loop just executed real tool calls and
// raises whichever allowance is close to running out. A turn that stalls, or
// that stops calling tools, never reaches here and keeps the base budget.
func (b *turnGenerationBudget) noteToolProgress() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.exhausted {
		return
	}
	b.byteCap = growAllowance(b.byteLimit(), b.bytes, turnGenerationByteChunk, turnGenerationHardBytes)
	b.reasoningCap = growAllowance(b.reasoningLimit(), b.reasoning, turnReasoningByteChunk, turnReasoningHardBytes)
	b.tokenCap = growAllowance(b.tokenLimit(), b.tokens, turnGenerationTokenChunk, turnGenerationHardTokens)
	b.timeCap = growAllowance(b.timeLimit(), b.elapsed, turnGenerationTimeChunk, turnGenerationHardTime)
}

// openClosingReserve funds one final no-tools summary after the allowance ran
// out, so the user learns what got done instead of only that a limit was hit.
// It opens once per turn and grants deliverable bytes only — the reserve must
// not pay for more thinking or more tool calls.
// reopenForNextWave funds another tool round after the allowance ran out
// mid-task. It is separate from the one-shot no-tools closing reserve.
func (b *turnGenerationBudget) reopenForNextWave() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.reserveOpened || b.continueWaves >= 2 {
		return false
	}
	b.continueWaves++
	b.exhausted = false
	b.byteCap = b.byteLimit() + turnGenerationByteChunk
	if b.byteCap > turnGenerationHardBytes+turnGenerationByteChunk {
		b.byteCap = turnGenerationHardBytes + turnGenerationByteChunk
	}
	b.tokenCap = b.tokenLimit() + turnGenerationTokenChunk
	if b.tokenCap > turnGenerationHardTokens+turnGenerationTokenChunk {
		b.tokenCap = turnGenerationHardTokens + turnGenerationTokenChunk
	}
	b.timeCap = b.timeLimit() + turnGenerationTimeChunk
	if b.timeCap > turnGenerationHardTime+turnGenerationTimeChunk {
		b.timeCap = turnGenerationHardTime + turnGenerationTimeChunk
	}
	return true
}

func (b *turnGenerationBudget) openClosingReserve() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.reserveOpened {
		return false
	}
	b.reserveOpened = true
	b.exhausted = false
	b.byteCap = b.bytes + turnGenerationReserveBytes
	// No new thinking: the reserve buys a report, not another deliberation.
	// A cap of zero would read as "default", so keep it at least one byte.
	b.reasoningCap = max(b.reasoning, 1)
	b.tokenCap = b.tokens + turnGenerationReserveBytes/4
	b.timeCap = b.elapsed + turnGenerationReserveTime
	return true
}

type turnBudgetAdapter struct {
	llmadapter.Adapter
	budget *turnGenerationBudget
}

func (a turnBudgetAdapter) Complete(ctx context.Context, secret []byte, req llmadapter.Request) (llmadapter.Response, error) {
	return a.budget.stream(ctx, a.Adapter, secret, req, func(llmadapter.Delta) error { return nil })
}
func (a turnBudgetAdapter) Stream(ctx context.Context, secret []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	return a.budget.stream(ctx, a.Adapter, secret, req, emit)
}

func (b *turnGenerationBudget) stream(ctx context.Context, adapter llmadapter.Adapter, secret []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	b.mu.Lock()
	remaining := b.timeLimit() - b.elapsed
	tokenRoom := b.tokenLimit() - b.tokens
	if b.exhausted || remaining <= 0 || b.bytes >= b.byteLimit() || tokenRoom <= 0 {
		b.mu.Unlock()
		return llmadapter.Response{}, errTurnGenerationBudget
	}
	req.MaxTokens = min(req.MaxTokens, tokenRoom)
	if req.MaxTokens <= 0 {
		req.MaxTokens = min(chatMaxTokens, tokenRoom)
	}
	b.mu.Unlock()
	op, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	start := time.Now()
	defer func() { b.mu.Lock(); b.elapsed += time.Since(start); b.mu.Unlock() }()
	var callbackErr error
	textBytes, reasoningBytes := 0, 0
	toolBytes := map[string]int{}
	var pendingTools []llmadapter.Delta
	var reportedUsage llmadapter.Usage
	// deliverable = reply text plus tool arguments (what the turn actually
	// produces); thought = reasoning, which is discarded and billed separately.
	consume := func(deliverable, thought int) error {
		b.mu.Lock()
		defer b.mu.Unlock()
		if callbackErr != nil {
			return callbackErr
		}
		if err := op.Err(); err != nil {
			if ctx.Err() == nil {
				callbackErr = errTurnGenerationBudget
			} else {
				callbackErr = ctx.Err()
			}
			return callbackErr
		}
		if b.exhausted || deliverable > b.byteLimit()-b.bytes || thought > b.reasoningLimit()-b.reasoning {
			b.exhausted = true
			callbackErr = errTurnGenerationBudget
			cancel()
			return callbackErr
		}
		b.bytes += deliverable
		b.reasoning += thought
		return nil
	}
	result, err := adapter.Stream(op, secret, req, func(d llmadapter.Delta) error {
		// Per-provider deltas are cumulative snapshots, not separate model
		// calls. Retain the latest only, including on partial stream failure.
		if d.Usage != nil {
			reportedUsage = *d.Usage
		}
		n := len(d.Text)
		if d.ToolCall != nil {
			n += max(0, len(d.ToolCall.Arguments)+len(d.ToolCall.Name)-toolBytes[d.ToolCall.ID])
		}
		if e := consume(n, len(d.Reasoning)); e != nil {
			return e
		}
		textBytes += len(d.Text)
		reasoningBytes += len(d.Reasoning)
		if d.ToolCall != nil {
			toolBytes[d.ToolCall.ID] = len(d.ToolCall.Arguments) + len(d.ToolCall.Name)
			// Finish metadata arrives after tool deltas. Release tools only once
			// the whole response is known to be complete and within budget.
			call := *d.ToolCall
			call.Arguments = append([]byte(nil), call.Arguments...)
			pendingTools = append(pendingTools, llmadapter.Delta{ToolCall: &call})
			d.ToolCall = nil
		}
		if emit != nil && (d.Text != "" || d.Reasoning != "" || d.Usage != nil) {
			callbackErr = emit(d)
		}
		return callbackErr
	})
	if result.Usage.TotalTokens == 0 && result.Usage.InputTokens == 0 && result.Usage.OutputTokens == 0 && !result.Usage.CacheUsageReported {
		result.Usage = reportedUsage
	}
	// Some adapters return tool calls/fallback text without a corresponding
	// delta. Charge those before the caller may execute or display them.
	missing := max(0, len(result.Message.Content)-textBytes)
	for _, tc := range result.Message.ToolCalls {
		missing += max(0, len(tc.Name)+len(tc.Arguments)-toolBytes[tc.ID])
	}
	missingThought := max(0, len(result.Reasoning)-reasoningBytes)
	if callbackErr == nil && err == nil && (missing > 0 || missingThought > 0) {
		callbackErr = consume(missing, missingThought)
	}
	b.mu.Lock()
	b.usageCalls++
	b.usage.InputTokens += max(0, result.Usage.InputTokens)
	b.usage.OutputTokens += max(0, result.Usage.OutputTokens)
	b.usage.TotalTokens += max(0, result.Usage.TotalTokens)
	b.usage.CachedInputTokens += max(0, result.Usage.CachedInputTokens)
	b.usage.CacheWriteInputTokens += max(0, result.Usage.CacheWriteInputTokens)
	if result.Usage.CacheUsageReported {
		b.cacheReportedCalls++
	}
	b.tokens += max(0, result.Usage.OutputTokens)
	tokensExceeded := b.tokens > b.tokenLimit()
	if tokensExceeded {
		b.exhausted = true
	}
	b.mu.Unlock()
	if callbackErr != nil {
		result.Message.ToolCalls = nil
		return result, callbackErr
	}
	if (op.Err() != nil && ctx.Err() == nil) || tokensExceeded {
		result.Message.ToolCalls = nil
		return result, errTurnGenerationBudget
	}
	if err == nil {
		err = chatModelFinishError(result.FinishReason)
	}
	if err != nil {
		result.Message.ToolCalls = nil
		return result, err
	}
	for _, delta := range pendingTools {
		if emit != nil {
			if err := emit(delta); err != nil {
				result.Message.ToolCalls = nil
				return result, err
			}
		}
	}
	return result, err
}

func (b *turnGenerationBudget) usageSnapshot() llmadapter.Usage {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := b.usage
	// Partial known cache counts remain available, but are never presented as
	// a complete hit rate if any expert/tool/model pass omitted its metadata.
	u.CacheUsageReported = b.usageCalls > 0 && b.cacheReportedCalls == b.usageCalls
	return u
}
