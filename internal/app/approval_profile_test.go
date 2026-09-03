package app

import "testing"

func TestApprovalProfileDangerous(t *testing.T) {
	if approvalProfileDangerous("workspace.read") || approvalProfileDangerous("web.search") {
		t.Fatal("low-risk tools must not be dangerous")
	}
	for _, name := range []string{"command.run", "desktop.open", "desktop.type", "cc.mouse_click", "im.send", "computer.act"} {
		if !approvalProfileDangerous(name) {
			t.Fatalf("%s must be dangerous", name)
		}
	}
	if companionToolPreapproved("command.run", false, false) || companionToolPreapproved("user.ask", false, false) {
		t.Fatal("dangerous and user.ask must not auto-approve")
	}
	if !companionToolPreapproved("workspace.read", true, false) {
		t.Fatal("low-risk companion tools stay preapproved")
	}
	if companionToolPreapproved("workspace.write", true, false) || companionToolPreapproved("workspace.edit", true, false) {
		t.Fatal("full-disk workspace writes must confirm once")
	}
	if !companionToolPreapproved("workspace.write", false, false) {
		t.Fatal("sandbox workspace writes stay preapproved")
	}
	// Standing computer-control enable pre-authorizes launch-shaped desktop
	// tools so a voice turn does not stall on an un-tappable approval.
	if companionToolPreapproved("desktop.open", false, false) {
		t.Fatal("desktop.open must confirm when computer control is off")
	}
	if !companionToolPreapproved("desktop.open", false, true) {
		t.Fatal("desktop.open must be preapproved once computer control is on")
	}
	if !companionToolPreapproved("media.play", false, true) {
		t.Fatal("media.play must be preapproved once computer control is on")
	}
	// The truly sensitive surface stays gated even with CC enabled.
	if companionToolPreapproved("desktop.type", false, true) || companionToolPreapproved("cc.mouse_click", false, true) || companionToolPreapproved("computer.act", false, true) {
		t.Fatal("pixel/keyboard desktop tools must still confirm even with CC on")
	}
}
