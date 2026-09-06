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
)

// The budget covers every model pass in this turn, including reasoning and
// tool arguments. Provider time excludes time waiting for human approval.
type turnGenerationBudget struct {
	mu            sync.Mutex
	bytes, tokens int
	elapsed       time.Duration
	exhausted     bool
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
	remaining := turnGenerationMaxTime - b.elapsed
	if b.exhausted || remaining <= 0 || b.bytes >= turnGenerationMaxBytes || b.tokens >= turnGenerationMaxTokens {
		b.mu.Unlock()
		return llmadapter.Response{}, errTurnGenerationBudget
	}
	req.MaxTokens = min(req.MaxTokens, turnGenerationMaxTokens-b.tokens)
	if req.MaxTokens <= 0 {
		req.MaxTokens = min(chatMaxTokens, turnGenerationMaxTokens-b.tokens)
	}
	b.mu.Unlock()
	op, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	start := time.Now()
	defer func() { b.mu.Lock(); b.elapsed += time.Since(start); b.mu.Unlock() }()
	var callbackErr error
	textBytes, reasoningBytes := 0, 0
	toolBytes := map[string]int{}
	consume := func(n int) error {
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
		if b.exhausted || n > turnGenerationMaxBytes-b.bytes {
			b.exhausted = true
			callbackErr = errTurnGenerationBudget
			cancel()
			return callbackErr
		}
		b.bytes += n
		return nil
	}
	result, err := adapter.Stream(op, secret, req, func(d llmadapter.Delta) error {
		n := len(d.Text) + len(d.Reasoning)
		if d.ToolCall != nil {
			n += max(0, len(d.ToolCall.Arguments)+len(d.ToolCall.Name)-toolBytes[d.ToolCall.ID])
		}
		if e := consume(n); e != nil {
			return e
		}
		textBytes += len(d.Text)
		reasoningBytes += len(d.Reasoning)
		if d.ToolCall != nil {
			toolBytes[d.ToolCall.ID] = len(d.ToolCall.Arguments) + len(d.ToolCall.Name)
		}
		callbackErr = emit(d)
		return callbackErr
	})
	// Some adapters return tool calls/fallback text without a corresponding
	// delta. Charge those before the caller may execute or display them.
	missing := max(0, len(result.Message.Content)-textBytes) + max(0, len(result.Reasoning)-reasoningBytes)
	for _, tc := range result.Message.ToolCalls {
		missing += max(0, len(tc.Name)+len(tc.Arguments)-toolBytes[tc.ID])
	}
	if callbackErr == nil && err == nil {
		callbackErr = consume(missing)
	}
	b.mu.Lock()
	b.tokens += max(0, result.Usage.OutputTokens)
	tokensExceeded := b.tokens > turnGenerationMaxTokens
	if tokensExceeded {
		b.exhausted = true
	}
	b.mu.Unlock()
	if callbackErr != nil {
		return result, callbackErr
	}
	if (op.Err() != nil && ctx.Err() == nil) || tokensExceeded {
		return result, errTurnGenerationBudget
	}
	return result, err
}
