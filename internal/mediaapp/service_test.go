package mediaapp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

func testMediaService(t *testing.T) (*Service, context.Context, string) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "media-svc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.EnableMediaSessionV2ForTest(ctx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "clip.mp3")
	if err := os.WriteFile(path, []byte("abcd"), 0o600); err != nil {
		t.Fatal(err)
	}
	assetID, err := store.InsertMediaAsset(ctx, "local-user", "user", "local-user", "user_selected", path, "audio/mpeg", "audio", "clip", 4)
	if err != nil {
		t.Fatal(err)
	}
	return New(store), ctx, assetID
}

func TestMediaOriginCapabilities(t *testing.T) {
	svc, ctx, assetID := testMediaService(t)
	snap, _, err := svc.CreateSession(ctx, "local-user", "user", "", assetID, ulid.Make().String())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Origin != "owned" {
		t.Fatalf("origin %q", snap.Origin)
	}
	seek, _, err := svc.Command(ctx, "local-user", snap.MediaSessionID, "seek", ulid.Make().String(), snap.Revision, 1200, 0)
	if err != nil {
		t.Fatal(err)
	}
	if seek.PositionMs != 1200 {
		t.Fatalf("owned seek must persist position %d", seek.PositionMs)
	}
}

func TestOwnedMediaLifecycle(t *testing.T) {
	svc, ctx, assetID := testMediaService(t)
	snap, _, err := svc.CreateSession(ctx, "local-user", "user", "", assetID, ulid.Make().String())
	if err != nil {
		t.Fatal(err)
	}
	play, op, err := svc.Command(ctx, "local-user", snap.MediaSessionID, "play", ulid.Make().String(), snap.Revision, 0, 0)
	if err != nil || op.Phase != "dispatching" {
		t.Fatalf("play %+v op=%+v err=%v", play, op, err)
	}
	vol, _, err := svc.Command(ctx, "local-user", play.MediaSessionID, "set_volume", ulid.Make().String(), play.Revision, 0, 35)
	if err != nil || vol.Volume != 35 {
		t.Fatalf("volume %+v err=%v", vol, err)
	}
	pause, _, err := svc.Command(ctx, "local-user", vol.MediaSessionID, "pause", ulid.Make().String(), vol.Revision, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if pause.VerificationStatus != "command_dispatched" {
		t.Fatalf("command is not verified playback %+v", pause)
	}
}
