//go:build windows

package voice

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The refiner against the real thing.
//
// Everything in refiner_test.go runs against a stand-in, which is right for
// the wire format and the fallback rules and useless for the only question a
// user cares about: does it hear them better than what it replaced. That
// question needs the real 232 MB model and the real subprocess, so it lives
// here, behind the same gate as the rest of the end-to-end suite:
//
//	LUNITIDE_VOICE_E2E=1 LUNITIDE_VOICE_E2E_ROOT=<dir> go test ./internal/voice/ -run E2ERefine -v -timeout 20m

func TestE2ERefinerIsInstallableFromTheCatalogueAsWritten(t *testing.T) {
	requireE2E(t)
	root := e2eRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	// Installed re-hashes every file, so this passing is the catalogue's
	// digests and the bytes on disk agreeing. It is the check that would
	// have caught a digest transcribed from the wrong revision — the kind of
	// mistake that is invisible until a user's first download fails.
	bundle := mustBundle(t, DefaultRefiner)
	installer := &Installer{Root: root}
	if installer.Installed(bundle) {
		t.Logf("%s already installed and verified", bundle.ID)
		return
	}
	last := -1
	start := time.Now()
	if err := installer.Install(ctx, bundle, func(p Progress) {
		if pct := p.Percent(); pct/10 != last/10 {
			last = pct
			t.Logf("%s %d%% (%s)", p.BundleID, pct, time.Since(start).Round(time.Second))
		}
	}); err != nil {
		t.Fatalf("install %s: %v", bundle.ID, err)
	}
	if !installer.Installed(bundle) {
		t.Fatal("install reported success but the bundle does not verify")
	}
}

func TestE2ERefinedTextIsWhatTheTurnReturns(t *testing.T) {
	requireE2E(t)
	root := e2eRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	refiner := &Refiner{Root: root, Startup: 5 * time.Minute}
	if err := refiner.Ready(ctx); err != nil {
		t.Skipf("refiner model not installed: %v", err)
	}
	warmStart := time.Now()
	if err := refiner.Warm(ctx); err != nil {
		t.Fatalf("warm refiner: %v", err)
	}
	t.Logf("refiner warm in %s", time.Since(warmStart).Round(time.Millisecond))
	defer refiner.Shutdown()

	backend := &SherpaBackend{Root: root, Startup: 5 * time.Minute, Refiner: refiner}
	if err := backend.Ready(ctx); err != nil {
		t.Skipf("streaming model not installed: %v", err)
	}
	defer backend.Shutdown()

	for _, clip := range []string{"0.wav", "1.wav", "2.wav", "3.wav"} {
		pcm := fetchClip(ctx, t, root, clip)
		seconds := float64(len(pcm)/BytesPerSample) / SampleRate

		// The streamed transcript, for comparison. Taken from the last
		// partial rather than by running the clip twice, so both numbers
		// describe the same pass of audio.
		var streamed string
		session, err := backend.Start(ctx, SessionOptions{
			Language:     "zh-CN",
			OnTranscript: func(tr Transcript) { streamed = tr.Text },
		})
		if err != nil {
			t.Fatalf("start session: %v", err)
		}
		for offset := 0; offset < len(pcm); offset += FrameBytes {
			end := min(offset+FrameBytes, len(pcm))
			if err := session.Append(ctx, pcm[offset:end]); err != nil {
				t.Fatalf("append: %v", err)
			}
			time.Sleep(5 * time.Millisecond)
		}

		finishStart := time.Now()
		final, err := session.Finish(ctx)
		waited := time.Since(finishStart)
		_ = session.Close()
		if err != nil {
			t.Fatalf("finish %s: %v", clip, err)
		}

		t.Logf("%s (%.1fs)\n  streamed: %q\n  refined:  %q\n  finish took %s", clip, seconds, streamed, final, waited.Round(time.Millisecond))

		if strings.TrimSpace(final) == "" {
			t.Errorf("%s: the turn ended with no text at all", clip)
		}
		// The pause between the user going quiet and the reply starting.
		// Measured at 280-480ms on these clips with the two recognizers
		// overlapped; a second would be felt as the app being slow to react,
		// whatever the transcript says.
		if waited > time.Second {
			t.Errorf("%s: refinement added %s to the end of the turn", clip, waited.Round(time.Millisecond))
		}
	}
}
