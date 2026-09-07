//go:build windows

package bootstrap

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/app"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/datadir"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/oklog/ulid/v2"
)

// Exercise the released composition root, including capability gates, encrypted
// identity, real SQLite and startup workers. No personal profile or remote peer
// is used; transport callbacks have no configured external MCP processes.
func desktopCompatibilityEngine(t *testing.T) *app.Engine {
	e, _ := desktopCompatibilityEngineAt(t, t.TempDir())
	return e
}

func desktopCompatibilityEngineAt(t *testing.T, path string) (*app.Engine, func()) {
	t.Helper()
	root, err := datadir.PrepareForTest(path)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := secret.NewDPAPIService(root)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := secretlease.NewLocalClient(secrets)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	e, cleanup, err := WireEngine(ctx, EngineDeps{
		DataRoot: root, SecretService: secrets, LeaseClient: lease,
		CursorKey:      []byte("0123456789abcdef0123456789abcdef"),
		Mcp6Registry:   mcp6.NewRegistry(nil, nil, nil),
		StartStdioPool: func(context.Context) {}, CloseStdioPool: func() {}, SetStdioWorkDir: func(string) {},
	})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	var once sync.Once
	closeEngine := func() { once.Do(func() { cancel(); cleanup(); _ = root.Close() }) }
	t.Cleanup(closeEngine)
	return e, closeEngine
}

func TestDesktopCompatibilityExpertKnowledgeAndPluginsSurviveRestart(t *testing.T) {
	path := t.TempDir()
	e, closeEngine := desktopCompatibilityEngineAt(t, path)
	readExperts := func(e *app.Engine) map[string]string {
		var listed struct {
			Experts []struct {
				ID   string `json:"expertId"`
				Name string `json:"name"`
			} `json:"experts"`
		}
		if err := json.Unmarshal(desktopCall(t, e, "expert.list", map[string]any{}), &listed); err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, expert := range listed.Experts {
			out[expert.ID] = expert.Name
			desktopCall(t, e, "expert.knowledge.get", map[string]any{"expertId": expert.ID})
		}
		if len(out) < 10 {
			t.Fatal("expert roster missing")
		}
		return out
	}
	before := readExperts(e)
	plugins := desktopCall(t, e, "plugin.list", map[string]any{})
	closeEngine()
	e, _ = desktopCompatibilityEngineAt(t, path)
	if after := readExperts(e); !reflect.DeepEqual(before, after) {
		t.Fatalf("expert IDs changed on restart: %d -> %d", len(before), len(after))
	}
	if after := desktopCall(t, e, "plugin.list", map[string]any{}); !reflect.DeepEqual(plugins, after) {
		t.Fatal("plugin installs changed on restart")
	}
}

func desktopCall(t *testing.T, e *app.Engine, method string, payload any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	r := bridge.Request{Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(), Method: method, SentAt: time.Now().UTC(), DeadlineMS: 5000, IdempotencyKey: ulid.Make().String(), Payload: raw}
	res := e.Handle(context.Background(), r)
	if !res.OK {
		t.Fatalf("%s failed: %+v", method, res.Error)
	}
	out, err := json.Marshal(res.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDesktopCompatibilityLocalExpertSend(t *testing.T) {
	e := desktopCompatibilityEngine(t)
	var roster struct {
		Items []struct {
			SubjectID string `json:"subjectId"`
			OrgName   string `json:"orgName"`
		} `json:"items"`
	}
	raw := desktopCall(t, e, "people.list", map[string]any{})
	if err := json.Unmarshal(raw, &roster); err != nil {
		t.Fatal(err)
	}
	if len(roster.Items) == 0 {
		t.Fatalf("empty contacts: %s", raw)
	}
	peer := ""
	for _, c := range roster.Items {
		if c.OrgName == "月汐智能体" {
			peer = c.SubjectID
			break
		}
	}
	if peer == "" {
		t.Fatal("built-in expert contact missing")
	}
	var opened struct {
		Thread struct {
			ID string `json:"threadId"`
		} `json:"thread"`
	}
	raw = desktopCall(t, e, "people.thread.open", map[string]any{"peerSubjectId": peer})
	if err := json.Unmarshal(raw, &opened); err != nil || opened.Thread.ID == "" {
		t.Fatalf("open: %s %v", raw, err)
	}
	for _, kind := range []string{"emoji", "image", "text"} {
		t.Run(kind, func(t *testing.T) {
			p := map[string]any{"threadId": opened.Thread.ID, "kind": kind, "body": "你好🙂"}
			if kind == "image" {
				delete(p, "body")
				p["fileName"] = "screenshot.png"
				p["fileMime"] = "image/png"
				p["contentBase64"] = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aONsAAAAASUVORK5CYII="
			}
			desktopCall(t, e, "people.thread.send", p)
		})
	}
}

func TestDesktopCompatibilityModuleEntries(t *testing.T) {
	e := desktopCompatibilityEngine(t)
	for _, method := range []string{"project.list", "skill.list", "plugin.list", "expert.list", "mcp.list", "mcp6.presets.list", "template.list", "meetings.list", "datasource.list", "automation.status", "voice.status", "omni.status", "cc.getConfig"} {
		t.Run(method, func(t *testing.T) { desktopCall(t, e, method, map[string]any{}) })
	}
	var identity struct {
		SubjectID string `json:"subjectId"`
	}
	if err := json.Unmarshal(desktopCall(t, e, "identity.get", map[string]any{}), &identity); err != nil {
		t.Fatal(err)
	}
	desktopCall(t, e, "memory.settings.get", map[string]any{"subjectId": identity.SubjectID})
}
