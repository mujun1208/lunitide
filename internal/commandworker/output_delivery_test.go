package commandworker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunBlockedOutputCannotDefeatProcessTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("real subprocess")
	}
	block := make(chan struct{})
	defer close(block)
	spec := helperSpec(t, t.TempDir(), "write", "8388608")
	spec.Timeout = 200 * time.Millisecond
	started := time.Now()
	out, err := Run(context.Background(), spec, nil, func([]byte) { <-block })
	if !errors.Is(err, ErrOutputUnavailable) || !out.Truncated {
		t.Fatalf("blocked consumer reported success: %+v %v", out, err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("blocked callback defeated worker teardown: %s", elapsed)
	}
}

func TestOutputDeliveryPanicIsReported(t *testing.T) {
	delivery, err := newOutputDelivery(func([]byte) { panic("consumer panic") })
	if err != nil {
		t.Fatal(err)
	}
	delivery.write([]byte("x"))
	if err := delivery.finish(); !errors.Is(err, ErrOutputUnavailable) {
		t.Fatalf("panic reported success: %v", err)
	}
}

func TestSpecArgumentOverrideIsExplicitAndBounded(t *testing.T) {
	spec := helperSpec(t, t.TempDir(), "echo", strings.Repeat("a", 2048))
	if err := spec.Validate(); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("default widened: %v", err)
	}
	spec.MaxArgBytes = 16 << 10
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	spec.MaxArgBytes++
	if err := spec.Validate(); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("override unbounded: %v", err)
	}
}
