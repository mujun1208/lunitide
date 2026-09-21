package producthub

import "strings"

var domainNames = map[string]string{
	"dialog": "对话体验", "office": "业务工作台", "assets": "资产与智能",
	"execution": "执行与控制", "foundation": "底座与治理",
}

func BuildGraph(cards []Card) Graph {
	nodes := map[string]GraphNode{}
	var edges []GraphEdge
	add := func(n GraphNode) {
		if n.ID == "" {
			n.ID = n.StableKey
		}
		nodes[n.ID] = n
	}
	link := func(from, to, rel string) {
		if from == "" || to == "" {
			return
		}
		edges = append(edges, GraphEdge{From: from, To: to, Rel: rel})
	}
	add(GraphNode{ID: "product.lunitide", StableKey: "product.lunitide", Type: "Product", Name: "Lunitide"})
	for id, name := range domainNames {
		dk := "domain." + id
		add(GraphNode{ID: dk, StableKey: dk, Type: "Domain", Name: name, Domain: id})
		link("product.lunitide", dk, "contains")
	}
	for _, c := range cards {
		domain := c.Domain
		if domain == "" {
			domain = "foundation"
		}
		modKey := "module." + domain + "." + c.Module
		if c.Module != "" {
			add(GraphNode{ID: modKey, StableKey: modKey, Type: "Module", Name: c.Module, Domain: domain})
			link("domain."+domain, modKey, "contains")
		}
		add(GraphNode{ID: c.StableKey, StableKey: c.StableKey, Type: featureType(c), Name: c.Name, Domain: domain})
		if c.Module != "" {
			link(modKey, c.StableKey, "contains")
		} else {
			link("domain."+domain, c.StableKey, "contains")
		}
		if len(c.Chain.Steps) > 0 && !strings.HasPrefix(c.StableKey, "landscape.") {
			ck := "chain." + c.StableKey
			add(GraphNode{ID: ck, StableKey: ck, Type: "Chain", Name: c.Name + " 链路", Domain: domain})
			link(c.StableKey, ck, "contains")
			for _, step := range c.Chain.Steps {
				sk := ck + ".step." + itoa(step.Index)
				add(GraphNode{ID: sk, StableKey: sk, Type: "Step", Name: step.Name, Domain: domain})
				link(ck, sk, "contains")
			}
		}
		for _, tool := range c.Attributes.Tools {
			tk := "capability.tool." + tool
			add(GraphNode{ID: tk, StableKey: tk, Type: "Capability", Name: tool, Domain: domain})
			link(c.StableKey, tk, "uses")
		}
		for _, cap := range c.Attributes.Capabilities {
			add(GraphNode{ID: cap, StableKey: cap, Type: "Capability", Name: cap, Domain: domain})
			link(c.StableKey, cap, "depends")
		}
		for _, skill := range c.Attributes.Skills {
			add(GraphNode{ID: skill, StableKey: skill, Type: "Skill", Name: skill, Domain: "assets"})
			link(c.StableKey, skill, "uses")
		}
		if strings.Contains(c.StableKey, ".skill") {
			sk := "skill." + c.Module
			add(GraphNode{ID: sk, StableKey: sk, Type: "Skill", Name: c.Name, Domain: "assets"})
			link(c.StableKey, sk, "uses")
		}
		for _, mcp := range c.Attributes.MCPs {
			add(GraphNode{ID: mcp, StableKey: mcp, Type: "Mcp", Name: mcp, Domain: "assets"})
			link(c.StableKey, mcp, "calls")
		}
		if strings.Contains(c.StableKey, ".mcp") {
			mk := "mcp." + c.Module
			add(GraphNode{ID: mk, StableKey: mk, Type: "Mcp", Name: c.Name, Domain: "assets"})
			link(c.StableKey, mk, "calls")
		}
		for _, br := range c.Scaffold.Bridge {
			bk := "bridge." + br
			add(GraphNode{ID: bk, StableKey: bk, Type: "Capability", Name: br, Domain: domain})
			link(c.StableKey, bk, "calls")
		}
		if strings.HasPrefix(c.StableKey, "feature.assets.plugin.") || strings.Contains(c.StableKey, ".plugin.") {
			pk := "plugin." + c.Module
			add(GraphNode{ID: pk, StableKey: pk, Type: "Plugin", Name: c.Name, Domain: "assets"})
			link(c.StableKey, pk, "uses")
		}
		if strings.Contains(c.StableKey, ".expert") {
			ek := "expert." + c.Module
			add(GraphNode{ID: ek, StableKey: ek, Type: "Expert", Name: c.Name, Domain: "assets"})
			link(c.StableKey, ek, "uses")
		}
		if strings.Contains(c.StableKey, ".mcp") {
			for _, mcp := range append([]string{}, c.Attributes.MCPs...) {
				tk := mcp + ".tool"
				add(GraphNode{ID: tk, StableKey: tk, Type: "McpTool", Name: mcp + " tool", Domain: "assets"})
				link(c.StableKey, tk, "calls")
			}
			if len(c.Attributes.MCPs) == 0 {
				tk := "mcptool." + c.Module
				add(GraphNode{ID: tk, StableKey: tk, Type: "McpTool", Name: c.Name + " 工具", Domain: "assets"})
				link(c.StableKey, tk, "calls")
			}
		}
		if strings.HasPrefix(c.StableKey, "landscape.") {
			add(GraphNode{ID: "scenario." + c.StableKey, StableKey: "scenario." + c.StableKey, Type: "Scenario", Name: c.Name, Domain: domain})
			link(c.StableKey, "scenario."+c.StableKey, "contains")
		}
	}
	out := Graph{Edges: edges}
	for _, n := range nodes {
		out.Nodes = append(out.Nodes, n)
	}
	return out
}

func featureType(c Card) string {
	if strings.HasPrefix(c.StableKey, "landscape.") {
		return "Scenario"
	}
	return "Feature"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
