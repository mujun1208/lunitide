package app

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

const (
	memoryHygieneMinInterval = 6 * time.Hour
	memoryHygieneIdleAfter   = 30 * time.Minute
	memoryHygieneExpireAfter = 30 * 24 * time.Hour
	memoryFlushReason        = "压缩前整理，确认后才进长期记忆"
)

type memoryHygieneResult struct {
	Seen      int
	Withdrawn int
	Deduped   int
}

type memoryNominationAPI interface {
	Nominate(context.Context, m8app.NominateInput) (m8app.NominateResult, error)
	ListNominationsFor(context.Context, string, string, int) ([]m8app.NominationView, error)
	WithdrawFor(context.Context, string, string, string) error
}

var (
	hygieneMu          sync.Mutex
	hygieneLastRun     time.Time
	lastEngineActivity time.Time
)

func (e *Engine) noteEngineActivityAndMaybeHygiene(ctx context.Context) {
	if e == nil {
		return
	}
	hygieneMu.Lock()
	idle := !lastEngineActivity.IsZero() && time.Since(lastEngineActivity) >= memoryHygieneIdleAfter
	lastEngineActivity = time.Now()
	hygieneMu.Unlock()
	if idle {
		_ = e.runMemoryHygieneThrottled(ctx)
	}
}

func (e *Engine) runMemoryHygieneThrottled(ctx context.Context) memoryHygieneResult {
	hygieneMu.Lock()
	if !hygieneLastRun.IsZero() && time.Since(hygieneLastRun) < memoryHygieneMinInterval {
		hygieneMu.Unlock()
		return memoryHygieneResult{}
	}
	hygieneLastRun = time.Now()
	hygieneMu.Unlock()
	return e.runMemoryHygiene(ctx)
}

func (e *Engine) nominationAPI() memoryNominationAPI {
	if e == nil || e.m10nomination == nil {
		return nil
	}
	return e.m10nomination
}

func (e *Engine) runMemoryHygiene(ctx context.Context) memoryHygieneResult {
	subject := ""
	if e != nil {
		subject = e.memorySubjectID()
	}
	return runMemoryHygieneOn(ctx, e.nominationAPI(), subject)
}

func runMemoryHygieneOn(ctx context.Context, api memoryNominationAPI, subject string) (out memoryHygieneResult) {
	if api == nil {
		return out
	}
	listed, err := api.ListNominationsFor(ctx, subject, m8core.NomNominated, 100)
	if err != nil {
		log.Printf("memory hygiene list skipped: %v", err)
		return out
	}
	seen := map[string]string{}
	now := time.Now()
	for _, item := range listed {
		out.Seen++
		key := strings.ToLower(strings.TrimSpace(item.Content))
		if key == "" {
			key = item.NominationID
		}
		if created, parseErr := time.Parse(time.RFC3339, item.CreatedAt); parseErr == nil && now.Sub(created) > memoryHygieneExpireAfter {
			if err := api.WithdrawFor(ctx, subject, item.NominationID, "hygiene"); err == nil {
				out.Withdrawn++
			}
			continue
		}
		if prev, ok := seen[key]; ok && prev != "" && prev != item.NominationID {
			if err := api.WithdrawFor(ctx, subject, item.NominationID, "hygiene"); err == nil {
				out.Deduped++
			}
			continue
		}
		seen[key] = item.NominationID
	}
	return out
}

func (e *Engine) flushMemoryBeforeCompaction(ctx context.Context, sessionID, userText, assistantText string) {
	if e == nil {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("memory flush recovered: %v", rec)
		}
	}()
	_ = e.runMemoryHygiene(ctx)
	content := strings.TrimSpace(userText)
	if assistantText = strings.TrimSpace(assistantText); assistantText != "" {
		if content != "" {
			content = "用户：" + content + "\n要点：" + assistantText
		} else {
			content = assistantText
		}
	}
	content = clipRunes(content, autoNominateMaxContent)
	api := e.nominationAPI()
	if content == "" || api == nil || sessionID == "" {
		return
	}
	_, err := api.Nominate(ctx, m8app.NominateInput{
		SubjectID: e.memorySubjectID(),
		Doc: m8core.PayloadDoc{
			Content:     content,
			ScopeID:     m8app.LearningScope,
			Sensitivity: m8core.SensPrivate,
			Leaves: []m8core.SourceLeafClaim{{
				JSONPointer: "/content",
				EvidenceRef: "compaction-flush://" + sessionID,
				Digest:      m8core.DigestOf(content),
			}},
		},
		Reason:          memoryFlushReason,
		Nominator:       "compaction-flush",
		SourceSessionID: sessionID,
		Actor:           "engine",
	})
	if err != nil {
		log.Printf("memory flush nominate skipped: %v", err)
	}
}
