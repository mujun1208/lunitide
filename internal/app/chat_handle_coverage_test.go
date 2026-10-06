package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

// Coverage for the handleChatStart compaction-checkpoint branch (chat.go
// priority-3 context assembly) and the reasoning-level normalizer fold.

type fakeCoverageSummaryReader struct {
	summary string
	end     int64
	err     error
	calls   int
}

func (f *fakeCoverageSummaryReader) GetLatestCompactionSummary(ctx context.Context, sessionID string) (string, error) {
	f.calls++
	return f.summary, f.err
}

func (f *fakeCoverageSummaryReader) GetLatestCompactionCheckpoint(ctx context.Context, sessionID string) (string, int64, error) {
	f.calls++
	return f.summary, f.end, f.err
}

type fakeFlatSummaryReader struct {
	summary string
	err     error
	calls   int
}

func (f *fakeFlatSummaryReader) GetLatestCompactionSummary(ctx context.Context, sessionID string) (string, error) {
	f.calls++
	return f.summary, f.err
}

// startChatWithEngine runs one chat turn on an engine the test customized
// (e.g. injected a summary reader) and captures the adapter request.
func startChatWithEngine(t *testing.T, payload string, customize func(*Engine)) llmadapter.Request {
	t.Helper()
	requests := make(chan llmadapter.Request, 1)
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	if customize != nil {
		customize(e)
	}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
	if !response.OK {
		t.Fatalf("chat.start failed: %s", response.Error.Message)
	}
	return capturedChatRequest(t, requests)
}

// sessionPayload is a durable-session chat turn: a valid session id plus an
// empty message reader routes handleChatStart through the full context
// assembly path (chat.go durable-session branch) instead of the explicit
// single-turn fallback.
func sessionPayload(goal string) string {
	return `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","sessionId":"` + chatAttachmentSessionID + `","messages":[{"role":"user","content":"` + goal + `"}]}`
}

// allRequestText concatenates every message of a request so tests can
// assert on context content regardless of where the assembler landed it.
func allRequestText(req llmadapter.Request) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

// A coverage-aware summary store (P2-2) is consulted for the latest
// checkpoint on every turn, and a non-empty summary must survive context
// assembly into the outgoing request.
func TestChatStartInjectsCompactionCheckpointSummary(t *testing.T) {
	reader := &fakeCoverageSummaryReader{summary: "前情摘要：已定稿架构方案草稿。", end: 42}
	req := startChatWithEngine(t, sessionPayload("继续总结我们的方案"), func(e *Engine) {
		e.messageReader = emptyCompanionReader{}
		e.summaryReader = reader
	})
	if reader.calls == 0 {
		t.Fatalf("checkpoint reader never consulted")
	}
	if !strings.Contains(allRequestText(req), "前情摘要") {
		t.Fatalf("checkpoint summary missing from context; messages:\n%.600s", allRequestText(req))
	}
}

// Stores without coverage support keep the flat reader path and still
// assemble a normal request.
func TestChatStartUsesFlatSummaryReader(t *testing.T) {
	reader := &fakeFlatSummaryReader{summary: "旧摘要内容。"}
	req := startChatWithEngine(t, sessionPayload("继续总结我们的方案"), func(e *Engine) {
		e.messageReader = emptyCompanionReader{}
		e.summaryReader = reader
	})
	if reader.calls == 0 {
		t.Fatalf("flat summary reader never consulted")
	}
	if len(req.Messages) == 0 {
		t.Fatalf("no messages assembled")
	}
}

// A summary read error skips the checkpoint instead of failing the turn.
func TestChatStartSummaryErrorSkipsCheckpoint(t *testing.T) {
	reader := &fakeCoverageSummaryReader{err: errors.New("summary store unavailable")}
	req := startChatWithEngine(t, sessionPayload("继续总结我们的方案"), func(e *Engine) {
		e.messageReader = emptyCompanionReader{}
		e.summaryReader = reader
	})
	if reader.calls == 0 {
		t.Fatalf("reader should still be consulted before the error path")
	}
	if strings.Contains(allRequestText(req), "前情摘要") {
		t.Fatalf("failed summary read must not inject a checkpoint")
	}
}

// GLM-5.3 folds medium into high; junk levels clear out.
func TestNormalizeReasoningLevelFoldsMedium(t *testing.T) {
	if got := normalizeReasoningLevel("Medium "); got != "high" {
		t.Fatalf("medium must fold to high, got %q", got)
	}
	if got := normalizeReasoningLevel("MAX"); got != "max" {
		t.Fatalf("case-insensitive max, got %q", got)
	}
	if got := normalizeReasoningLevel("ultra"); got != "" {
		t.Fatalf("unknown level must clear, got %q", got)
	}
}

// A chips-style council turn must survive the full chat.start path with the
// council config wired (chat.go lane overlay block), producing an adapter
// request instead of failing the turn.
func TestChatStartCouncilTurnStreams(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "council-coverage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	svc := m8app.NewExpertService(store.AgentRuntimeRepository(), "local-user", &m8app.MemoryPersonaStore{})
	svc.SetSkillStore(store)
	ctx := context.Background()
	requests := make(chan llmadapter.Request, 4)
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	e.SetM8ExpertService(svc)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	_, _ = createNamedLocalExpert(t, e, ctx, "润色A", "req-cov-a"), createNamedLocalExpert(t, e, ctx, "润色B", "req-cov-b")
	turn := "[引用专家 润色A|01ARZ3NDEKTSV4RRFFQ69G5FAV][引用专家 润色B|01ARZ3NDEKTSV4RRFFQ69G5FAW] 把这段话润色得更顺：今天开会很顺利"
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","messages":[{"role":"user","content":"` + turn + `"}]}`
	response := e.HandleStreaming(ctx, validRequest("chat.start", payload), func(bridge.Event) error { return nil })
	if !response.OK {
		t.Fatalf("council chat.start failed: code=%s msg=%s", response.Error.Code, response.Error.Message)
	}
	req := capturedChatRequest(t, requests)
	if len(req.Messages) == 0 {
		t.Fatalf("council turn produced no adapter request")
	}
}
