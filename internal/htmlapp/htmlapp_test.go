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
	} else if !strings.Contains(err.Error(), "checklist") {
		t.Fatalf("unknown template must list checklist: %v", err)
	}
	html, err = Render("checklist", `<script>x</script>`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "lunitide-checklist-") || !strings.Contains(html, "id=\"list\"") || strings.Contains(html, "<script>x</script>") {
		t.Fatal("checklist template missing or title not escaped")
	}
	html, err = Render("timer", `<script>x</script>`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "id=\"clock\"") || strings.Contains(html, "<script>x</script>") {
		t.Fatal("timer template missing or title not escaped")
	}
}
