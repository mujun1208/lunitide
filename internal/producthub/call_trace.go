package producthub

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type callHop struct {
	Method   string
	Handler  string
	Branch   bool
	Dispatch bool
	Steps    []string
	Missing  []string
}

type callIndex struct {
	handlers   map[string]string
	runtime    map[string]bool
	runtimeSrc string
	bodies     map[string]string
	sources    map[string]string
	funcs      map[string]bool
	ready      bool
}

type callHit struct {
	pos  int
	name string
	bare bool
}

var (
	registryHop  = regexp.MustCompile(`bridge\.Method\("([^"]+)"\):\s*(\w+)`)
	constHop     = regexp.MustCompile(`bridge\.(Method[A-Za-z0-9]+):\s*(\w+)`)
	quotedHop    = regexp.MustCompile(`(?m)^\s*"([^"]+\.[^"]+)":\s*(\w+),`)
	methodConst  = regexp.MustCompile(`\b(Method[A-Za-z0-9]+)\s+Method\s*=\s*"([^"]+)"`)
	runtimeCase  = regexp.MustCompile(`case "([^"]+)":`)
	dottedCase   = regexp.MustCompile(`case "([^"]+\.[^"]+)":`)
	selectorCall = regexp.MustCompile(`\.([A-Za-z_][A-Za-z0-9_]*)\(`)
	bareCall     = regexp.MustCompile(`(?:^|[^.\w])([A-Za-z_][A-Za-z0-9_]*)\(`)
	assignLeft   = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*(?:\s*,\s*[A-Za-z_][A-Za-z0-9_]*)*)\s*(?::=|=)`)
	varBind      = regexp.MustCompile(`(?m)^var\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	nextBranch   = regexp.MustCompile(`(?m)^[ \t]*(?:if r\.Method == "|case "|default:)`)
	nextFunc     = regexp.MustCompile(`(?m)^func `)
	funcDef      = regexp.MustCompile(`func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	callMu       sync.Mutex
	callCached   callIndex
	callRoot     string
	callStamp    time.Time
)

func traceCalls(cards []Card) (map[string]callHop, bool) {
	idx := loadCallIndex()
	if !idx.ready {
		return nil, false
	}
	out := map[string]callHop{}
	for _, c := range cards {
		for _, method := range claimedBridges(c) {
			if _, seen := out[method]; seen {
				continue
			}
			out[method] = idx.lookup(method)
		}
	}
	return out, true
}

func (idx callIndex) lookup(method string) callHop {
	if fn, ok := idx.handlers[method]; ok {
		return idx.hop(method, fn, idx.bodies[fn])
	}
	if idx.runtime[method] {
		hop := callHop{Method: method, Handler: "toolruntime", Branch: true}
		hop.Steps, hop.Missing = idx.callsIn(branchBody(idx.runtimeSrc, method))
		return hop
	}
	if fn := aliasHandler(method); fn != "" {
		return idx.hop(method, fn, idx.bodies[fn])
	}
	return callHop{Method: method}
}

func (idx callIndex) hop(method, fn, fileText string) callHop {
	if method == "computer.control" {
		return callHop{Method: method, Handler: "ExecuteTool", Dispatch: true}
	}
	if src := idx.sources[method]; src != "" {
		fileText = src
	}
	hop := callHop{Method: method, Handler: fn, Branch: strings.Contains(fileText, `"`+method+`"`)}
	body := functionBody(fileText, fn)
	if hop.Branch {
		if sliced := branchBody(fileText, method); sliced != "" {
			body = sliced
		}
	}
	body = idx.followSingleCall(body)
	hop.Steps, hop.Missing = idx.callsIn(body)
	return hop
}

func (idx callIndex) followSingleCall(body string) string {
	steps, _ := idx.callsIn(body)
	if len(steps) != 1 {
		return body
	}
	inner := functionBody(idx.bodies[steps[0]], steps[0])
	if inner == "" || inner == body {
		return body
	}
	return inner
}

func (idx callIndex) callsIn(body string) (steps, missing []string) {
	var hits []callHit
	for _, loc := range selectorCall.FindAllStringSubmatchIndex(body, -1) {
		hits = append(hits, callHit{pos: loc[0], name: body[loc[2]:loc[3]]})
	}
	for _, loc := range bareCall.FindAllStringSubmatchIndex(body, -1) {
		hits = append(hits, callHit{pos: loc[0], name: body[loc[2]:loc[3]], bare: true})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].pos < hits[j].pos })
	seen := map[string]bool{}
	locals := assignedNames(body)
	for _, hit := range hits {
		if hit.name == "" || seen[hit.name] || skipCall(hit.name) {
			continue
		}
		seen[hit.name] = true
		steps = append(steps, hit.name)
		if hit.bare && !idx.funcs[hit.name] && !locals[hit.name] {
			missing = append(missing, hit.name)
		}
	}
	return steps, missing
}

func branchBody(fileText, method string) string {
	markers := []string{`r.Method == "` + method + `"`, `case "` + method + `":`}
	start, markLen := -1, 0
	for _, marker := range markers {
		if i := strings.Index(fileText, marker); i >= 0 && (start < 0 || i < start) {
			start, markLen = i, len(marker)
		}
	}
	if start < 0 {
		return ""
	}
	rest := fileText[start+markLen:]
	next := len(rest)
	if loc := nextBranch.FindStringIndex(rest); loc != nil {
		next = loc[0]
	}
	return rest[:next]
}

func loadCallIndex() callIndex {
	root := FindProductRoot()
	if root == "" {
		return callIndex{}
	}
	stamp := newestGo(filepath.Join(root, "internal"))
	callMu.Lock()
	defer callMu.Unlock()
	if callCached.ready && callRoot == root && !stamp.After(callStamp) {
		return callCached
	}
	callCached = buildCallIndex(root)
	callRoot = root
	callStamp = stamp
	return callCached
}

func buildCallIndex(root string) callIndex {
	registry := readText(filepath.Join(root, "internal", "app", "handlers_registry.go"))
	runtime := readText(filepath.Join(root, "internal", "toolruntime", "runtime.go"))
	if registry == "" && runtime == "" {
		return callIndex{}
	}
	idx := callIndex{
		handlers:   map[string]string{},
		runtime:    map[string]bool{},
		runtimeSrc: runtime,
		bodies:     map[string]string{},
		sources:    map[string]string{},
		funcs:      map[string]bool{},
		ready:      true,
	}
	need := map[string]bool{}
	consts := map[string]string{}
	schema := readText(filepath.Join(root, "internal", "bridge", "schema_generated.go"))
	for _, hit := range methodConst.FindAllStringSubmatch(schema, -1) {
		consts[hit[1]] = hit[2]
	}
	add := func(method, fn string) {
		if method == "" || fn == "" {
			return
		}
		if _, ok := idx.handlers[method]; ok {
			return
		}
		idx.handlers[method] = fn
		need[fn] = true
	}
	for _, hit := range registryHop.FindAllStringSubmatch(registry, -1) {
		add(hit[1], hit[2])
	}
	for _, hit := range constHop.FindAllStringSubmatch(registry, -1) {
		add(consts[hit[1]], hit[2])
	}
	for _, hit := range quotedHop.FindAllStringSubmatch(registry, -1) {
		add(hit[1], hit[2])
	}
	for _, hit := range runtimeCase.FindAllStringSubmatch(runtime, -1) {
		if strings.Contains(hit[1], ".") {
			idx.runtime[hit[1]] = true
		}
	}
	for _, method := range []string{
		"computer.control",
		"media.create", "media.clear", "media.move", "media.remove", "media.jump",
		"media.pause", "media.stop", "media.toggle", "media.next", "media.previous",
		"media.seek", "media.set_volume", "media.mute", "media.unmute",
	} {
		if idx.runtime[method] {
			continue
		}
		add(method, aliasHandler(method))
	}
	_ = filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body := readText(path)
		for _, hit := range funcDef.FindAllStringSubmatch(body, -1) {
			idx.funcs[hit[1]] = true
			if idx.bodies[hit[1]] == "" {
				idx.bodies[hit[1]] = body
			}
		}
		for _, hit := range varBind.FindAllStringSubmatch(body, -1) {
			idx.funcs[hit[1]] = true
		}
		for _, loc := range dottedCase.FindAllStringSubmatchIndex(body, -1) {
			method := body[loc[2]:loc[3]]
			if _, ok := idx.handlers[method]; ok || idx.runtime[method] {
				continue
			}
			lookback := loc[0] - 2000
			if lookback < 0 {
				lookback = 0
			}
			if !strings.Contains(body[lookback:loc[0]], "r.Method") {
				continue
			}
			fn := funcNameBefore(body, loc[0])
			add(method, fn)
			if fn != "" {
				idx.sources[method] = body
			}
		}
		for fn := range need {
			if idx.bodies[fn] == "" && definesFunc(body, fn) {
				idx.bodies[fn] = body
			}
		}
		return nil
	})
	return idx
}

func aliasHandler(method string) string {
	if method == "computer.control" {
		return "ExecuteTool"
	}
	action, ok := strings.CutPrefix(method, "media.")
	if !ok || action == "" || strings.Contains(action, ".") {
		return ""
	}
	switch action {
	case "create":
		return "handleMediaSessionCreate"
	case "clear", "move", "remove", "jump":
		return "handleMediaQueueCommand"
	case "pause", "stop", "toggle", "next", "prev", "previous", "seek", "mute", "unmute", "set_volume":
		return "handleMediaSessionCommand"
	default:
		return ""
	}
}

func definesFunc(body, fn string) bool {
	return strings.Contains(body, "func "+fn+"(") || strings.Contains(body, ") "+fn+"(")
}

func funcNameBefore(body string, pos int) string {
	name := ""
	for _, loc := range funcDef.FindAllStringSubmatchIndex(body, -1) {
		if loc[0] >= pos {
			break
		}
		name = body[loc[2]:loc[3]]
	}
	return name
}

func assignedNames(body string) map[string]bool {
	out := map[string]bool{}
	for _, match := range assignLeft.FindAllStringSubmatch(body, -1) {
		for _, part := range strings.Split(match[1], ",") {
			name := strings.TrimSpace(part)
			if name != "" && name != "_" {
				out[name] = true
			}
		}
	}
	return out
}

func functionBody(fileText, fn string) string {
	markers := []string{"func " + fn + "(", ") " + fn + "("}
	start := -1
	for _, marker := range markers {
		if i := strings.Index(fileText, marker); i >= 0 && (start < 0 || i < start) {
			start = i
		}
	}
	if start < 0 {
		return ""
	}
	rest := fileText[start:]
	next := len(rest)
	if loc := nextFunc.FindStringIndex(rest[1:]); loc != nil {
		next = 1 + loc[0]
	}
	return rest[:next]
}

func newestGo(root string) time.Time {
	var newest time.Time
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest
}

func skipCall(name string) bool {
	switch name {
	case "append", "cap", "close", "complex", "copy", "delete", "imag", "len", "make", "new", "panic", "print", "println", "real", "recover", "min", "max", "clear",
		"bool", "byte", "error", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64", "rune", "string", "uintptr",
		"if", "for", "switch", "return", "func", "map", "chan", "var", "type", "struct", "interface", "select", "defer", "go", "range", "else", "case", "default", "break", "continue":
		return true
	default:
		return len(name) < 4
	}
}
