package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/meetings"
)

// pacedNotesAdapter emits the notes JSON in pieces, spaced far enough apart to
// clear the parse throttle, the way a real slow generation arrives.
type pacedNotesAdapter struct {
	deltas    []string
	gap       time.Duration
	streamErr error
	complete  string
}

func (a pacedNotesAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	if a.complete == "" {
		return llmadapter.Response{}, errors.New("complete not configured")
	}
	return llmadapter.Response{Message: llmadapter.Message{Content: a.complete}}, nil
}

func (a pacedNotesAdapter) Stream(_ context.Context, _ []byte, _ llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	if a.streamErr != nil {
		return llmadapter.Response{}, a.streamErr
	}
	var whole strings.Builder
	for i, piece := range a.deltas {
		if i > 0 {
			time.Sleep(a.gap)
		}
		whole.WriteString(piece)
		if emit != nil {
			if err := emit(llmadapter.Delta{Text: piece}); err != nil {
				return llmadapter.Response{}, err
			}
		}
	}
	return llmadapter.Response{Message: llmadapter.Message{Content: whole.String()}}, nil
}

func (pacedNotesAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, nil
}

// The whole reason notes stream: a finished topic must reach the reader before the
// closing brace. If this regresses to a blocking Complete, the page shows a
// spinner for the entire generation and reads as a hang.
func TestCompleteMeetingPublishesTopicsWhileStreaming(t *testing.T) {
	e := NewEngineWithGateway(meetingNotesProvider{}, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return pacedNotesAdapter{
			gap: notesStreamParseInterval + 120*time.Millisecond,
			deltas: []string{
				`{"title":"上线评审","background":"对齐范围","topics":[{"heading":"范围","points":["只做浏览器包"]},`,
				`{"heading":"排期","points":["下周三上线"]}],"decisions":["先发浏览器包"]}`,
			},
		}, nil
	})
	var mu sync.Mutex
	var published []string
	ctx := meetings.WithInterimPublisher(context.Background(), func(n meetings.Notes) {
		mu.Lock()
		defer mu.Unlock()
		published = append(published, n.Summary)
	})
	notes, err := e.completeMeeting(ctx, "周会", "先对齐范围，下周三上线")
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(published) == 0 {
		t.Fatal("nothing was published mid-stream; the reader waits for the whole document")
	}
	first := published[0]
	if !strings.Contains(first, "只做浏览器包") {
		t.Fatalf("first publish missed the finished topic:\n%s", first)
	}
	if strings.Contains(first, "下周三上线") {
		t.Fatalf("first publish leaked a topic that had not arrived yet:\n%s", first)
	}
	if !strings.Contains(notes.Summary, "下周三上线") {
		t.Fatalf("final notes lost the last topic:\n%s", notes.Summary)
	}
}

// A provider whose stream dies must still produce notes, or switching the order
// would trade a slow document for no document.
func TestCompleteMeetingUsesCompleteWhenStreamFails(t *testing.T) {
	e := NewEngineWithGateway(meetingNotesProvider{}, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return pacedNotesAdapter{
			streamErr: errors.New("stream unsupported"),
			complete:  `{"title":"评审","topics":[{"heading":"范围","points":["只做浏览器包"]}],"actions":[{"owner":"张三","task":"写纪要"}]}`,
		}, nil
	})
	notes, err := e.completeMeeting(context.Background(), "周会", "先对齐范围")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notes.Summary, "只做浏览器包") || !strings.Contains(notes.Actions, "写纪要") {
		t.Fatalf("complete fallback notes = %#v", notes)
	}
}

// Publishing must be optional: every other caller of completeMeeting passes a
// plain context, and a missing publisher cannot be a nil dereference.
func TestCompleteMeetingWithoutPublisherStillWorks(t *testing.T) {
	e := NewEngineWithGateway(meetingNotesProvider{}, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return pacedNotesAdapter{
			gap:    notesStreamParseInterval + 120*time.Millisecond,
			deltas: []string{`{"title":"评审","topics":[{"heading":"范围","points":["只做浏览器包"]}`, `]}`},
		}, nil
	})
	notes, err := e.completeMeeting(context.Background(), "周会", "先对齐范围")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notes.Summary, "只做浏览器包") {
		t.Fatalf("notes = %#v", notes)
	}
}
