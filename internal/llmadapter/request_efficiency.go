package llmadapter

import (
	"sort"

	"github.com/lunitide/lunitide/internal/tokenefficiency"
)

// EfficiencySnapshot is an observation-only report of request preparation.
// It never contains prompt text or private reasoning.
type EfficiencySnapshot struct {
	PolicyVersion string
	BytesBefore   int
	BytesAfter    int
	Disabled      bool
}

func requestVisibleBytes(r Request) int {
	n := 0
	for _, m := range r.Messages {
		n += len(m.Content)
		for _, call := range m.ToolCalls {
			n += len(call.Name) + len(call.Arguments)
		}
	}
	for _, tool := range r.Tools {
		n += len(tool.Name) + len(tool.Description) + len(tool.Schema)
	}
	return n
}

func prepareEfficientRequestReport(in Request, opts Options) (Request, EfficiencySnapshot) {
	before := requestVisibleBytes(in)
	out := prepareEfficientRequest(in, opts)
	return out, EfficiencySnapshot{
		PolicyVersion: "token-efficiency-v1",
		BytesBefore:   before,
		BytesAfter:    requestVisibleBytes(out),
		Disabled:      opts.DisableTokenEfficiency,
	}
}

func attachEfficientRequest(in Request, opts Options) Request {
	out, snap := prepareEfficientRequestReport(in, opts)
	out.Efficiency = snap
	return out
}

// AttachEfficientRequest is the exported observation boundary used by the
// call meter so ledger snapshots match the request the adapter will send.
func AttachEfficientRequest(in Request, opts Options) Request {
	return attachEfficientRequest(in, opts)
}

// prepareEfficientRequest is the single provider-neutral optimization boundary.
// Stable tool order keeps equivalent catalogs identical for provider prefix
// caches. Each tool definition remains complete; authorization/routing and all
// system/user/assistant messages, images and output limits are untouched.
// Copies protect the caller's append-only tool history and concurrent agents.
func prepareEfficientRequest(in Request, opts Options) Request {
	if opts.DisableTokenEfficiency {
		return in
	}
	if len(in.Tools) > 1 {
		// Duplicate names are ambiguous. Preserve the original order so existing
		// validation, rather than this optimization, decides their meaning.
		names := make(map[string]bool, len(in.Tools))
		unique := true
		for _, t := range in.Tools {
			if t.Name == "" || names[t.Name] {
				unique = false
				break
			}
			names[t.Name] = true
		}
		if unique && !sort.SliceIsSorted(in.Tools, func(i, j int) bool { return in.Tools[i].Name < in.Tools[j].Name }) {
			in.Tools = append([]ToolDefinition(nil), in.Tools...)
			sort.SliceStable(in.Tools, func(i, j int) bool { return in.Tools[i].Name < in.Tools[j].Name })
		}
	}
	// Keep every call/result pair even if a repeated query returns identical
	// content: the call ID and temporal observation are part of its evidence.
	namesByCall := make(map[string]string)
	for _, m := range in.Messages {
		for _, call := range m.ToolCalls {
			if call.ID == "" {
				continue
			}
			if prior, exists := namesByCall[call.ID]; exists && prior != call.Name {
				namesByCall[call.ID] = "" // Ambiguous history cannot prove its format.
			} else if !exists {
				namesByCall[call.ID] = call.Name
			}
		}
	}
	copied := false
	for i, m := range in.Messages {
		changed := false
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			callsCopied := false
			for j, call := range m.ToolCalls {
				args := tokenefficiency.CompactToolJSON(string(call.Arguments))
				if args == string(call.Arguments) {
					continue
				}
				if !callsCopied {
					m.ToolCalls = append([]ToolCall(nil), m.ToolCalls...)
					callsCopied = true
				}
				m.ToolCalls[j].Arguments = []byte(args)
				changed = true
			}
		}
		if m.Role == RoleTool && m.ToolCallID != "" && structuredEfficiencyResult(namesByCall[m.ToolCallID]) {
			content := tokenefficiency.CompactToolJSON(m.Content)
			if content != m.Content {
				m.Content = content
				changed = true
			}
		}
		if !changed {
			continue
		}
		if !copied {
			in.Messages = append([]Message(nil), in.Messages...)
			copied = true
		}
		in.Messages[i] = m
	}
	return in
}

// Only outputs with a known structured envelope are eligible. A file-read
// result may itself look like JSON but its literal formatting is evidence.
// Unknown/MCP tools therefore keep their complete original bytes by default.
func structuredEfficiencyResult(name string) bool {
	switch name {
	case "office.generate", "office.inspect", "office.patch", "office.range.patch", "office.image.replace", "office.chart.patch", "office.cache.refresh", "office.deliver", "weather.get":
		return true
	default:
		return false
	}
}
