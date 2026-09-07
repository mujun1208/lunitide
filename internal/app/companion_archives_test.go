package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/compactionapp"
	"github.com/lunitide/lunitide/internal/messageapp"
)

type weeklySummaryFixture struct {
	fail  bool
	calls int
}

func (s *weeklySummaryFixture) Summarize(_ context.Context, _, _, _ string, _, _ int64, messages []compactionapp.SummaryMessage, _ string) (string, string, error) {
	s.calls++
	if s.fail {
		return "", "", errors.New("provider unavailable")
	}
	var body []string
	for _, m := range messages {
		body = append(body, m.Content)
	}
	summary := strings.Join(body, "；")
	raw, _ := json.Marshal(map[string]any{"summary": summary, "keyPoints": body, "actionItems": []string{}})
	return string(raw), summary, nil
}

func TestCompanionWeeklyArchiveFailureRetryRecallAndPreserveOriginals(t *testing.T) {
	ctx := context.Background()
	e, id, store := agentRunEngine(t)
	r := validRequest("session.update", fmt.Sprintf(`{"id":%q,"title":"月伴对话","version":1}`, id))
	r.IdempotencyKey = "weekly-title"
	if response := e.Handle(ctx, r); !response.OK {
		t.Fatalf("title: %+v", response.Error)
	}
	var err error
	e.messages, err = messageapp.New(store, store, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	for i, text := range []string{"周末去图书馆", "喜欢科幻小说"} {
		r := validRequest("message.append", fmt.Sprintf(`{"sessionId":%q,"text":%q}`, id, text))
		r.IdempotencyKey = fmt.Sprintf("weekly-message-%d", i)
		if response := e.Handle(ctx, r); !response.OK {
			t.Fatal(response.Error)
		}
	}
	e.providers = compactionProviderService{}
	summarizer := &weeklySummaryFixture{fail: true}
	wire := func() {
		e.SetCompactionServices(compactionapp.NewTrigger(compactionapp.DefaultWatermarkConfig(), store, store, store.CompactionMessageReader()), compactionapp.NewExecutor(store, store, summarizer), store)
		e.SetCompanionArchiveStore(store)
	}
	wire()
	nextWeek := compactionapp.WeekStart(time.Now()).AddDate(0, 0, 7)
	if err := e.RunCompanionArchives(ctx, nextWeek.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if summarizer.calls != 0 {
		t.Fatal("current week archived prematurely")
	}
	if err := e.RunCompanionArchives(ctx, nextWeek); err == nil {
		t.Fatal("failed summary reported success")
	}
	if got, err := store.SearchCompanionArchives(ctx, id, "图书馆", 3); err != nil || len(got) != 0 {
		t.Fatalf("failed draft leaked as memory: %+v %v", got, err)
	}
	summarizer.fail = false
	wire() // A restarted worker must rediscover failed source.
	if err := e.RunCompanionArchives(ctx, nextWeek); err != nil {
		t.Fatal(err)
	}
	if summarizer.calls != 2 {
		t.Fatalf("weekly attempts = %d", summarizer.calls)
	}
	if err := e.RunCompanionArchives(ctx, nextWeek.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if summarizer.calls != 2 {
		t.Fatal("already archived week summarized twice")
	}
	recalled := e.companionArchiveEvidence(ctx, id, "还记得我说的图书馆吗")
	if len(recalled) != 1 || !strings.Contains(recalled[0].Content, "图书馆") || !strings.Contains(recalled[0].Provenance, id) {
		t.Fatalf("history not recalled: %+v", recalled)
	}
	for _, greeting := range []string{"", "你好", "今天天气怎么样", "给我讲个笑话"} {
		if got := e.companionArchiveEvidence(ctx, id, greeting); len(got) != 0 {
			t.Fatalf("unsolicited history on %q: %+v", greeting, got)
		}
	}
	if active, err := store.GetLatestCompactionSummary(ctx, id); err != nil || active != "" {
		t.Fatalf("weekly archive replaced active conversation context: %q %v", active, err)
	}
	if got := e.companionArchiveEvidence(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "图书馆"); len(got) != 0 {
		t.Fatal("another session read this archive")
	}
	if rows, err := store.ListMessagesByRange(ctx, id, 1, 2); err != nil || len(rows) != 2 {
		t.Fatalf("archive removed originals: %+v %v", rows, err)
	}
}
