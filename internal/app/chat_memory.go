package app

import (
	"context"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/domain/memory"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/m8app"
)

// Chat-side memory fusion budgets. Confirmed preferences keep the existing
// 8×2KiB system-instruction cap. Query-conditioned pinned facts and retrieved
// evidence sit in envelope slots so they participate in ADR-005 assembly
// instead of crowding the system prompt. Companion uses a tighter pinned
// budget and skips the evidence channel to protect first-spoken-token latency.
const (
	pinnedInjectMaxItems     = 6
	pinnedInjectMaxBytes     = 1536
	companionPinnedMaxItems  = 3
	companionPinnedMaxBytes  = 768
	evidenceInjectMaxItems   = 4
	evidenceInjectMaxBytes   = 3072
	workingInjectMaxItems    = 3
	workingInjectMaxBytes    = 1024
	autoNominateMinUserRunes = 8
	autoNominateMinAsstRunes = 40
	autoNominateMaxContent   = 1800
	memoryOpsSubject         = "local-user"
)

type chatMemoryRequest struct {
	Query     string
	SessionID string
	Companion bool
}

// chatMemoryPack is the fused inject payload for one chat.start.
//
//	Prefs        — M8 confirmed preferences (always-on system instruction)
//	Pinned       — query-conditioned confirmed facts + procedural memories
//	TaskState    — working-layer memories for the current project/session
//	Evidence     — episodic/semantic/ontology hits (untrusted, droppable)
//
// Candidates that were never confirmed never appear in any of these slots.
type chatMemoryPack struct {
	Prefs     []string
	Pinned    []contextapp.ContextSource
	TaskState []contextapp.ContextSource
	Evidence  []contextapp.ContextSource
	TraceID   string
	Enabled   bool
}

type sessionGetter interface {
	Get(context.Context, string) (session.Session, error)
}

func (e *Engine) prepareChatMemory(ctx context.Context, req chatMemoryRequest) chatMemoryPack {
	pack := chatMemoryPack{Enabled: true}
	if e == nil {
		return pack
	}
	settings := e.chatMemorySettings(ctx)
	pack.Enabled = settings.MemoryEnabled

	if e.m8memory != nil {
		if snapshot, err := e.m8memory.ConfirmedSnapshot(ctx, m8app.LearningScope, preferenceInjectMaxItems, preferenceInjectMaxBytes); err == nil {
			pack.Prefs = snapshot
		} else {
			log.Printf("chat memory: preference snapshot skipped: %v", err)
		}
	}
	if !pack.Enabled {
		return pack
	}

	query := strings.TrimSpace(req.Query)
	prefSet := make(map[string]struct{}, len(pack.Prefs))
	for _, p := range pack.Prefs {
		prefSet[p] = struct{}{}
	}

	pinnedMaxItems, pinnedMaxBytes := pinnedInjectMaxItems, pinnedInjectMaxBytes
	if req.Companion {
		pinnedMaxItems, pinnedMaxBytes = companionPinnedMaxItems, companionPinnedMaxBytes
	}

	if query != "" && e.m8memory != nil {
		res, err := e.m8memory.RecallForInject(ctx, m8app.RecallInput{
			ScopeID: m8app.LearningScope,
			Query:   clipRunes(query, 2048),
			TopK:    pinnedMaxItems,
		})
		if err != nil {
			log.Printf("chat memory: recall inject skipped: %v", err)
		} else {
			pack.TraceID = res.TraceID
			used := 0
			for _, hit := range res.Hits {
				content := strings.TrimSpace(hit.Content)
				if content == "" {
					continue
				}
				if _, dup := prefSet[content]; dup {
					continue
				}
				if len(pack.Pinned) >= pinnedMaxItems || used+len(content) > pinnedMaxBytes {
					break
				}
				pack.Pinned = append(pack.Pinned, contextapp.ContextSource{
					Type:       contextapp.SourcePinnedFacts,
					ID:         hit.Source,
					Authority:  contextapp.AuthorityPinned,
					Content:    content,
					Provenance: "m8:confirmed:" + hit.Source,
				})
				prefSet[content] = struct{}{}
				used += len(content)
			}
		}
	}

	projectID := e.projectIDForSession(ctx, req.SessionID)
	if projectID == "" || !memoryServiceAvailable(e.memories) {
		return pack
	}

	working, err := e.memories.ListByProject(ctx, projectID, memory.LayerWorking)
	if err != nil {
		log.Printf("chat memory: working-layer list skipped: %v", err)
	} else {
		pack.TaskState = clipMemorySources(workingToSources(working, req.SessionID), workingInjectMaxItems, workingInjectMaxBytes)
	}

	if query == "" {
		return pack
	}
	hits, err := e.memories.Search(ctx, projectID, query)
	if err != nil {
		log.Printf("chat memory: domain search skipped: %v", err)
		return pack
	}

	pinnedUsed := sourcesBytes(pack.Pinned)
	var evidence []contextapp.ContextSource
	evidenceUsed := 0
	for _, item := range hits {
		content := strings.TrimSpace(item.Key + ": " + item.Content)
		if content == "" || content == ": " {
			continue
		}
		if _, dup := prefSet[strings.TrimSpace(item.Content)]; dup {
			continue
		}
		src := contextapp.ContextSource{
			ID:         item.ID,
			Content:    clipRunes(content, 512),
			Provenance: "memory:" + string(item.Layer) + ":" + item.ID,
		}
		switch item.Layer {
		case memory.LayerProcedural:
			if len(pack.Pinned) >= pinnedMaxItems || pinnedUsed+len(src.Content) > pinnedMaxBytes {
				continue
			}
			src.Type = contextapp.SourcePinnedFacts
			src.Authority = contextapp.AuthorityPinned
			pack.Pinned = append(pack.Pinned, src)
			prefSet[strings.TrimSpace(item.Content)] = struct{}{}
			pinnedUsed += len(src.Content)
		case memory.LayerEpisodic, memory.LayerSemantic:
			if req.Companion {
				continue
			}
			if len(evidence) >= evidenceInjectMaxItems || evidenceUsed+len(src.Content) > evidenceInjectMaxBytes {
				continue
			}
			src.Type = contextapp.SourceRetrievedEvidence
			src.Authority = contextapp.AuthorityEvidence
			evidence = append(evidence, src)
			evidenceUsed += len(src.Content)
		}
	}
	if !req.Companion {
		pack.Evidence = append(pack.Evidence, evidence...)
		if ontologyServiceAvailable(e.ontology) {
			if nodes, oerr := e.ontology.SearchNodes(ctx, projectID, query); oerr != nil {
				log.Printf("chat memory: ontology search skipped: %v", oerr)
			} else {
				for _, n := range nodes {
					if len(pack.Evidence) >= evidenceInjectMaxItems {
						break
					}
					label := strings.TrimSpace(n.Name)
					if n.Description != "" {
						label = label + " — " + strings.TrimSpace(n.Description)
					}
					if label == "" {
						continue
					}
					content := clipRunes("本体："+label, 256)
					if evidenceUsed+len(content) > evidenceInjectMaxBytes {
						break
					}
					pack.Evidence = append(pack.Evidence, contextapp.ContextSource{
						Type:       contextapp.SourceRetrievedEvidence,
						ID:         n.ID,
						Authority:  contextapp.AuthorityEvidence,
						Content:    content,
						Provenance: "ontology:" + n.ID,
					})
					evidenceUsed += len(content)
				}
			}
		}
	}
	return pack
}

func (e *Engine) chatMemorySettings(ctx context.Context) m8core.MemorySettings {
	defaults := m8core.DefaultMemorySettings(memoryOpsSubject)
	if e == nil || e.memoryOps == nil {
		return defaults
	}
	st, err := e.memoryOps.SettingsGet(ctx, memoryOpsSubject)
	if err != nil {
		log.Printf("chat memory: settings read skipped: %v", err)
		return defaults
	}
	return st
}

func (e *Engine) projectIDForSession(ctx context.Context, sessionID string) string {
	if e == nil || sessionID == "" || e.sessions == nil {
		return ""
	}
	g, ok := e.sessions.(sessionGetter)
	if !ok {
		return ""
	}
	item, err := g.Get(ctx, sessionID)
	if err != nil {
		return ""
	}
	return item.ProjectID
}

func applyChatMemoryPack(env *contextapp.ContextEnvelope, pack chatMemoryPack) {
	if env == nil {
		return
	}
	if len(pack.Pinned) > 0 {
		env.PinnedFacts = append(env.PinnedFacts, pack.Pinned...)
	}
	if len(pack.TaskState) > 0 {
		env.TaskState = append(env.TaskState, pack.TaskState...)
	}
	if len(pack.Evidence) > 0 {
		env.RelatedEvidence = append(env.RelatedEvidence, pack.Evidence...)
	}
}

func workingToSources(items []memory.Memory, sessionID string) []contextapp.ContextSource {
	out := make([]contextapp.ContextSource, 0, len(items))
	for _, item := range items {
		if item.ExpiresAt != nil && time.Now().UTC().After(*item.ExpiresAt) {
			continue
		}
		if item.Scope == memory.ScopeSession && sessionID != "" && item.SourceID != nil && *item.SourceID != sessionID {
			continue
		}
		content := strings.TrimSpace(item.Key + ": " + item.Content)
		if content == "" || content == ": " {
			continue
		}
		out = append(out, contextapp.ContextSource{
			Type:       contextapp.SourceTaskState,
			ID:         item.ID,
			Authority:  contextapp.AuthorityWorkspace,
			Content:    clipRunes(content, 400),
			Provenance: "memory:working:" + item.ID,
		})
	}
	return out
}

func clipMemorySources(items []contextapp.ContextSource, maxItems, maxBytes int) []contextapp.ContextSource {
	if maxItems < 1 || maxBytes < 1 {
		return nil
	}
	out := make([]contextapp.ContextSource, 0, maxItems)
	used := 0
	for _, item := range items {
		n := len(item.Content)
		if n == 0 || used+n > maxBytes || len(out) >= maxItems {
			continue
		}
		out = append(out, item)
		used += n
	}
	return out
}

func sourcesBytes(items []contextapp.ContextSource) int {
	n := 0
	for _, item := range items {
		n += len(item.Content)
	}
	return n
}

func clipRunes(s string, max int) string {
	if max < 1 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}

func renderPreferenceInstruction(instruction string, prefs []string) string {
	if len(prefs) == 0 {
		return instruction
	}
	var b strings.Builder
	b.WriteString(instruction)
	b.WriteString("\n\n以下为用户已显式确认的偏好，回答时必须遵守：\n")
	for _, pref := range prefs {
		b.WriteString("- ")
		b.WriteString(pref)
		b.WriteString("\n")
	}
	return b.String()
}

func (e *Engine) maybeAutoNominateTurn(ctx context.Context, sessionID, userText, assistantText, messageID string, companion bool) error {
	if e == nil || companion || sessionID == "" || messageID == "" {
		return nil
	}
	settings := e.chatMemorySettings(ctx)
	if !settings.MemoryEnabled || !settings.AutoNominate {
		return nil
	}
	userText = strings.TrimSpace(userText)
	assistantText = strings.TrimSpace(assistantText)
	if utf8.RuneCountInString(userText) < autoNominateMinUserRunes || utf8.RuneCountInString(assistantText) < autoNominateMinAsstRunes {
		return nil
	}
	gist := clipRunes("用户："+userText+"\n要点："+assistantText, autoNominateMaxContent)
	if e.m8memory != nil {
		pending, err := e.m8memory.ListPendingCandidates(ctx, 20)
		if err == nil {
			for _, item := range pending {
				if item.Content == gist {
					return nil
				}
			}
		}
	}
	doc := m8core.PayloadDoc{
		Content:     gist,
		ScopeID:     m8app.LearningScope,
		Sensitivity: m8core.SensPrivate,
		Leaves: []m8core.SourceLeafClaim{{
			JSONPointer: "/content",
			EvidenceRef: clipRunes("chat://"+sessionID+"/"+messageID, m8core.MaxEvidenceRef),
			Digest:      m8core.DigestOf(gist),
		}},
	}
	if e.m10nomination != nil {
		_, err := e.m10nomination.Nominate(ctx, m8app.NominateInput{
			SubjectID:       memoryOpsSubject,
			Doc:             doc,
			Reason:          "本轮对话自动提名",
			Nominator:       "chat.auto",
			SourceSessionID: sessionID,
			Actor:           "engine",
		})
		if err != nil {
			log.Printf("chat memory: auto-nominate skipped: %v", err)
		}
		return err
	}
	if e.m8memory == nil {
		return nil
	}
	_, err := e.m8memory.ProposeCandidate(ctx, m8app.ProposeInput{
		SubjectID: memoryOpsSubject,
		Doc:       doc,
		Inferred:  true,
		Trust:     m8core.TrustUntrusted,
		Actor:     "engine",
	})
	if err != nil {
		log.Printf("chat memory: auto-propose skipped: %v", err)
	}
	return err
}
