package htmlapp

import (
	"strings"
	"testing"
)

func TestRenderPenaltyShootout(t *testing.T) {
	html, err := Render("penalty-shootout", "世界杯点球大战")
	if err != nil {
		t.Fatal(err)
	}
	for _, need := range []string{"<canvas", "WORLD CUP", "世界杯点球大战", "function shoot"} {
		if !strings.Contains(html, need) {
			t.Fatalf("missing %q in generated game", need)
		}
	}
	if strings.Contains(html, "{{TITLE}}") {
		t.Fatal("title placeholder leaked")
	}
	if len(html) > 50<<10 {
		t.Fatalf("game HTML too large for preview: %d", len(html))
	}
}

func TestRenderEscapesTitleAndRejectsUnknown(t *testing.T) {
	html, err := Render("penalty-shootout", `<script>x</script>`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>x</script>") || !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("title was not escaped: %s", html[:200])
	}
	if _, err := Render("snake", "x"); err == nil {
		t.Fatal("unknown template accepted")
	}
	html, err = Render("timer", `<script>x</script>`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "id=\"clock\"") || strings.Contains(html, "<script>x</script>") {
		t.Fatal("timer template missing or title not escaped")
	}
}
