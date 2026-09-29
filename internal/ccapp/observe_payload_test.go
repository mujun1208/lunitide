package ccapp

import (
	"strings"
	"testing"
)

func TestObservePayloadDoesNotBuryControlsInInternalURLs(t *testing.T) {
	nodes := []UINode{{ID: "L1", Role: "link", Name: "Recommended", Value: "app://page?config=" + strings.Repeat("x", 10000)}, {ID: "B1", Role: "button", Name: "Play"}, {ID: "E1", Role: "edit", Value: "https://example.com"}}
	payload := observeUIPayload(nodes, 80, "frame-1")
	got := payload["nodes"].([]UINode)
	if got[0].Value != "" || got[1].Name != "Play" || got[2].Value != "https://example.com" {
		t.Fatalf("bad visible nodes: %+v", got)
	}
	if nodes[0].Value == "" || got[0].ID != nodes[0].ID {
		t.Fatal("internal hit data or identifiers changed")
	}
}

func TestObservePayloadCompactsWithoutLosingChoices(t *testing.T) {
	var nodes []UINode
	// 12 identical list rows: keep 3 (their IDs intact), count the rest.
	for i := 0; i < 12; i++ {
		nodes = append(nodes, UINode{ID: "L" + string(rune('A'+i)), Role: "listitem", Name: "", X: 10, Y: 20 * i, W: 200, H: 18})
	}
	// Nameless panes carry nothing the model can act on.
	nodes = append(nodes, UINode{ID: "O1", Role: "other"}, UINode{ID: "O2", Role: "other", Name: "  "})
	// Long UIA names are capped but the control stays.
	long := strings.Repeat("这是一段很长的正文", 30)
	nodes = append(nodes, UINode{ID: "T1", Role: "button", Name: long})
	nodes = append(nodes, UINode{ID: "B1", Role: "button", Name: "保存"}, UINode{ID: "B2", Role: "button", Name: "取消"})

	payload := observeUIPayload(nodes, 80, "frm_1")
	got := payload["nodes"].([]UINode)
	if payload["count"] != len(nodes) {
		t.Fatalf("count must stay the raw tree size: %v", payload["count"])
	}
	if payload["returned"] != len(got) {
		t.Fatalf("returned must match the visible nodes: %v vs %d", payload["returned"], len(got))
	}
	ids := map[string]bool{}
	for _, n := range got {
		ids[n.ID] = true
	}
	for _, want := range []string{"LA", "LB", "LC", "T1", "B1", "B2"} {
		if !ids[want] {
			t.Fatalf("compaction dropped an actionable node %s: %+v", want, got)
		}
	}
	for _, gone := range []string{"LD", "LL", "O1", "O2"} {
		if ids[gone] {
			t.Fatalf("%s should be compacted away", gone)
		}
	}
	if dropped, _ := payload["compacted"].(int); dropped != 9+2 {
		t.Fatalf("compacted count: %v", payload["compacted"])
	}
	for _, n := range got {
		if n.ID == "T1" {
			if r := []rune(n.Name); len(r) > observeNameRunes+1 || !strings.HasSuffix(n.Name, "…") {
				t.Fatalf("long name not capped: %d runes", len(r))
			}
		}
	}
	if nodes[len(nodes)-3].Name != long {
		t.Fatal("caller's node slice must not be mutated")
	}

	// A tree that is nothing but unnamed panes must not compact to empty:
	// an empty tree is the GUI-loop trigger and would misfire.
	panes := []UINode{{ID: "O1", Role: "other"}, {ID: "O2", Role: "other"}}
	if got := observeUIPayload(panes, 80, "frm_2")["nodes"].([]UINode); len(got) != 2 {
		t.Fatalf("all-pane tree must be returned as-is, got %d", len(got))
	}
	huge := "app://x?c=" + strings.Repeat("z", 4000)
	urlPanes := []UINode{{ID: "O1", Role: "other", Value: huge}}
	gotURL := observeUIPayload(urlPanes, 80, "frm_3")["nodes"].([]UINode)
	if len(gotURL) != 1 || strings.Contains(gotURL[0].Value, strings.Repeat("z", 300)) {
		t.Fatalf("all-pane fallback must still strip huge URLs: %+v", gotURL)
	}
	if urlPanes[0].Value != huge {
		t.Fatal("caller node must stay intact")
	}
}
