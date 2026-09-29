package producthub

import (
	"context"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// LandscapeQuote is one candidate excerpt returned by the researcher channel.
// The engine-side researcher fetched the public page and already enforced the
// verbatim gate (the quote must be a substring of the fetched page text). The
// hub re-checks structure before a quote may become a draft: name must come
// from the saved list, axis must be one of the four comparison axes, and the
// URL and date must be well formed. Anything else is dropped, not repaired.
type LandscapeQuote struct {
	Name  string `json:"name"`
	Axis  string `json:"axis"`
	Quote string `json:"quote"`
	URL   string `json:"url"`
	Date  string `json:"date"`
}

// LandscapeDraft is a collected quote waiting for human confirmation.
// Status is draft or confirmed. A draft never enters the report and never
// touches the health score; only a confirmed draft is quoted in 竞品对照.
type LandscapeDraft struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Axis   string `json:"axis"`
	Quote  string `json:"quote"`
	URL    string `json:"url"`
	Date   string `json:"date"`
	Status string `json:"status"`
}

// LandscapeResearcher is the collection channel. The engine implements it
// with a read-only GET of registered public pages plus an LLM that may only
// answer with axis + verbatim quote; the URL and date are stamped by the
// program, never by the model.
type LandscapeResearcher interface {
	// ResearchLandscape returns candidate quotes that already passed the
	// verbatim gate, plus one human-readable skip reason per name that
	// produced nothing this round.
	ResearchLandscape(ctx context.Context, names []string) (quotes []LandscapeQuote, skips []string)
}

// landscapeAxes maps the four comparison axes to the labels the report
// already uses on the landscape page payload.
var landscapeAxes = map[string]string{
	"local":  "本机优先",
	"media":  "媒体核验",
	"assets": "技能 / MCP",
	"hub":    "知识自描述",
}

// SetResearcher wires the collection channel. Without one, collecting
// explains itself instead of failing.
func (s *Service) SetResearcher(r LandscapeResearcher) {
	if s != nil {
		s.researcher = r
	}
}

// CollectLandscape runs one manual collection round over the names already
// saved on the landscape page. Every quote that survives both gates is
// stored as a draft; nothing here changes the report or the health score.
func (s *Service) CollectLandscape(ctx context.Context, names []string) (map[string]any, error) {
	out := map[string]any{"collected": 0, "skipped": []string{}, "drafts": []LandscapeDraft{}}
	if s == nil {
		return out, nil
	}
	wanted := map[string]bool{}
	var list []string
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" || isSelfName(name) {
			continue
		}
		if !wanted[name] {
			wanted[name] = true
			list = append(list, name)
		}
	}
	if len(list) == 0 {
		out["skipped"] = []string{"图景页还没有保存竞品名单，先加入产品再采集。"}
		return out, nil
	}
	if s.researcher == nil {
		out["skipped"] = []string{"采集通道未装配：本机没有可用的采集实现，不能编造摘录。"}
		return out, nil
	}
	quotes, skips := s.researcher.ResearchLandscape(ctx, list)
	out["skipped"] = skips
	drafts := make([]LandscapeDraft, 0, len(quotes))
	for _, q := range quotes {
		if !validLandscapeQuote(q, wanted) {
			out["skipped"] = append(out["skipped"].([]string),
				"「"+strings.TrimSpace(q.Name)+"」有一条摘录没有通过结构校验，已丢弃。")
			continue
		}
		d := LandscapeDraft{
			ID: ulid.Make().String(), Name: strings.TrimSpace(q.Name), Axis: q.Axis,
			Quote: strings.TrimSpace(q.Quote), URL: strings.TrimSpace(q.URL), Date: q.Date,
			Status: "draft",
		}
		if err := s.persist.ProductHubSaveLandscapeDraft(ctx, d); err != nil {
			return nil, err
		}
		drafts = append(drafts, d)
	}
	out["collected"] = len(drafts)
	out["drafts"] = drafts
	return out, nil
}

// LandscapeDrafts lists every stored draft and confirmed entry, drafts first.
func (s *Service) LandscapeDrafts(ctx context.Context) ([]LandscapeDraft, error) {
	all, err := s.persist.ProductHubLoadLandscapeDrafts(ctx)
	if err != nil {
		return nil, err
	}
	var drafts, confirmed []LandscapeDraft
	for _, d := range all {
		if d.Status == "draft" {
			drafts = append(drafts, d)
		} else {
			confirmed = append(confirmed, d)
		}
	}
	return append(drafts, confirmed...), nil
}

// ConfirmLandscapeDraft turns a draft into a confirmed entry. Confirming is
// the human gate: from this point the report may quote the excerpt with its
// source URL and date. Confirming an already confirmed draft is a no-op.
func (s *Service) ConfirmLandscapeDraft(ctx context.Context, id string) (LandscapeDraft, error) {
	all, err := s.persist.ProductHubLoadLandscapeDrafts(ctx)
	if err != nil {
		return LandscapeDraft{}, err
	}
	for _, d := range all {
		if d.ID != strings.TrimSpace(id) {
			continue
		}
		if d.Status != "confirmed" {
			d.Status = "confirmed"
			if err := s.persist.ProductHubSaveLandscapeDraft(ctx, d); err != nil {
				return LandscapeDraft{}, err
			}
		}
		return d, nil
	}
	return LandscapeDraft{}, ErrNotFound
}

// DiscardLandscapeDraft deletes one draft or confirmed entry.
func (s *Service) DiscardLandscapeDraft(ctx context.Context, id string) error {
	return s.persist.ProductHubDeleteLandscapeDraft(ctx, strings.TrimSpace(id))
}

// validLandscapeQuote is the structural gate. It never repairs input.
func validLandscapeQuote(q LandscapeQuote, wanted map[string]bool) bool {
	name := strings.TrimSpace(q.Name)
	quote := strings.TrimSpace(q.Quote)
	url := strings.TrimSpace(q.URL)
	if !wanted[name] || len(name) > 64 {
		return false
	}
	if _, ok := landscapeAxes[q.Axis]; !ok {
		return false
	}
	if len(quote) == 0 || len(quote) > 600 {
		return false
	}
	if len(url) < 8 || len(url) > 512 || !(strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://")) {
		return false
	}
	if _, err := time.Parse("2006-01-02", q.Date); err != nil || len(q.Date) != 10 {
		return false
	}
	return true
}

// mergeConfirmedLandscape appends confirmed collected entries to the notes
// the landscape page already sent, so 竞品对照 quotes them with their source
// URL and collection date. Drafts stay out.
func (s *Service) mergeConfirmedLandscape(ctx context.Context, notes []LandscapeNote) []LandscapeNote {
	all, err := s.persist.ProductHubLoadLandscapeDrafts(ctx)
	if err != nil {
		return notes
	}
	for _, d := range all {
		if d.Status != "confirmed" {
			continue
		}
		label, ok := landscapeAxes[d.Axis]
		if !ok {
			continue
		}
		notes = append(notes, LandscapeNote{
			Name: d.Name, Axis: label, Note: d.Quote, Source: d.URL, Date: d.Date,
		})
	}
	return notes
}
