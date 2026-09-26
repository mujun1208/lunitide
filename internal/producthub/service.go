package producthub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/buildinfo"
	"github.com/oklog/ulid/v2"
)

type Service struct {
	persist Persist
	gate    *gate
	collab  Collaborator
	version string
	openMu  sync.Mutex
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

func (s *Service) SetProductVersion(version string) {
	if s != nil {
		s.version = strings.TrimSpace(version)
	}
}

func (s *Service) productVersion() string {
	if s != nil && strings.TrimSpace(s.version) != "" {
		return strings.TrimSpace(s.version)
	}
	return strings.TrimSpace(buildinfo.Version)
}

func (s *Service) Latest(ctx context.Context) (*Edition, error) {
	return s.persist.ProductHubLoadLatest(ctx)
}

// open is the login read. A stored assembly for this product version is shown
// as-is. A missing store or a different version queries the current product,
// reassembles every card and the graph, and saves that version.
func (s *Service) open(ctx context.Context) (Edition, error) {
	s.openMu.Lock()
	defer s.openMu.Unlock()
	ed, err := s.persist.ProductHubLoadLatest(ctx)
	if err != nil {
		return Edition{}, err
	}
	ver := s.productVersion()
	if ed != nil && ed.ProductVersion == ver && ver != "" && len(ed.Features) > 0 && len(ed.Graph.Nodes) > 0 && storedAssemblyComplete(ed) {
		return *ed, nil
	}
	return s.Generate(ctx, "version")
}

func storedAssemblyComplete(ed *Edition) bool {
	if !pluginRosterComplete(ed) {
		return false
	}
	for _, node := range ed.Graph.Nodes {
		if node.Type != "Expert" {
			continue
		}
		if strings.TrimSpace(node.Principle) == "" || strings.TrimSpace(node.Logic) == "" || strings.TrimSpace(node.Analysis) == "" {
			return false
		}
	}
	return true
}

func pluginRosterComplete(ed *Edition) bool {
	if ed == nil {
		return false
	}
	want := 0
	for _, card := range ed.Features {
		if _, ok := pluginRosterID(card.StableKey); ok {
			want++
		}
	}
	if want == 0 {
		return false
	}
	got := 0
	for _, node := range ed.Graph.Nodes {
		if node.Type != "Plugin" {
			continue
		}
		if node.StableKey == "plugin.plugins" {
			return false
		}
		got++
	}
	return got == want
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
		EditionID:    ulid.Make().String(),
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		CardCount:    len(cards),
		Added:        added,
		Updated:      updated,
		Removed:      removed,
		Features:     cards,
		Changes:      ch,
		Findings:     findings,
		Graph:        BuildGraph(cards),
		CatalogProbe: probe,
	}
	if trigger == "manual" || trigger == "check" {
		tasks, logText, notes := invokeLive(ctx)
		since := time.Time{}
		if prev != nil {
			since = readWatermark(prev.Findings)
		}
		faults := ClassifyLogSince(logText, since)
		ed.LiveProbe, ed.HealthScore = ScoreLive(tasks, faults)
		ed.Findings = append(ed.Findings, TaskFindings(tasks)...)
		ed.Findings = append(ed.Findings, faults...)
		ed.Findings = append(ed.Findings, LandscapeFindings(notes)...)
		ed.Findings = append(ed.Findings, watermarkFinding(logClock()))
	} else if prev != nil {
		ed.Findings = append(ed.Findings, liveFindings(prev.Findings)...)
		ed.HealthScore = healthScore(ed.Findings, probe)
	} else {
		ed.HealthScore = healthScore(findings, probe)
	}
	ed.ReportMarkdown, ed.ReportHTML = RenderReport(ed)
	ed.ProductVersion = s.productVersion()
	ed.Digest = digestEdition(ed)
	if err := s.persist.ProductHubSaveEdition(ctx, ed); err != nil {
		return Edition{}, err
	}
	return ed, nil
}

func (s *Service) Overview(ctx context.Context) (Overview, error) {
	ed, err := s.open(ctx)
	if err != nil {
		return Overview{}, err
	}
	score, shown, live := displayedScore(ed, ed.Findings, ed.CatalogProbe)
	return Overview{
		Product: "Lunitide", EditionID: ed.EditionID, GeneratedAt: ed.GeneratedAt,
		CardCount: len(ed.Features), HealthScore: score,
		Added: ed.Added, Updated: ed.Updated, Removed: ed.Removed,
		ProbePassed: shown.Passed, ProbeTotal: shown.Total, LiveChecked: live,
		Domains: domainStats(ed.Features), Tags: collectTagValues(ed.Features),
	}, nil
}

func (s *Service) FeatureCard(ctx context.Context, key string) (Card, bool, error) {
	ed, err := s.open(ctx)
	if err != nil {
		return Card{}, false, err
	}
	for _, c := range ed.Features {
		if c.StableKey == key {
			return c, true, nil
		}
	}
	return Card{}, false, nil
}

func (s *Service) Graph(ctx context.Context) (Graph, error) {
	ed, err := s.open(ctx)
	if err != nil {
		return Graph{}, err
	}
	return ed.Graph, nil
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
	ed, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	return ed.Changes, nil
}

func (s *Service) Diagnostics(ctx context.Context) ([]Finding, string, string, error) {
	if freshCheck(ctx) {
		ed, err := s.Generate(ctx, "check")
		if err != nil {
			return nil, "", "", err
		}
		return visibleFindings(ed.Findings), ed.ReportMarkdown, ed.ReportHTML, nil
	}
	ed, err := s.open(ctx)
	if err != nil {
		return nil, "", "", err
	}
	return visibleFindings(ed.Findings), ed.ReportMarkdown, ed.ReportHTML, nil
}

func (s *Service) Tags(ctx context.Context) ([]string, error) {
	ed, err := s.open(ctx)
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
	ed, err := s.open(ctx)
	if err != nil {
		return "", "", err
	}
	switch format {
	case "html":
		return ed.ReportHTML, "text/html", nil
	case "poster":
		return "", "", ErrNotImplemented
	default:
		return ed.ReportMarkdown, "text/markdown", nil
	}
}


func digestEdition(ed Edition) string {
	copy := ed
	copy.ReportHTML = ""
	copy.ReportMarkdown = ""
	raw, _ := json.Marshal(copy.Features)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
