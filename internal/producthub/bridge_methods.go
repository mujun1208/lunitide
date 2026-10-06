package producthub

import (
	"sort"
	"strconv"
	"strings"
	"sync"
)

// The engine verb table. The app layer registers every key of its runtime
// dispatch tables (RuntimeHandlers plus internalRuntimeHandlers) once at
// wiring time. The hub then knows the full verb surface — the runtime
// allow-list, not just the schema — without importing the engine.

var (
	bridgeMethodsMu sync.Mutex
	bridgeMethods   []string
)

// SetBridgeMethods registers the runtime verb table. An empty slice unregisters
// so tests can restore the package state.
func SetBridgeMethods(methods []string) {
	sorted := make([]string, 0, len(methods))
	for _, m := range methods {
		if m = strings.TrimSpace(m); m != "" {
			sorted = append(sorted, m)
		}
	}
	sort.Strings(sorted)
	bridgeMethodsMu.Lock()
	bridgeMethods = sorted
	bridgeMethodsMu.Unlock()
}

// RegisteredBridgeMethods returns a copy of the registered runtime verbs.
func RegisteredBridgeMethods() []string {
	bridgeMethodsMu.Lock()
	defer bridgeMethodsMu.Unlock()
	return append([]string(nil), bridgeMethods...)
}

// runtimeInternalPrefixes are engine plumbing verbs: transport, workspace
// files, logs, workers, and coordination. They keep the product running but
// are not user features, so they get one stats line instead of cards.
var runtimeInternalPrefixes = []string{
	"internal.", "fs.", "files.", "changeset.", "review.", "run.", "evidence.",
	"web.", "chat.", "subagent.", "delegation.", "trace.", "barrier.",
	"tombstone.", "stream.", "worker.", "handoff.", "sync.", "http.", "git.",
	"merge.", "complexity.", "collabGate.", "kb.", "tools.", "node.", "recall.",
	"backup.", "archive.", "document.", "devTask.", "item.", "widget.",
	"operation.", "ontology.", "deliverable.", "extension.", "connector.",
}

// bridgeModulePrefixes maps a user-facing verb prefix to its domain and module.
// Order matters where one prefix extends another: agentHub before agent.run,
// projectAttachment before project, browser before br, mc before mcp.
var bridgeModulePrefixes = []struct{ prefix, domain, module string }{
	{"agentHub.", "execution", "agenthub"},
	{"agent.run.", "execution", "subagents"},
	{"activity.", "foundation", "activity"},
	{"appUpdate.", "foundation", "diagnostics"},
	{"attachment.", "assets", "attachment"},
	{"automation.", "office", "automation"},
	{"browser.", "execution", "browser"},
	{"br.", "execution", "browser"},
	{"capability.", "foundation", "capability"},
	{"cc.", "execution", "computer"},
	{"code.workspace", "execution", "workspace"},
	{"command.", "execution", "command"},
	{"context.", "foundation", "diagnostics"},
	{"conversations.", "dialog", "session"},
	{"datasource.", "foundation", "datasources"},
	{"db.", "foundation", "datasources"},
	{"desktop.", "execution", "workspace"},
	{"diagnostics.", "foundation", "diagnostics"},
	{"diagram.", "office", "office"},
	{"expert.", "assets", "expert"},
	{"feedback.", "foundation", "feedback"},
	{"identity.", "foundation", "security"},
	{"im.", "execution", "channels"},
	{"mc.", "assets", "mcp"},
	{"mcp.", "assets", "mcp"},
	{"mcp6.", "assets", "mcp"},
	{"media.", "office", "media"},
	{"meetings.", "office", "meetings"},
	{"memory.", "assets", "memory"},
	{"message.", "dialog", "chat"},
	{"mro.", "office", "mro"},
	{"ocr.", "assets", "ocr"},
	{"office.", "office", "office"},
	{"omni.", "dialog", "companion"},
	{"openapi.", "office", "office"},
	{"org.", "foundation", "org"},
	{"people.", "office", "people"},
	{"plan.", "foundation", "projects"},
	{"plugin.", "assets", "plugins"},
	{"productHub.", "foundation", "producthub"},
	{"projectAttachment.", "foundation", "projects"},
	{"project.", "foundation", "projects"},
	{"provider.", "foundation", "providers"},
	{"release.", "foundation", "projects"},
	{"session.", "dialog", "session"},
	{"skill.", "assets", "skill"},
	{"stage.", "foundation", "projects"},
	{"system.", "foundation", "diagnostics"},
	{"talk.", "dialog", "companion"},
	{"template.", "office", "template"},
	{"terminal.", "execution", "terminal"},
	{"tts.", "dialog", "voice"},
	{"ui.", "foundation", "appearance"},
	{"voice.", "dialog", "companion"},
	{"workflow.", "foundation", "projects"},
	{"workspace.", "execution", "workspace"},
}

// bridgeMethodDomain classifies one runtime verb. A user-facing verb returns
// its domain and module; engine plumbing returns ok=false and gets no card.
// An unrecognised verb is treated as plumbing, not silently carded.
func bridgeMethodDomain(method string) (domain, module string, ok bool) {
	for _, prefix := range runtimeInternalPrefixes {
		if strings.HasPrefix(method, prefix) {
			return "", "", false
		}
	}
	for _, m := range bridgeModulePrefixes {
		if strings.HasPrefix(method, m.prefix) {
			return m.domain, m.module, true
		}
	}
	return "", "", false
}

// BridgeMethodIsUserFacing reports whether a registered verb belongs to a user
// feature (and so gets a card plus probe coverage) or to engine plumbing
// (stats line only). Unrecognised verbs count as plumbing, not silently carded.
func BridgeMethodIsUserFacing(method string) bool {
	_, _, ok := bridgeMethodDomain(method)
	return ok
}

// bridgeMethodCandidates turns every registered user-facing verb that no
// hand-written candidate and no seed card already claims into one template
// card, so the live catalog covers the whole verb surface. The card says it is
// an auto-registered template — it does not pretend to be a hand-written page.
func bridgeMethodCandidates(covered map[string]bool) []Candidate {
	var out []Candidate
	for _, method := range RegisteredBridgeMethods() {
		if covered[method] {
			continue
		}
		domain, module, ok := bridgeMethodDomain(method)
		if !ok {
			continue
		}
		out = append(out, Candidate{
			StableKey: "feature." + domain + ".bridge." + strings.ReplaceAll(method, ".", "-"),
			Name:      method, NameEN: method,
			Domain: domain, Module: module,
			Summary:     "「" + method + "」由引擎动词表自动登记，讲解为通用模板。",
			Description: "引擎运行时允许清单里的动词 " + method + "。这张卡由动词表自动登记：入口与链路是通用模板，不是逐项手写讲解；这个动词是否真的跑得通，以诊断实测行里的证据为准。",
			Source:      "bridge", ChainClass: "crud-bridge",
			Scaffold:    Scaffold{Bridge: []string{method}},
			Attributes:  Attributes{Operations: []string{method}, Tools: []string{method}},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StableKey < out[j].StableKey })
	return out
}

// coveredBridgeMethods collects the verbs already claimed by the hand-written
// candidates and the seed booklet, so the auto registry does not double-card a
// verb that already has a curated feature card.
func coveredBridgeMethods(candidates []Candidate) map[string]bool {
	covered := map[string]bool{}
	claim := func(bridges []string) {
		for _, b := range bridges {
			covered[b] = true
		}
	}
	for _, c := range candidates {
		claim(c.Scaffold.Bridge)
	}
	for _, c := range Seed() {
		claim(c.Scaffold.Bridge)
	}
	return covered
}

// BridgeMethodCounts summarises the runtime verb surface for the report:
// registered verbs, user-facing verbs, auto-carded verbs, verbs already on a
// curated card, and engine plumbing that deliberately gets no card.
type BridgeMethodCounts struct {
	Registered int
	UserFace   int
	Carded     int
	Covered    int
	Internal   int
}

func bridgeMethodCounts() BridgeMethodCounts {
	var c BridgeMethodCounts
	covered := coveredBridgeMethods(handWrittenCandidates())
	for _, method := range RegisteredBridgeMethods() {
		c.Registered++
		if _, _, ok := bridgeMethodDomain(method); !ok {
			c.Internal++
			continue
		}
		c.UserFace++
		if covered[method] {
			c.Covered++
		} else {
			c.Carded++
		}
	}
	return c
}

// BridgeMethodLine is the report's runtime-verb stats line. Empty when no
// verb table is registered, so reports from older callers stay unchanged.
func BridgeMethodLine() string {
	c := bridgeMethodCounts()
	if c.Registered == 0 {
		return ""
	}
	return "运行时动词 " + strconv.Itoa(c.Registered) + " 项：用户功能 " + strconv.Itoa(c.UserFace) +
		"（动词表自动建卡 " + strconv.Itoa(c.Carded) + "，另有 " + strconv.Itoa(c.Covered) +
		" 项已挂在手写功能卡上）、运行时内部 " + strconv.Itoa(c.Internal) +
		" 项（引擎管道与协调，不建卡，只计入本行）。"
}

// handWrittenCandidates is the live catalog without the auto bridge registry —
// the candidates a verb can be "already covered" by.
func handWrittenCandidates() []Candidate {
	var out []Candidate
	out = append(out, pageCandidates()...)
	out = append(out, settingsCandidates()...)
	out = append(out, mediaActionCandidates()...)
	out = append(out, pluginCandidates()...)
	out = append(out, verbCandidates()...)
	out = append(out, extraVerbCandidates()...)
	return out
}
