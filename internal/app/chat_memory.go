package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/domain/memory"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

// Chat-side memory fusion budgets. Confirmed preferences keep the existing
// 8×2KiB system-instruction cap. Query-conditioned pinned facts and retrieved
// evidence sit in envelope slots so they participate in ADR-005 assembly
// instead of crowding the system prompt. Companion uses a tighter pinned
// budget and skips the evidence channel to protect first-spoken-token latency.
const (
	pinnedInjectMaxItems       = 6
	pinnedInjectMaxBytes       = 1536
	companionPinnedMaxItems    = 3
	companionPinnedMaxBytes    = 768
	evidenceInjectMaxItems     = 4
	evidenceInjectMaxBytes     = 3072
	workingInjectMaxItems      = 3
	workingInjectMaxBytes      = 1024
	autoNominateMinUserRunes   = 8
	autoNominateMinAsstRunes   = 40
	preferenceMinAsstRunes     = 2
	autoNominateMaxContent     = 1800
	preferenceNominationReason = "用户声明的偏好，确认后进长期记忆"
	memoryOpsLegacySubject     = "local-user"
	expertLastNominationReason = "专家本轮摘要，确认后进长期记忆"
	sessionLastMemoryKey       = "session:last"
)

type chatMemoryRequest struct {
	Query     string
	SessionID string
	Companion bool
	ExpertIDs []string
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
		if snapshot, err := e.m8memory.ConfirmedSnapshotFor(ctx, e.memorySubjectID(), m8app.LearningScope, preferenceInjectMaxItems, preferenceInjectMaxBytes); err == nil {
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
			ScopeID:   m8app.LearningScope,
			Query:     clipRunes(query, 2048),
			TopK:      pinnedMaxItems,
			SubjectID: e.memorySubjectID(),
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
		working = isolateExpertMemories(working, req.ExpertIDs)
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

	hits = isolateExpertMemories(hits, req.ExpertIDs)
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

func (e *Engine) memorySubjectID() string {
	if e != nil && e.identity != nil {
		if id := strings.TrimSpace(e.identity.SubjectID()); id != "" {
			return id
		}
	}
	return memoryOpsLegacySubject
}

func (e *Engine) ensureChatMemorySubject(ctx context.Context) string {
	id := e.memorySubjectID()
	if e == nil || e.identity == nil {
		return id
	}
	e.memorySubjectOnce.Do(func() {
		if err := e.identity.RebindLegacy(ctx); err != nil {
			log.Printf("chat memory: rebind local-user skipped: %v", err)
		}
	})
	return id
}

func (e *Engine) chatMemorySettings(ctx context.Context) m8core.MemorySettings {
	subject := e.ensureChatMemorySubject(ctx)
	defaults := m8core.DefaultMemorySettings(subject)
	if e == nil || e.memoryOps == nil {
		return defaults
	}
	st, err := e.memoryOps.SettingsGet(ctx, subject)
	if err != nil {
		log.Printf("chat memory: settings read skipped: %v", err)
		return defaults
	}
	return st
}

func (e *Engine) peopleLocalBrainMemoryHint(ctx context.Context, sessionID, userText string, expertIDs ...string) string {
	if sessionID == "" {
		return ""
	}
	// Colleague / local-brain turns are not voice TTFT: use the session pack
	// (full pinned + evidence), not the companion-tight budget.
	pack := e.prepareChatMemory(ctx, chatMemoryRequest{Query: userText, SessionID: sessionID, Companion: false, ExpertIDs: expertIDs})
	return renderPeopleMemorySlots(renderPreferenceInstruction("", pack.Prefs), pack.Pinned, pack.TaskState, nil)
}

func (e *Engine) peopleCompanionMemoryHint(ctx context.Context, sessionID, userText string, expertIDs ...string) string {
	if sessionID == "" {
		return ""
	}
	pack := e.prepareChatMemory(ctx, chatMemoryRequest{Query: userText, SessionID: sessionID, Companion: false, ExpertIDs: expertIDs})
	return renderPeopleMemorySlots(renderPreferenceInstruction("", pack.Prefs), pack.Pinned, pack.TaskState, pack.Evidence)
}

func renderPeopleMemorySlots(prefs string, pinned, taskState, evidence []contextapp.ContextSource) string {
	var b strings.Builder
	b.WriteString(prefs)
	write := func(title string, items []contextapp.ContextSource) {
		started := false
		for _, item := range items {
			content := strings.TrimSpace(item.Content)
			if content == "" {
				continue
			}
			if !started {
				b.WriteString("\n[")
				b.WriteString(title)
				b.WriteString("]\n")
				started = true
			}
			b.WriteString("- ")
			b.WriteString(content)
			b.WriteString("\n")
		}
	}
	write("置顶记忆", pinned)
	write("工作记忆", taskState)
	write("相关证据", evidence)
	return b.String()
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

func isolateExpertMemories(items []memory.Memory, expertIDs []string) []memory.Memory {
	if len(items) == 0 {
		return items
	}
	out := make([]memory.Memory, 0, len(items))
	for _, item := range items {
		if strings.HasPrefix(strings.TrimSpace(item.Key), "expert:") {
			if len(expertIDs) == 0 || !memoryOwnedByExperts(item.Key, expertIDs) {
				continue
			}
		}
		out = append(out, item)
	}
	return preferExpertOwnedMemories(out, expertIDs)
}

func preferExpertOwnedMemories(items []memory.Memory, expertIDs []string) []memory.Memory {
	if len(items) == 0 || len(expertIDs) == 0 {
		return items
	}
	owned, rest := make([]memory.Memory, 0, len(items)), make([]memory.Memory, 0, len(items))
	for _, item := range items {
		if memoryOwnedByExperts(item.Key, expertIDs) {
			owned = append(owned, item)
			continue
		}
		rest = append(rest, item)
	}
	return append(owned, rest...)
}

func memoryOwnedByExperts(key string, expertIDs []string) bool {
	key = strings.TrimSpace(key)
	for _, id := range expertIDs {
		id = strings.TrimSpace(id)
		if id != "" && strings.HasPrefix(key, "expert:"+id+":") {
			return true
		}
	}
	return false
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
		content := formatWorkingMemoryContent(item)
		if content == "" {
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

func formatWorkingMemoryContent(item memory.Memory) string {
	key := strings.TrimSpace(item.Key)
	content := strings.TrimSpace(key + ": " + strings.TrimSpace(item.Content))
	if content == "" || content == ": " {
		return ""
	}
	if key == sessionLastMemoryKey || (strings.HasPrefix(key, "expert:") && strings.HasSuffix(key, ":last")) {
		return "未确认工作摘要（确认后才进长期记忆）：" + content
	}
	return content
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

func (e *Engine) invokeMemorySearch(ctx context.Context, raw json.RawMessage) (toolruntime.Result, error) {
	if e == nil || e.m8memory == nil {
		return toolruntime.Result{}, errors.New("confirmed memory is not available")
	}
	var a struct {
		Query string `json:"query"`
		Max   int    `json:"max"`
	}
	if json.Unmarshal(raw, &a) != nil || strings.TrimSpace(a.Query) == "" {
		return toolruntime.Result{}, errors.New("memory.search needs query")
	}
	max := a.Max
	if max < 1 {
		max = 6
	}
	if max > 12 {
		max = 12
	}
	res, err := e.m8memory.RecallForInject(ctx, m8app.RecallInput{
		ScopeID:   m8app.LearningScope,
		Query:     clipRunes(strings.TrimSpace(a.Query), 2048),
		TopK:      max,
		SubjectID: e.memorySubjectID(),
	})
	if err != nil {
		return toolruntime.Result{}, err
	}
	var b strings.Builder
	for _, hit := range res.Hits {
		content := strings.TrimSpace(hit.Content)
		if content == "" {
			continue
		}
		fmt.Fprintf(&b, "%s\t%s\n", hit.Source, clipRunes(content, 400))
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return toolruntime.Result{Output: "no confirmed memories matched"}, nil
	}
	return toolruntime.Result{Output: out}, nil
}

func (e *Engine) invokeMemoryGet(ctx context.Context, raw json.RawMessage) (toolruntime.Result, error) {
	if e == nil || e.m8memory == nil {
		return toolruntime.Result{}, errors.New("confirmed memory is not available")
	}
	var a struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &a) != nil || strings.TrimSpace(a.ID) == "" {
		return toolruntime.Result{}, errors.New("memory.get needs id")
	}
	id := strings.TrimSpace(a.ID)
	id = strings.TrimPrefix(id, "candidate:")
	content, err := e.m8memory.ConfirmedByIDFor(ctx, id, e.memorySubjectID())
	if err != nil {
		if errors.Is(err, m8app.ErrCandidateNotFound) {
			return toolruntime.Result{Output: "confirmed memory not found"}, nil
		}
		return toolruntime.Result{}, err
	}
	return toolruntime.Result{Output: clipRunes(content, 800)}, nil
}

func formatChatMemorySummary(pack chatMemoryPack) string {
	if len(pack.Prefs) == 0 && len(pack.Pinned) == 0 && len(pack.TaskState) == 0 && len(pack.Evidence) == 0 {
		return ""
	}
	prefPart := "偏好 " + strconv.Itoa(len(pack.Prefs))
	if n := len(pack.Prefs); n > 0 {
		shown := pack.Prefs
		if n > 2 {
			shown = pack.Prefs[:2]
		}
		quoted := make([]string, 0, len(shown))
		for _, pref := range shown {
			quoted = append(quoted, "「"+clipRunes(strings.TrimSpace(pref), 16)+"」")
		}
		prefPart = "偏好" + strings.Join(quoted, "")
		if n > 2 {
			prefPart += " 等" + strconv.Itoa(n) + "条"
		}
	}
	return "注入记忆：" + prefPart + " · 置顶 " + strconv.Itoa(len(pack.Pinned)) + " · 任务 " + strconv.Itoa(len(pack.TaskState)) + " · 证据 " + strconv.Itoa(len(pack.Evidence))
}

func renderPreferenceInstruction(instruction string, prefs []string) string {
	if len(prefs) == 0 {
		return instruction
	}
	var b strings.Builder
	b.WriteString(instruction)
	b.WriteString("\n\n[持久记忆]\n用户偏好（已显式确认，回答时必须遵守）：\n")
	for _, pref := range prefs {
		b.WriteString("- ")
		b.WriteString(pref)
		b.WriteString("\n")
	}
	return b.String()
}

func looksLikePreferenceTurn(userText string) bool {
	t := strings.TrimSpace(userText)
	if t == "" {
		return false
	}
	lower := strings.ToLower(t)
	for _, n := range []string{
		"记住", "请记住", "以后", "默认用", "默认都", "从现在起",
		"都用中文", "请用中文", "不要再用", "我希望你",
		"remember", "from now on", "always use", "please always",
	} {
		if strings.Contains(t, n) || strings.Contains(lower, n) {
			return true
		}
	}
	return false
}

func turnLongEnoughForMemory(userText, assistantText string) bool {
	if utf8.RuneCountInString(userText) < autoNominateMinUserRunes {
		return false
	}
	need := autoNominateMinAsstRunes
	if looksLikePreferenceTurn(userText) {
		need = preferenceMinAsstRunes
	}
	return utf8.RuneCountInString(assistantText) >= need
}

func (e *Engine) maybeAutoNominateTurn(ctx context.Context, sessionID, userText, assistantText, messageID string, companion bool) error {
	if e == nil || sessionID == "" || messageID == "" {
		return nil
	}
	userText = strings.TrimSpace(userText)
	assistantText = strings.TrimSpace(assistantText)
	pref := looksLikePreferenceTurn(userText)
	if companion && !pref {
		return nil
	}
	settings := e.chatMemorySettings(ctx)
	if !settings.MemoryEnabled {
		return nil
	}
	if !settings.AutoNominate && !pref {
		return nil
	}
	if !turnLongEnoughForMemory(userText, assistantText) {
		return nil
	}
	gist := clipRunes("用户："+userText+"\n要点："+assistantText, autoNominateMaxContent)
	if e.m8memory != nil {
		pending, err := e.m8memory.ListPendingCandidatesFor(ctx, e.memorySubjectID(), 20)
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
		reason := "本轮对话自动提名"
		if pref {
			reason = preferenceNominationReason
		}
		_, err := e.m10nomination.Nominate(ctx, m8app.NominateInput{
			SubjectID:       e.memorySubjectID(),
			Doc:             doc,
			Reason:          reason,
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
		SubjectID: e.memorySubjectID(),
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

func expertOwnedMemoryKey(expertID, kind string) string {
	return "expert:" + strings.TrimSpace(expertID) + ":" + kind
}

func (e *Engine) resolveTurnExpertIDs(ctx context.Context, sessionID, turnText string) []string {
	eq := e.turnEquipmentFor(ctx, sessionID, turnText, false)
	if len(eq.ExpertIDs) > 0 {
		return append([]string(nil), eq.ExpertIDs...)
	}
	if e.m8expert == nil || len(eq.Names) == 0 {
		return nil
	}
	listed, err := e.m8expert.List(ctx, m8app.ExpertFilter{})
	if err != nil {
		return nil
	}
	byName := map[string]string{}
	for _, row := range listed.Experts {
		byName[row.Name] = row.ExpertID
	}
	var out []string
	seen := map[string]bool{}
	for _, name := range eq.Names {
		id := strings.TrimSpace(byName[name])
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (e *Engine) maybeWriteExpertTurnMemories(ctx context.Context, sessionID, userText, assistantText string) {
	for _, id := range e.resolveTurnExpertIDs(ctx, sessionID, userText) {
		e.writeExpertLastMemory(ctx, sessionID, id, userText, assistantText)
	}
}

// writeSessionLastMemory is the cross-surface working gist: text → companion
// continue → people “继续刚才的”. It is not a confirmed preference. Companion
// turns skip auto-nominate (voice chitchat), but they still upsert this key
// so the next surface can see “刚才”.
func (e *Engine) writeSessionLastMemory(ctx context.Context, sessionID, userText, assistantText string) {
	if e == nil || !memoryServiceAvailable(e.memories) || sessionID == "" {
		return
	}
	settings := e.chatMemorySettings(ctx)
	if !settings.MemoryEnabled {
		return
	}
	userText = strings.TrimSpace(userText)
	assistantText = strings.TrimSpace(assistantText)
	if !turnLongEnoughForMemory(userText, assistantText) {
		return
	}
	projectID := e.projectIDForSession(ctx, sessionID)
	if projectID == "" {
		return
	}
	src, srcType := sessionID, "session"
	content := clipRunes("用户："+userText+"\n要点："+assistantText, autoNominateMaxContent)
	if err := e.upsertWorkingMemory(ctx, projectID, sessionLastMemoryKey, content, &src, &srcType); err != nil {
		log.Printf("chat memory: session last write skipped: %v", err)
	}
}

// recordPeopleTurnMemory is the colleague-surface closeout: working gist,
// expert last, and a preference-shaped turn into the confirm inbox.
// AutoNominate stays off by default; only 记住/以后/默认用… nominate.
func (e *Engine) recordPeopleTurnMemory(ctx context.Context, sessionID, expertID, userText, assistantText, messageID string) {
	e.writeExpertLastMemory(ctx, sessionID, expertID, userText, assistantText)
	e.writeSessionLastMemory(ctx, sessionID, userText, assistantText)
	_ = e.maybeAutoNominateTurn(ctx, sessionID, userText, assistantText, messageID, false)
}

func (e *Engine) writeExpertLastMemory(ctx context.Context, sessionID, expertID, userText, assistantText string) {
	if e == nil || !memoryServiceAvailable(e.memories) || sessionID == "" {
		return
	}
	expertID = strings.TrimSpace(expertID)
	if expertID == "" {
		return
	}
	settings := e.chatMemorySettings(ctx)
	if !settings.MemoryEnabled {
		return
	}
	userText = strings.TrimSpace(userText)
	assistantText = strings.TrimSpace(assistantText)
	if utf8.RuneCountInString(userText) < autoNominateMinUserRunes || utf8.RuneCountInString(assistantText) < autoNominateMinAsstRunes {
		return
	}
	projectID := e.projectIDForSession(ctx, sessionID)
	if projectID == "" {
		return
	}
	src, srcType := sessionID, "session"
	content := clipRunes("用户："+userText+"\n要点："+assistantText, autoNominateMaxContent)
	if err := e.upsertWorkingMemory(ctx, projectID, expertOwnedMemoryKey(expertID, "last"), content, &src, &srcType); err != nil {
		log.Printf("chat memory: expert %s write skipped: %v", expertID, err)
		return
	}
	e.maybeNominateExpertLast(ctx, sessionID, expertID, content)
}

func (e *Engine) maybeNominateExpertLast(ctx context.Context, sessionID, expertID, gist string) {
	if e == nil || sessionID == "" || strings.TrimSpace(gist) == "" {
		return
	}
	settings := e.chatMemorySettings(ctx)
	if !settings.MemoryEnabled {
		return
	}
	if e.m8memory != nil {
		pending, err := e.m8memory.ListPendingCandidatesFor(ctx, e.memorySubjectID(), 20)
		if err == nil {
			for _, item := range pending {
				if item.Content == gist {
					return
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
			EvidenceRef: clipRunes("expert://"+strings.TrimSpace(expertID)+"/"+sessionID, m8core.MaxEvidenceRef),
			Digest:      m8core.DigestOf(gist),
		}},
	}
	if e.m10nomination != nil {
		_, err := e.m10nomination.Nominate(ctx, m8app.NominateInput{
			SubjectID:       e.memorySubjectID(),
			Doc:             doc,
			Reason:          expertLastNominationReason,
			Nominator:       "chat.expert",
			SourceSessionID: sessionID,
			Actor:           "engine",
		})
		if err != nil {
			log.Printf("chat memory: expert last nomination skipped: %v", err)
		}
		return
	}
	if e.m8memory == nil {
		return
	}
	_, err := e.m8memory.ProposeCandidate(ctx, m8app.ProposeInput{
		SubjectID: e.memorySubjectID(),
		Doc:       doc,
		Inferred:  true,
		Trust:     m8core.TrustUntrusted,
		Actor:     "engine",
	})
	if err != nil {
		log.Printf("chat memory: expert last propose skipped: %v", err)
	}
}

func (e *Engine) upsertWorkingMemory(ctx context.Context, projectID, key, content string, sourceID, sourceType *string) error {
	if !memoryServiceAvailable(e.memories) {
		return nil
	}
	items, err := e.memories.ListByProject(ctx, projectID, memory.LayerWorking)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Key == key {
			return e.memories.UpdateContent(ctx, item.ID, content)
		}
	}
	_, err = e.memories.Create(ctx, memory.Memory{
		ProjectID:  projectID,
		Layer:      memory.LayerWorking,
		Scope:      memory.ScopeProject,
		Key:        key,
		Content:    content,
		SourceID:   sourceID,
		SourceType: sourceType,
		Confidence: 0.7,
	})
	return err
}
