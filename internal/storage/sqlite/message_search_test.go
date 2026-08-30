package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
)

func TestMessageFTSMatchQuotesTerms(t *testing.T) {
	if got := messageFTSMatch(`月汐 搜索`); got != `"月汐" OR "搜索"` {
		t.Fatalf("got %q", got)
	}
	if got := messageFTSMatch(`ab"cd`); got != `"ab""cd"` {
		t.Fatalf("embedded quote must double, got %q", got)
	}
	if messageFTSMatch("   ") != "" {
		t.Fatal("blank query must be empty MATCH")
	}
	if got := escapeLike(`a%b_c\d`); got != `a\%b\_c\\d` {
		t.Fatalf("like escape = %q", got)
	}
	long, short := partitionSearchTerms("月汐 搜索 compaction")
	if strings.Join(short, ",") != "月汐,搜索" || strings.Join(long, ",") != "compaction" {
		t.Fatalf("partition long=%v short=%v", long, short)
	}
}

func TestSearchMessagesIndexesAppendAndRewind(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "msg-fts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	projects := projectapp.New(store, store)
	parent, err := projects.Create(ctx, "fts-project", "test", nil, project.Project{Name: "FTS Project"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := projects.Create(ctx, "fts-other", "test", nil, project.Project{Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	sessions := sessionapp.New(store, store)
	chat, err := sessions.Create(ctx, "fts-session", "test", nil, session.Session{ProjectID: parent.ID, Title: "代码讨论"})
	if err != nil {
		t.Fatal(err)
	}
	weather, err := sessions.Create(ctx, "fts-weather", "test", nil, session.Session{ProjectID: parent.ID, Title: "上海天气"})
	if err != nil {
		t.Fatal(err)
	}
	alien, err := sessions.Create(ctx, "fts-alien", "test", nil, session.Session{ProjectID: other.ID, Title: "别的项目"})
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := messageapp.New(store, store, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := msgs.Append(ctx, "fts-a", "test", map[string]string{"t": "a"}, message.Message{SessionID: chat.ID, Text: "这里谈到了月汐功能"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = msgs.Append(ctx, "fts-b", "test", map[string]string{"t": "b"}, message.Message{SessionID: chat.ID, Text: "也谈到了历史搜索"}); err != nil {
		t.Fatal(err)
	}
	if _, err = msgs.Append(ctx, "fts-c", "test", map[string]string{"t": "c"}, message.Message{SessionID: weather.ID, Text: "明天多云"}); err != nil {
		t.Fatal(err)
	}
	if _, err = msgs.Append(ctx, "fts-d", "test", map[string]string{"t": "d"}, message.Message{SessionID: alien.ID, Text: "月汐功能在别的项目"}); err != nil {
		t.Fatal(err)
	}

	hits, err := store.SearchMessages(ctx, messageapp.SearchQuery{Query: "月汐 搜索", ProjectID: parent.ID, Limit: 32})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected FTS hits for 月汐/搜索")
	}
	for _, h := range hits {
		if h.SessionID != chat.ID {
			t.Fatalf("project filter leaked %s title=%s", h.SessionID, h.SessionTitle)
		}
		if h.SessionTitle != "代码讨论" {
			t.Fatalf("title = %q", h.SessionTitle)
		}
		if h.Snippet == "" {
			t.Fatal("empty snippet")
		}
	}

	trigram, err := store.SearchMessages(ctx, messageapp.SearchQuery{Query: "谈到了", ProjectID: parent.ID, Limit: 8})
	if err != nil || len(trigram) == 0 {
		t.Fatalf("3-rune trigram/LIKE must hit, err=%v n=%d", err, len(trigram))
	}

	only, err := store.SearchMessages(ctx, messageapp.SearchQuery{Query: "月汐", SessionID: chat.ID, Limit: 8})
	if err != nil || len(only) == 0 {
		t.Fatalf("session filter: %v n=%d", err, len(only))
	}
	if only[0].MessageID != first.ID && !strings.Contains(only[0].Snippet, "月汐") {
		t.Fatalf("expected 月汐 snippet, got %+v", only[0])
	}

	miss, err := store.SearchMessages(ctx, messageapp.SearchQuery{Query: "不存在的关键词xyz", ProjectID: parent.ID, Limit: 8})
	if err != nil || len(miss) != 0 {
		t.Fatalf("miss = %+v err=%v", miss, err)
	}

	if _, err := msgs.Rewind(ctx, "fts-rewind", "test", chat.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	after, err := store.SearchMessages(ctx, messageapp.SearchQuery{Query: "月汐", ProjectID: parent.ID, Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range after {
		if h.SessionID == chat.ID {
			t.Fatalf("rewind must drop FTS rows, still have %+v", h)
		}
	}
}
