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
	if companionToolPreapproved("command.run", false) || companionToolPreapproved("user.ask", false) {
		t.Fatal("dangerous and user.ask must not auto-approve")
	}
	if !companionToolPreapproved("workspace.read", true) {
		t.Fatal("low-risk companion tools stay preapproved")
	}
	if companionToolPreapproved("workspace.write", true) || companionToolPreapproved("workspace.edit", true) {
		t.Fatal("full-disk workspace writes must confirm once")
	}
	if !companionToolPreapproved("workspace.write", false) {
		t.Fatal("sandbox workspace writes stay preapproved")
	}
}
