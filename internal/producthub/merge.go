package producthub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

func Merge(seed []Card, live []Candidate, previous []Card) []Card {
	prevBy := indexCards(previous)
	seedBy := indexCards(seed)
	liveBy := map[string]Candidate{}
	for _, c := range live {
		if c.StableKey == "" {
			continue
		}
		liveBy[c.StableKey] = c
	}
	keys := map[string]struct{}{}
	for k := range seedBy {
		keys[k] = struct{}{}
	}
	for k := range liveBy {
		keys[k] = struct{}{}
	}
	out := make([]Card, 0, len(keys))
	for k := range keys {
		s, hasSeed := seedBy[k]
		l, hasLive := liveBy[k]
		switch {
		case hasSeed && hasLive:
			out = append(out, overlay(s, l))
		case hasLive:
			out = append(out, fromCandidate(l))
		case hasSeed && (s.Source == "landscape" || strings.HasPrefix(k, "landscape.")):
			s.Provenance = "seed"
			if s.Chain.Steps == nil {
				s.Chain = defaultChain(s.ChainClass, s.Name)
			}
			out = append(out, s)
		case hasSeed:
			s.Provenance = "seed-retired"
			s.Tags = appendUnique(s.Tags, "status:弃用")
			if _, ok := prevBy[k]; ok || hasSeed {
				out = append(out, s)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StableKey < out[j].StableKey })
	return out
}

func Changelog(previous, next []Card) []Change {
	prevBy := indexCards(previous)
	nextBy := indexCards(next)
	var ch []Change
	for k, n := range nextBy {
		p, ok := prevBy[k]
		if !ok {
			ch = append(ch, describeChange(n, "added"))
			continue
		}
		if cardDigest(p) != cardDigest(n) {
			ch = append(ch, describeChange(n, "updated"))
		}
	}
	for k, p := range prevBy {
		if _, ok := nextBy[k]; !ok {
			ch = append(ch, describeChange(p, "removed"))
		}
	}
	sort.Slice(ch, func(i, j int) bool {
		if ch[i].Kind == ch[j].Kind {
			return ch[i].StableKey < ch[j].StableKey
		}
		return ch[i].Kind < ch[j].Kind
	})
	return ch
}

func describeChange(card Card, kind string) Change {
	summary := card.Summary
	if summary == "" {
		switch kind {
		case "added":
			summary = "新增功能卡，已挂到对应前台页面。"
		case "removed":
			summary = "活源不再提供该卡，已按页面退役。"
		default:
			summary = "功能卡或链路声明有更新。"
		}
	}
	impacts := append([]string{}, card.Scaffold.Pages...)
	for _, page := range card.Scaffold.Pages {
		impacts = appendUnique(impacts, "page."+page)
	}
	if card.Module != "" {
		impacts = appendUnique(impacts, "module."+card.Domain+"."+card.Module)
	}
	return Change{
		StableKey: card.StableKey,
		Kind:      kind,
		Title:     card.Name,
		Summary:   summary,
		Impacts:   impacts,
	}
}

func overlay(seed Card, live Candidate) Card {
	out := fromCandidate(live)
	if seedComplete(seed) {
		out.Name = seed.Name
		out.NameEN = seed.NameEN
		out.Summary = seed.Summary
		out.Description = seed.Description
		out.Methods = seed.Methods
		out.Chain = seed.Chain
		out.Principle = seed.Principle
		out.Logic = seed.Logic
		out.Tech = seed.Tech
		out.Analysis = seed.Analysis
		out.Provenance = "seed+live"
	} else {
		if out.Summary == "" {
			out.Summary = seed.Summary
		}
		if out.Description == "" {
			out.Description = seed.Description
		}
		if len(seed.Methods) > 0 {
			out.Methods = seed.Methods
		}
		if seedComplete(seed) || len(seed.Chain.Steps) >= 3 {
			out.Chain = seed.Chain
		}
		out.Provenance = "live"
	}
	if len(seed.Attributes.Tools) > 0 && len(out.Attributes.Tools) == 0 {
		out.Attributes = seed.Attributes
	}
	if len(seed.Scaffold.Bridge) > 0 && len(out.Scaffold.Bridge) == 0 {
		out.Scaffold = seed.Scaffold
	}
	out.Tags = seed.Tags
	return out
}

func fromCandidate(c Candidate) Card {
	class := c.ChainClass
	if class == "" {
		class = "crud-bridge"
	}
	summary := c.Summary
	if summary == "" {
		summary = c.Name
	}
	desc := c.Description
	if desc == "" {
		desc = c.Name + "由产品活源自动生成，底座见脚手架。"
	}
	methods := c.Methods
	if len(methods) == 0 {
		methods = defaultMethods(class)
	}
	return Card{
		StableKey:   c.StableKey,
		Name:        c.Name,
		NameEN:      c.NameEN,
		Domain:      c.Domain,
		Module:      c.Module,
		Summary:     clip(summary, 128),
		Description: clip(desc, 1024),
		Attributes:  c.Attributes,
		Methods:     methods,
		Chain:       defaultChain(class, c.Name),
		ChainClass:  class,
		Scaffold:    c.Scaffold,
		Provenance:  "live",
		Source:      c.Source,
		Probe:       ProbeScore{Passed: 1, Total: 1},
		Version:     "generated",
	}
}

func seedComplete(c Card) bool {
	if strings.TrimSpace(c.Summary) == "" || strings.TrimSpace(c.Description) == "" {
		return false
	}
	if len(c.Chain.Steps) < 3 || len(c.Chain.Steps) > 8 {
		return false
	}
	var ok, fail bool
	for _, b := range c.Chain.Branches {
		if b.Type == "success" {
			ok = true
		}
		if b.Type == "failure" && (b.Retry != nil || b.Fallback != nil || strings.TrimSpace(b.Description) != "") {
			fail = true
		}
	}
	return ok && fail
}

func indexCards(in []Card) map[string]Card {
	out := make(map[string]Card, len(in))
	for _, c := range in {
		if c.StableKey != "" {
			out[c.StableKey] = c
		}
	}
	return out
}

func cardDigest(c Card) string {
	copy := c
	copy.Probe = ProbeScore{}
	raw, _ := json.Marshal(copy)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func appendUnique(in []string, v string) []string {
	for _, x := range in {
		if x == v {
			return in
		}
	}
	return append(in, v)
}
