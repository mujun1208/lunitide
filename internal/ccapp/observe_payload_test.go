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
