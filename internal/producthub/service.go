package producthub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

type Service struct {
	persist Persist
	gate    *gate
	collab  Collaborator
}

func New(persist Persist) *Service {
	if persist == nil {
		persist = &MemoryPersist{}
	}
	return &Service{persist: persist, gate: newGate()}
}

func (s *Service) Status(ctx context.Context, token string) map[string]any {
	unlocked := s.Check(token)
	return map[string]any{
		"username": AdminUsername,
		"unlocked": unlocked,
		"visible":  unlocked,
	}
}

func (s *Service) Latest(ctx context.Context) (*Edition, error) {
	return s.persist.ProductHubLoadLatest(ctx)
}

func (s *Service) Generate(ctx context.Context, trigger string) (Edition, error) {
	if trigger == "manual" {
		if err := s.touchRefresh(); err != nil {
			return Edition{}, err
		}
	}
	if err := s.ensureAuth(ctx); err != nil {
		return Edition{}, err
	}
	prev, _ := s.persist.ProductHubLoadLatest(ctx)
	var previous []Card
	if prev != nil {
		previous = prev.Features
	}
	live := LiveCatalog()
	cards := Merge(Seed(), live, previous)
	for i := range cards {
		cards[i] = enrichCard(cards[i])
		if len(cards[i].Methods) == 0 {
			cards[i].Methods = defaultMethods(emptyText(cards[i].ChainClass, "crud-bridge"))
		}
	}
	if ens, err := s.persist.ProductHubLoadEnrichments(ctx); err == nil {
		cards = applyEnrichments(cards, ens)
	}
	manual, _ := s.persist.ProductHubLoadTags(ctx)
	cards = applyTags(cards, manual)
	ch := Changelog(previous, cards)
	findings, probe, cards := diagnoseCatalog(cards, live)
	if prev != nil {
		findings = mergeFindingStatus(prev.Findings, findings)
	}
	if logs, err := s.persist.ProductHubLoadApplies(ctx); err == nil {
		findings = attachApplies(findings, logs)
	}
	added, updated, removed := countKinds(ch)
	ed := Edition{
		EditionID:   ulid.Make().String(),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		CardCount:   len(cards),
		Added:       added,
		Updated:     updated,
		Removed:     removed,
		Features:    cards,
		Changes:     ch,
		Findings:    findings,
		Graph:       BuildGraph(cards),
	}
	if trigger == "manual" {
		tasks, logText, notes := invokeLive(ctx)
		faults := ClassifyLog(logText)
		ed.LiveProbe, ed.HealthScore = ScoreLive(tasks, faults)
		ed.Findings = append(ed.Findings, TaskFindings(tasks)...)
		ed.Findings = append(ed.Findings, faults...)
		ed.Findings = append(ed.Findings, LandscapeFindings(notes)...)
	} else {
		ed.HealthScore = healthScore(findings, probe)
	}
	ed.ReportMarkdown, ed.ReportHTML = RenderReport(ed)
	ed.Digest = digestEdition(ed)
	if err := s.persist.ProductHubSaveEdition(ctx, ed); err != nil {
		return Edition{}, err
	}
	return ed, nil
}

func (s *Service) Overview(ctx context.Context) (Overview, error) {
	ed, err := s.requireEdition(ctx)
	if err != nil {
		return Overview{}, err
	}
	cards, findings, probe, err := s.view(ctx, ed)
	if err != nil {
		return Overview{}, err
	}
	added, updated, removed := countKinds(Changelog(ed.Features, cards))
	score, shown, live := displayedScore(ed, findings, probe)
	return Overview{
		Product: "Lunitide", EditionID: ed.EditionID, GeneratedAt: ed.GeneratedAt,
		CardCount: len(cards), HealthScore: score,
		Added: added, Updated: updated, Removed: removed,
		ProbePassed: shown.Passed, ProbeTotal: shown.Total, LiveChecked: live,
		Domains: domainStats(cards), Tags: collectTagValues(cards),
	}, nil
}

// view rebuilds the booklet from the current live catalog without writing an edition
// and without calling a skill or model.
func (s *Service) view(ctx context.Context, ed Edition) ([]Card, []Finding, ProbeScore, error) {
	live := LiveCatalog()
	cards := Merge(Seed(), live, ed.Features)
	for i := range cards {
		cards[i] = enrichCard(cards[i])
		if len(cards[i].Methods) == 0 {
			cards[i].Methods = defaultMethods(emptyText(cards[i].ChainClass, "crud-bridge"))
		}
	}
	if ens, err := s.persist.ProductHubLoadEnrichments(ctx); err == nil {
		cards = applyEnrichments(cards, ens)
	}
	if manual, err := s.persist.ProductHubLoadTags(ctx); err == nil {
		cards = applyTags(cards, manual)
	}
	findings, probe, cards := diagnoseCatalog(cards, live)
	findings = mergeFindingStatus(ed.Findings, findings)
	findings = append(findings, liveFindings(ed.Findings)...)
	if logs, err := s.persist.ProductHubLoadApplies(ctx); err == nil {
		findings = attachApplies(findings, logs)
	}
	return cards, findings, probe, nil
}

func (s *Service) FeatureCard(ctx context.Context, key string) (Card, bool, error) {
	ed, err := s.requireEdition(ctx)
	if err != nil {
		return Card{}, false, err
	}
	cards, _, _, err := s.view(ctx, ed)
	if err != nil {
		return Card{}, false, err
	}
	for _, c := range cards {
		if c.StableKey == key {
			return c, true, nil
		}
	}
	return Card{}, false, nil
}

func (s *Service) Graph(ctx context.Context) (Graph, error) {
	ed, err := s.requireEdition(ctx)
	if err != nil {
		return Graph{}, err
	}
	cards, _, _, err := s.view(ctx, ed)
	if err != nil {
		return Graph{}, err
	}
	return BuildGraph(cards), nil
}

func (s *Service) Node(ctx context.Context, id string) (GraphNode, bool, error) {
	g, err := s.Graph(ctx)
	if err != nil {
		return GraphNode{}, false, err
	}
	for _, n := range g.Nodes {
		if n.ID == id || n.StableKey == id {
			return n, true, nil
		}
	}
	return GraphNode{}, false, nil
}

func (s *Service) Changelog(ctx context.Context) ([]Change, error) {
	ed, err := s.requireEdition(ctx)
	if err != nil {
		return nil, err
	}
	cards, _, _, err := s.view(ctx, ed)
	if err != nil {
		return nil, err
	}
	return Changelog(ed.Features, cards), nil
}

func (s *Service) Diagnostics(ctx context.Context) ([]Finding, string, string, error) {
	ed, err := s.requireEdition(ctx)
	if err != nil {
		return nil, "", "", err
	}
	cards, findings, probe, err := s.view(ctx, ed)
	if err != nil {
		return nil, "", "", err
	}
	ed.Features = cards
	ed.Findings = findings
	ed.HealthScore, _, _ = displayedScore(ed, findings, probe)
	ed.Graph = BuildGraph(cards)
	md, pageHTML := RenderReport(ed)
	return findings, md, pageHTML, nil
}

func (s *Service) Tags(ctx context.Context) ([]string, error) {
	ed, err := s.requireEdition(ctx)
	if err != nil {
		return nil, err
	}
	return collectTagValues(ed.Features), nil
}

func (s *Service) TagSet(ctx context.Context, key, vocab, value string) error {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(vocab) == "" || strings.TrimSpace(value) == "" {
		return ErrNotFound
	}
	if err := s.persist.ProductHubSaveTag(ctx, NodeTag{StableKey: key, Vocab: vocab, Value: value, AssignedBy: "manual"}); err != nil {
		return err
	}
	ed, err := s.persist.ProductHubLoadLatest(ctx)
	if err != nil || ed == nil {
		return err
	}
	tag := vocab + ":" + value
	for i := range ed.Features {
		if ed.Features[i].StableKey == key {
			ed.Features[i].Tags = appendUnique(ed.Features[i].Tags, tag)
		}
	}
	return s.persist.ProductHubSaveEdition(ctx, *ed)
}

func (s *Service) Export(ctx context.Context, format string) (content, mime string, err error) {
	ed, err := s.requireEdition(ctx)
	if err != nil {
		return "", "", err
	}
	cards, findings, probe, err := s.view(ctx, ed)
	if err != nil {
		return "", "", err
	}
	ed.Features = cards
	ed.Findings = findings
	ed.HealthScore, _, _ = displayedScore(ed, findings, probe)
	md, pageHTML := RenderReport(ed)
	switch format {
	case "html":
		return pageHTML, "text/html", nil
	case "poster":
		return "", "", ErrNotImplemented
	default:
		return md, "text/markdown", nil
	}
}

func (s *Service) requireEdition(ctx context.Context) (Edition, error) {
	ed, err := s.persist.ProductHubLoadLatest(ctx)
	if err != nil {
		return Edition{}, err
	}
	if ed == nil {
		return s.Generate(ctx, "boot")
	}
	return *ed, nil
}

func digestEdition(ed Edition) string {
	copy := ed
	copy.ReportHTML = ""
	copy.ReportMarkdown = ""
	raw, _ := json.Marshal(copy.Features)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
