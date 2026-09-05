package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

// guiSomPickPrompt is the SoM chooser. Do not reuse visionDescribePrompt
// (that one asks for OCR/prose).
const guiSomPickPrompt = "Pick exactly one on-screen target. Reply with a single JSON object and nothing else. No prose, no OCR transcript, no description."

type guiExecutor string

const (
	guiExecNone   guiExecutor = "none"
	guiExecGUI    guiExecutor = "gui"
	guiExecVision guiExecutor = "vision"
)

type guiFallbackIn struct {
	ccOn                bool
	isSubagent          bool
	route               TaskRoute
	nodeCount           int
	desktopTypeL0Passed bool
	guiCatalog          bool
	visionCatalog       bool
	alreadyUsed         bool
}

type somPick struct {
	MarkID  string
	X, Y    *float64
	FrameID string
}

type guiFallbackRuntime struct {
	Goal     string
	FrameID  string
	Nodes    int
	VisW     int
	VisH     int
	Images   []gateway.Image
	Observe  func() (frameID string, nodes, visW, visH int, images []gateway.Image, err error)
	Complete func(exec guiExecutor, images []gateway.Image, prompt string) (string, error)
	HasHit   func(id string) bool
	Click    func(args json.RawMessage, allowPixels bool) (toolruntime.Result, error)
}

func shouldAttemptGUIFallback(in guiFallbackIn) bool {
	if !in.ccOn || in.isSubagent || in.alreadyUsed || in.desktopTypeL0Passed {
		return false
	}
	switch in.route {
	case RouteR2, RouteR3:
		return true
	default:
		return false
	}
}

func pickGUIFallback(in guiFallbackIn) guiExecutor {
	if !shouldAttemptGUIFallback(in) {
		return guiExecNone
	}
	if in.nodeCount > 0 && in.guiCatalog {
		return guiExecGUI
	}
	if in.nodeCount > 0 && in.visionCatalog {
		return guiExecVision
	}
	if in.nodeCount == 0 && in.guiCatalog {
		return guiExecGUI
	}
	return guiExecNone
}

func desktopToolFailedForGUI(name, summary string, toolErr error) bool {
	if name != "desktop.type" && name != "computer.act" {
		return false
	}
	if toolErr == nil && !strings.Contains(summary, "ok:false") {
		return false
	}
	if l0, ok := extractL0(summary); ok && l0.Passed {
		return false
	}
	return true
}

func desktopTypePassedL0(name, summary string) bool {
	if name != "desktop.type" {
		return false
	}
	l0, ok := extractL0(summary)
	return ok && l0.Passed
}

func guiSomPickUserPrompt(goal, frameID string, emptyTree bool) string {
	var b strings.Builder
	b.WriteString(guiSomPickPrompt)
	if emptyTree {
		b.WriteString("\nThe UI tree is empty. Reply {\"x\":<0-1 or 1-1000>,\"y\":<same>,\"frameId\":\"")
		b.WriteString(strings.TrimSpace(frameID))
		b.WriteString("\"}.")
	} else {
		b.WriteString("\nMarks such as B1 are painted on the screenshot. Reply {\"markId\":\"B1\"} using one painted id.")
	}
	if hint := strings.TrimSpace(goal); hint != "" {
		b.WriteString("\n\nUser goal:\n")
		b.WriteString(hint)
	}
	return b.String()
}

func firstJSONObject(raw string) (string, error) {
	start := strings.Index(raw, "{")
	if start < 0 {
		return "", fmt.Errorf("no json object")
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : i+1], nil
			}
		}
	}
	return "", fmt.Errorf("unbalanced json object")
}

func validSOMMarkID(id string) bool {
	id = strings.ToUpper(strings.TrimSpace(id))
	if len(id) < 2 || len(id) > 8 {
		return false
	}
	if id[0] < 'A' || id[0] > 'Z' {
		return false
	}
	for _, c := range id[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func parseSOMPick(raw string, emptyTree bool, wantFrame string) (somPick, error) {
	js, err := firstJSONObject(raw)
	if err != nil {
		return somPick{}, err
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(js), &obj) != nil {
		return somPick{}, fmt.Errorf("invalid som json")
	}
	wantFrame = strings.TrimSpace(wantFrame)
	if emptyTree {
		x, okX := somJSONNumber(obj["x"])
		y, okY := somJSONNumber(obj["y"])
		frame, okF := somJSONString(obj["frameId"])
		if !okX || !okY || !okF {
			return somPick{}, fmt.Errorf("empty tree requires x,y,frameId")
		}
		if strings.TrimSpace(frame) != wantFrame {
			return somPick{}, fmt.Errorf("frameId mismatch")
		}
		if x < 0 || x > 1000 || y < 0 || y > 1000 {
			return somPick{}, fmt.Errorf("xy out of range")
		}
		return somPick{X: &x, Y: &y, FrameID: strings.TrimSpace(frame)}, nil
	}
	mark, ok := somJSONString(obj["markId"])
	if !ok {
		return somPick{}, fmt.Errorf("markId must be a string")
	}
	mark = strings.ToUpper(strings.TrimSpace(mark))
	if !validSOMMarkID(mark) {
		return somPick{}, fmt.Errorf("invalid markId")
	}
	return somPick{MarkID: mark}, nil
}

func somJSONString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

func somJSONNumber(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var n float64
	if json.Unmarshal(raw, &n) != nil {
		return 0, false
	}
	return n, true
}

func mapGUICoord(v float64, size int) (int, error) {
	if size <= 0 {
		return 0, fmt.Errorf("missing vision size")
	}
	if v < 0 || v > 1000 {
		return 0, fmt.Errorf("xy out of range")
	}
	var frac float64
	if v <= 1 {
		frac = v
	} else {
		frac = v / 1000
	}
	px := int(math.Round(frac * float64(size)))
	if px >= size {
		px = size - 1
	}
	if px < 0 {
		px = 0
	}
	return px, nil
}

func buildGUIClickArgs(pick somPick, emptyTree bool, visW, visH int) (json.RawMessage, error) {
	if !emptyTree {
		if strings.TrimSpace(pick.MarkID) == "" {
			return nil, fmt.Errorf("missing markId")
		}
		return json.Marshal(map[string]any{"action": "click", "id": pick.MarkID})
	}
	if pick.X == nil || pick.Y == nil {
		return nil, fmt.Errorf("missing xy")
	}
	x, err := mapGUICoord(*pick.X, visW)
	if err != nil {
		return nil, err
	}
	y, err := mapGUICoord(*pick.Y, visH)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"action": "click", "x": x, "y": y, "frameId": pick.FrameID})
}

func runGUIFallback(in guiFallbackIn, rt guiFallbackRuntime) (toolruntime.Result, json.RawMessage, bool) {
	if !shouldAttemptGUIFallback(in) {
		return toolruntime.Result{}, nil, false
	}
	frameID, nodes, visW, visH := strings.TrimSpace(rt.FrameID), rt.Nodes, rt.VisW, rt.VisH
	images := rt.Images
	if frameID == "" || len(images) == 0 {
		if rt.Observe == nil {
			return toolruntime.Result{}, nil, false
		}
		var err error
		frameID, nodes, visW, visH, images, err = rt.Observe()
		if err != nil || strings.TrimSpace(frameID) == "" {
			return toolruntime.Result{}, nil, false
		}
		frameID = strings.TrimSpace(frameID)
	}
	in.nodeCount = nodes
	exec := pickGUIFallback(in)
	if exec == guiExecNone {
		return toolruntime.Result{}, nil, false
	}
	if rt.Complete == nil {
		return toolruntime.Result{}, nil, false
	}
	raw, err := rt.Complete(exec, images, guiSomPickUserPrompt(rt.Goal, frameID, nodes == 0))
	if err != nil {
		return toolruntime.Result{}, nil, true
	}
	pick, err := parseSOMPick(raw, nodes == 0, frameID)
	if err != nil {
		return toolruntime.Result{}, nil, true
	}
	if pick.MarkID != "" && (rt.HasHit == nil || !rt.HasHit(pick.MarkID)) {
		return toolruntime.Result{}, nil, true
	}
	args, err := buildGUIClickArgs(pick, nodes == 0, visW, visH)
	if err != nil {
		return toolruntime.Result{}, nil, true
	}
	if rt.Click == nil {
		return toolruntime.Result{}, nil, true
	}
	res, clickErr := rt.Click(args, nodes == 0)
	if clickErr != nil {
		summary := clickErr.Error()
		if !strings.HasPrefix(summary, "ok:false") {
			summary = "ok:false\n" + summary
		}
		return toolruntime.Result{Output: summary}, args, true
	}
	return res, args, true
}

func (e *Engine) guiFallbackCatalogFlags(ctx context.Context, skipModel string) (gui, vision bool) {
	if e == nil || e.providers == nil {
		return false, false
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return false, false
	}
	return len(e.preferBoundCatalog(ctx, "gui", provider.CatalogForKind(items, provider.KindGUI))) > 0,
		len(e.preferBoundCatalog(ctx, "vision", provider.VisionDescribeCatalog(items, skipModel))) > 0
}

func (e *Engine) completeSOMPick(ctx context.Context, exec guiExecutor, images []gateway.Image, prompt, skipModel string) (string, error) {
	if e == nil || e.providers == nil {
		return "", fmt.Errorf("no providers")
	}
	if len(images) == 0 {
		return "", fmt.Errorf("no screenshot")
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return "", err
	}
	var catalog []provider.CatalogEntry
	switch exec {
	case guiExecGUI:
		catalog = e.preferBoundCatalog(ctx, "gui", provider.CatalogForKind(items, provider.KindGUI))
	case guiExecVision:
		catalog = e.preferBoundCatalog(ctx, "vision", provider.VisionDescribeCatalog(items, skipModel))
	default:
		return "", fmt.Errorf("no som executor")
	}
	if len(catalog) == 0 {
		return "", fmt.Errorf("empty som catalog")
	}
	req := gateway.Request{
		Messages:    []gateway.Message{{Role: gateway.RoleUser, Content: prompt}},
		Images:      images,
		MaxTokens:   128,
		MaxAttempts: 1,
	}
	var last error
	for _, entry := range catalog {
		req.Model = entry.Model.ModelID
		var text string
		leaseErr := e.withProviderLease(ctx, entry.Provider, secretlease.OperationChat, func(op context.Context, secret []byte) error {
			a, adapterErr := e.adapter(op, entry.Provider)
			if adapterErr != nil {
				return adapterErr
			}
			out, completeErr := a.Complete(op, secret, req)
			if completeErr != nil {
				return completeErr
			}
			text = strings.TrimSpace(out.Message.Content)
			if text == "" {
				return fmt.Errorf("empty som pick")
			}
			return nil
		})
		if leaseErr == nil && text != "" {
			return text, nil
		}
		last = leaseErr
	}
	if last == nil {
		last = fmt.Errorf("som complete failed")
	}
	return "", last
}

func (e *Engine) tryGUIFallback(ctx context.Context, mode executionMode, sessionID, goal, chatModel string, state *streamState, images []gateway.Image, alreadyUsed, desktopTypeL0Passed bool) (toolruntime.Result, json.RawMessage, bool) {
	if e == nil || state == nil || !e.computerControlEnabled() {
		return toolruntime.Result{}, nil, false
	}
	gui, vision := e.guiFallbackCatalogFlags(ctx, chatModel)
	in := guiFallbackIn{
		ccOn:                true,
		isSubagent:          false,
		route:               state.taskRoute,
		desktopTypeL0Passed: desktopTypeL0Passed,
		guiCatalog:          gui,
		visionCatalog:       vision,
		alreadyUsed:         alreadyUsed,
	}
	rt := guiFallbackRuntime{
		Goal:   goal,
		Images: images,
		Observe: func() (string, int, int, int, []gateway.Image, error) {
			res, err := e.executeUserToolWithCompanion(ctx, mode, sessionID, "computer.act", json.RawMessage(`{"action":"observe"}`), nil, state.companion)
			if err != nil {
				return "", 0, 0, 0, images, err
			}
			if len(res.VisionData) > 0 {
				images = appendCaptureVision(images, res.VisionMIME, res.VisionData)
			}
			if e.ccctrl == nil {
				return "", 0, 0, 0, images, fmt.Errorf("cc unavailable")
			}
			frameID, nodes := e.ccctrl.ObservedSnapshot()
			visW, visH := e.ccctrl.VisionSize()
			if strings.TrimSpace(frameID) == "" {
				return "", 0, 0, 0, images, fmt.Errorf("observe produced no frame")
			}
			return frameID, nodes, visW, visH, images, nil
		},
		Complete: func(exec guiExecutor, imgs []gateway.Image, prompt string) (string, error) {
			return e.completeSOMPick(ctx, exec, imgs, prompt, chatModel)
		},
		HasHit: func(id string) bool {
			return e.ccctrl != nil && e.ccctrl.HasObservedID(id)
		},
		Click: func(args json.RawMessage, allowPixels bool) (toolruntime.Result, error) {
			if allowPixels && e.ccctrl != nil {
				e.ccctrl.SetAllowGUIPixels(true)
				defer e.ccctrl.SetAllowGUIPixels(false)
			}
			return e.executeUserToolWithCompanion(ctx, mode, sessionID, "computer.act", args, nil, state.companion)
		},
	}
	if e.ccctrl != nil {
		rt.FrameID, rt.Nodes = e.ccctrl.ObservedSnapshot()
		rt.VisW, rt.VisH = e.ccctrl.VisionSize()
	}
	return runGUIFallback(in, rt)
}
