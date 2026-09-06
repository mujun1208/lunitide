//go:build windows

package diagramrender

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in integration uses the built private worker, never the user's main UI.
func TestNativeIsolatedDiagramRenderingAndCPULoopDeadline(t *testing.T) {
	executable, assets := os.Getenv("LUNITIDE_DIAGRAM_TEST_EXECUTABLE"), os.Getenv("LUNITIDE_DIAGRAM_TEST_ASSETS")
	if executable == "" || assets == "" {
		t.Skip("set isolated desktop worker and renderer asset paths")
	}
	for index, source := range []string{"flowchart LR\nA[完整图表]-->B[结果]", "sequenceDiagram\nAlice->>Bob: hello\nBob-->>Alice: done", "classDiagram\nAnimal <|-- Duck\nAnimal : +eat()"} {
		began := time.Now()
		h := New(assets)
		h.Executable = executable
		ctx, cancel := context.WithTimeout(context.Background(), Timeout)
		result, err := h.render(ctx, Request{Source: source, Config: json.RawMessage(`{"theme":"default","flowchart":{"htmlLabels":false}}`)})
		cancel()
		if err != nil || !strings.Contains(result.SVG, "<svg") {
			t.Fatalf("native %s: %v", source, err)
		}
		kind := []string{"flowchart", "sequence", "class"}[index]
		output := "(no evidence directory requested)"
		if evidence := os.Getenv("LUNITIDE_DIAGRAM_TEST_EVIDENCE"); evidence != "" {
			if err = os.MkdirAll(evidence, 0700); err != nil {
				t.Fatal(err)
			}
			output = filepath.Join(evidence, "phase2-diagram-"+kind+".svg")
			if err = os.WriteFile(output, []byte(result.SVG), 0600); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("diagram=%s bytes=%d elapsed=%s output=%s", kind, len(result.SVG), time.Since(began), output)
	}
	// Deliberately hostile JS in a private test asset tree exercises a stuck
	// renderer CPU, not an asynchronous promise that politely honors a timer.
	hostile := t.TempDir()
	if err := os.WriteFile(filepath.Join(hostile, "diagram-worker.html"), []byte(`<html><body><script>for(;;){}</script></body></html>`), 0600); err != nil {
		t.Fatal(err)
	}
	h := New(hostile)
	h.Executable = executable
	began := time.Now()
	_, err := h.render(context.Background(), Request{Source: "flowchart LR\nA-->B", Config: json.RawMessage(`{}`)})
	t.Logf("independent renderer CPU loop reclaimed after=%s result=%v", time.Since(began), err)
	if err == nil || time.Since(began) > Timeout+4*time.Second {
		t.Fatalf("CPU loop escaped deadline: %v elapsed=%s", err, time.Since(began))
	}
}
