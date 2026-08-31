package main

import "testing"

func TestEngineWatchShouldRelaunch(t *testing.T) {
	if engineWatchShouldRelaunch(true, true, 12, false) {
		t.Fatal("shutdown must not relaunch")
	}
	if !engineWatchShouldRelaunch(false, true, 12, true) {
		t.Fatal("broken RPC must relaunch")
	}
	if !engineWatchShouldRelaunch(false, false, 12, false) {
		t.Fatal("dead engine pid must relaunch")
	}
	if engineWatchShouldRelaunch(false, false, 12, true) {
		t.Fatal("healthy reconnect must leave the engine")
	}
	if engineWatchShouldRelaunch(false, false, 0, false) {
		t.Fatal("unknown pid is not a death signal")
	}
}
