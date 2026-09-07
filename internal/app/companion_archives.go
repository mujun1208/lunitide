package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/compactionapp"
	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/compaction"
	"github.com/lunitide/lunitide/internal/domain/provider"
)

type companionArchiveState struct {
	store compactionapp.CompanionArchiveStore
	mu    sync.Mutex
}

func (e *Engine) SetCompanionArchiveStore(store compactionapp.CompanionArchiveStore) {
	e.companionArchives.store = store
}

// RunCompanionArchives is also called after restart: missed weeks are read from
// durable source/coverage, never from a process-local last-run timestamp.
func (e *Engine) RunCompanionArchives(ctx context.Context, now time.Time) error {
	a := &e.companionArchives
	if a.store == nil || e.compactionTrigger == nil || e.compactionExecutor == nil || e.providers == nil {
		return nil
	}
	if e.memoryOps != nil && !e.chatMemorySettings(ctx).MemoryEnabled {
		return nil
	}
	if !a.mu.TryLock() {
		return nil
	}
	defer a.mu.Unlock()
	providers, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return err
	}
	var providerID, modelID string
	for _, p := range providers {
		if p.Status != provider.StatusEnabled || p.CredentialState != provider.CredentialConfigured {
			continue
		}
		for _, m := range p.Models {
			if m.EffectiveKind() == provider.KindLLM {
				providerID = p.ID
				modelID = m.ModelID
				if m.IsDefault || m.KindDefault {
					break
				}
			}
		}
		if modelID != "" {
			break
		}
	}
	if providerID == "" || modelID == "" {
		return nil
	} // Pending source remains eligible.
	// Bound catch-up per sweep; an absent provider or failed summary never
	// advances coverage. The next hourly sweep retries from the durable range.
	for pass := 0; pass < 8; pass++ {
		weeks, err := a.store.ListDueCompanionWeeks(ctx, now)
		if err != nil {
			return err
		}
		progress := false
		for _, week := range weeks {
			raw, _ := json.Marshal(map[string]string{"sessionId": week.SessionID})
			release, scopeErr := e.authorizeDataRequest(ctx, "session.get", raw)
			if scopeErr != nil {
				continue
			}
			completed, runErr := e.archiveCompanionWeek(ctx, week, providerID, modelID)
			release()
			if runErr != nil {
				return runErr
			}
			progress = progress || completed
		}
		if !progress {
			return nil
		}
	}
	return nil
}

func (e *Engine) archiveCompanionWeek(ctx context.Context, week compactionapp.CompanionWeek, providerID, modelID string) (bool, error) {
	triggered, err := e.compactionTrigger.TriggerCompanionWeek(ctx, week, providerID, modelID)
	if err != nil || !triggered.Triggered {
		return false, err
	}
	result, err := e.compactionExecutor.Execute(ctx, triggered.CheckpointID)
	if err != nil {
		return false, err
	}
	if result.Status != compaction.StatusSucceeded {
		return false, fmt.Errorf("weekly companion archive did not complete: %s", result.Status)
	}
	// A weekly memory is complete when its validated summary is durable. It
	// is retrieved on demand, never installed as the active conversation context.
	// This also leaves any manually selected compaction checkpoint unchanged.
	return true, nil
}

func (e *Engine) StartCompanionArchives(parent context.Context) func() {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			runCtx, stop := context.WithTimeout(ctx, 10*time.Minute)
			if err := e.RunCompanionArchives(runCtx, time.Now()); err != nil && ctx.Err() == nil {
				log.Printf("companion weekly archive: %v", err)
			}
			stop()
			timer.Reset(time.Hour)
		}
	}()
	return func() { cancel(); <-done }
}

func (e *Engine) companionArchiveEvidence(ctx context.Context, sessionID, query string) []contextapp.ContextSource {
	if e.companionArchives.store == nil || !wantsCompanionArchiveRecall(query) {
		return nil
	}
	if e.memoryOps != nil && !e.chatMemorySettings(ctx).MemoryEnabled {
		return nil
	}
	archives, err := e.companionArchives.store.SearchCompanionArchives(ctx, sessionID, query, 3)
	if err != nil {
		log.Printf("companion archive recall: %v", err)
		return nil
	}
	var out []contextapp.ContextSource
	for _, a := range archives {
		out = append(out, contextapp.ContextSource{Type: contextapp.SourceCompactionSummary, ID: a.CheckpointID, Authority: contextapp.AuthorityEvidence, Content: "月伴每周归档（" + a.Period + " 起），仅作历史回忆依据：\n" + a.Summary, Provenance: "session:" + sessionID + ":checkpoint:" + a.CheckpointID})
	}
	return out
}

func wantsCompanionArchiveRecall(query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	for _, hint := range []string{"还记得", "记不记得", "你记得", "归档", "回忆", "上次", "之前", "以前", "上周", "上个月", "历史", "我说过", "聊过", "提过", "记忆", "remember", "recall", "last week", "last time", "archive", "we discussed"} {
		if strings.Contains(q, hint) {
			return true
		}
	}
	return false
}
