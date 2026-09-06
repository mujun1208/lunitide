package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/meetings"
	"github.com/oklog/ulid/v2"
)

func TestMeetingSummarySourceMigrationDoesNotInventLegacyInput(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	store, err := OpenTemplated(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	m := meetings.Meeting{MeetingID: ulid.Make().String(), Title: "历史会议", Status: meetings.StatusReady, AudioSource: meetings.AudioMicrophone,
		StartedAt: "2026-09-06T00:00:00Z", CreatedAt: "2026-09-06T00:00:00Z", UpdatedAt: "2026-09-06T00:00:00Z", Transcript: "后来修改过的原稿", Summary: "来源无法证明的旧摘要", Actions: "旧待办"}
	if err = store.InsertMeeting(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	raw := openRaw(t, path)
	for _, column := range []string{"summary_edited", "summary_source_transcript", "summary_source_title", "summary_source_digest", "summary_source_revision", "transcript_revision"} {
		if _, err = raw.Exec("ALTER TABLE meetings DROP COLUMN " + column); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = raw.Exec(`DELETE FROM schema_migrations WHERE version='0141_meeting_summary_source.sql'`); err != nil {
		t.Fatal(err)
	}
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.GetMeeting(ctx, m.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Transcript != m.Transcript || got.Summary != m.Summary || got.Actions != m.Actions || got.TranscriptRevision != 1 || got.SummarySourceRevision != 0 || got.SummarySourceDigest != "" || got.SummarySourceTranscript != "" || got.Status != meetings.StatusNeedsSummary || got.SummaryError == "" {
		t.Fatalf("legacy provenance fabricated or content lost: %#v", got)
	}
}
