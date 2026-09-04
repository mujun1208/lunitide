//go:build windows && !race

// See watchdog_test.go: CGO+race cannot link lunitide.syso (.rsrc overflow).

package main

import (
	"testing"
	"time"
)

func TestClaimGatewayInstanceRetryWaitsForRelease(t *testing.T) {
	already, release := claimGatewayInstance()
	if already {
		t.Skip("another Lunitide holds Local\\lunitide-gateway")
	}
	got := make(chan bool, 1)
	go func() {
		stillHeld, release2 := claimGatewayInstanceRetry(2 * time.Second)
		got <- stillHeld
		if !stillHeld {
			release2()
		}
	}()
	time.Sleep(200 * time.Millisecond)
	release()
	if stillHeld := <-got; stillHeld {
		t.Fatal("retry must claim after the dying host drops the mutex")
	}
}

func TestClaimGatewayInstanceRetryGivesUpWhileHeld(t *testing.T) {
	already, release := claimGatewayInstance()
	if already {
		t.Skip("another Lunitide holds Local\\lunitide-gateway")
	}
	defer release()
	stillHeld, _ := claimGatewayInstanceRetry(300 * time.Millisecond)
	if !stillHeld {
		t.Fatal("retry must keep reporting already-running while this process holds the mutex")
	}
}
