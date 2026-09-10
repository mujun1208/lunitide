package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
)

type compactionCompleteAdapter struct{}

func (compactionCompleteAdapter) Complete(_ context.Context, _ []byte, _ llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{
		Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: `{"summary":"ok","keyPoints":[],"actionItems":[]}`},
		Usage:   llmadapter.Usage{InputTokens: 4, OutputTokens: 2, TotalTokens: 6},
	}, nil
}

func (compactionCompleteAdapter) Stream(context.Context, []byte, llmadapter.Request, func(llmadapter.Delta) error) (llmadapter.Response, error) {
	return llmadapter.Response{}, nil
}

func (compactionCompleteAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, nil
}

func TestCompactionWiringRecordsCompactionPurpose(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.OpenTemplated(ctx, filepath.Join(t.TempDir(), "compaction-purpose.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	e := NewEngineWithContextReader(compactionProviderService{}, nil, nil, nil, chatAttachmentReader{}, &compactionTokenLookup{}, "test", streamTestLease{})
	calls := &memCalls{}
	e.SetCallAttemptStore(calls)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return compactionCompleteAdapter{}, nil
	})
	e.SetupCompactionServices(db, db.CompactionMessageReader())
	if e.compactionExecutor == nil {
		t.Fatal("SetupCompactionServices must wire the executor")
	}

	a, err := (&compactionAdapterFactory{e: e}).Adapter(ctx, compactionTestProvider())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Complete(ctx, []byte("secret"), llmadapter.Request{Model: "model"}); err != nil {
		t.Fatal(err)
	}

	compaction := 0
	for _, rec := range calls.recs {
		switch rec.Purpose {
		case "compaction":
			compaction++
		case "plan", "judge":
			t.Fatalf("compaction complete must not reuse plan/judge purpose: %+v", purposesOf(calls))
		}
	}
	if compaction < 1 {
		t.Fatalf("SetupCompactionServices Complete must record purpose compaction: %+v", purposesOf(calls))
	}
}
