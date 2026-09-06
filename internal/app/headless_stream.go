package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/scheduler"
)

type headlessCollector struct {
	mu       sync.Mutex
	done     chan struct{}
	finished bool
	text     strings.Builder
	runes    int
	tokens   int64
	err      error
}

func (c *headlessCollector) emit(event bridge.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished {
		return nil
	}
	if event.Delta != nil {
		for _, r := range event.Delta.Text {
			if c.runes >= 500 {
				break
			}
			c.text.WriteRune(r)
			c.runes++
		}
	}
	if event.Usage != nil {
		c.tokens += int64(event.Usage.TotalTokens)
	}
	if event.Error != nil {
		c.err = errors.New(event.Error.Message + " (" + event.Error.Code + ")")
	}
	switch event.Type {
	case bridge.EventApprovalRequired:
		c.err = errors.New("自动化操作需要人工审批，请在对话中完成审批后重新执行")
	case bridge.EventCancelled:
		c.err = context.Canceled
	case bridge.EventFailed:
		if c.err == nil {
			c.err = errors.New("自动化对话执行失败")
		}
	case bridge.EventCompleted:
		if event.Completed != nil && event.Completed.PersistFailed {
			c.err = errors.New("自动化回复尚未持久化，结果待核对")
		}
	default:
		return nil
	}
	c.finished = true
	close(c.done)
	return nil
}

func (c *headlessCollector) outcome() scheduler.Outcome {
	c.mu.Lock()
	defer c.mu.Unlock()
	return scheduler.Outcome{Summary: c.text.String(), TotalTokens: c.tokens, Err: c.err}
}

func (e *Engine) runHeadlessStream(ctx context.Context, request bridge.Request) scheduler.Outcome {
	collector := &headlessCollector{done: make(chan struct{})}
	response := e.HandleStreaming(withUnattended(ctx), request, collector.emit)
	if !response.OK {
		if response.Error != nil {
			return scheduler.Outcome{Err: errors.New(response.Error.Message + " (" + response.Error.Code + ")")}
		}
		return scheduler.Outcome{Err: errors.New("自动化对话未能启动")}
	}
	var started struct {
		StreamID string `json:"streamId"`
	}
	raw, err := json.Marshal(response.Payload)
	if err == nil {
		err = json.Unmarshal(raw, &started)
	}
	if err != nil || started.StreamID == "" {
		return scheduler.Outcome{Err: errors.New("自动化对话启动回执缺失")}
	}
	select {
	case <-collector.done:
		out := collector.outcome()
		if out.Err != nil {
			e.cancelStream(started.StreamID)
		}
		return out
	case <-ctx.Done():
		e.cancelStream(started.StreamID)
		out := collector.outcome()
		out.Err = ctx.Err()
		return out
	}
}
