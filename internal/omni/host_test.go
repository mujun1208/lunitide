package omni

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

type countingTripper struct{ n int }

func (c *countingTripper) RoundTrip(*http.Request) (*http.Response, error) {
	c.n++
	return nil, errors.New("blocked")
}

func TestSnapshotSkipsHealthWhenRuntimeMissing(t *testing.T) {
	h := NewHost(t.TempDir())
	trip := &countingTripper{}
	h.HTTP = &http.Client{Transport: trip, Timeout: time.Second}
	snap := h.Snapshot()
	if trip.n != 0 {
		t.Fatalf("health probes = %d; missing runtime must not ping loopback", trip.n)
	}
	if snap["hostState"] != HostMissingModel {
		t.Fatalf("hostState = %v", snap["hostState"])
	}
}

func TestSnapshotProbesHealthWhenRuntimeFound(t *testing.T) {
	h := NewHost(t.TempDir())
	h.Finder = func() string { return "llama-omni-server" }
	h.Present = func() bool { return true }
	trip := &countingTripper{}
	h.HTTP = &http.Client{Transport: trip, Timeout: time.Second}
	_ = h.Snapshot()
	if trip.n == 0 {
		t.Fatal("expected a loopback health probe when the runtime is on disk")
	}
}
