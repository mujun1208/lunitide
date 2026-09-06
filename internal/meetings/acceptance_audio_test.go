package meetings_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/lunitide/lunitide/internal/meetings"
	sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
)

// One migration fixture, 600 independent meetings. These are 10 fault families
// crossed with all six arrival orders and ten PCM boundary fixtures, not 600
// repetitions of one happy path. No capture device or provider is involved.
func TestAudioAcceptance600FaultSchedules(t *testing.T) {
	meetings.SilenceLoopbackForTest(t)
	ctx := context.Background()
	root := t.TempDir()
	dbPath, audioRoot := filepath.Join(root, "meetings.db"), filepath.Join(root, "audio")
	store, err := sqlitestore.OpenTemplated(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	orders := [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
	lengths := [][3]int{
		{24576, 24576, 1}, {24576, 16384, 8192}, {24575, 16383, 8193},
		{16000, 16385, 16000}, {19200, 19200, 19200}, {16384, 16384, 16385},
		{1, 24576, 24576}, {24576, 1, 24576}, {24000, 24000, 16}, {16383, 16384, 16385},
	}
	faults := []string{"duplicate_ack", "lost_ack_restart", "abandoned_temp", "rename_failure", "payload_digest", "identity_collision", "sample_offset", "sample_count", "request_canceled", "corrupt_committed_pcm"}
	const seed int64 = 20260906
	rng := rand.New(rand.NewSource(seed))
	groups, commits, gapRejects, faultChecks, replays, databaseReopens, failedDecodes := 0, 0, 0, 0, 0, 0, 0
	for faultIndex, fault := range faults {
		t.Run(fault, func(t *testing.T) {
			for orderIndex, order := range orders {
				for variant, sizes := range lengths {
					label := fmt.Sprintf("seed=%d fault=%s order=%v variant=%d", seed, fault, order, variant)
					newService := func() *meetings.Service {
						svc := meetings.New(store)
						svc.SetAudioRoot(audioRoot)
						return svc
					}
					svc := newService()
					m, startErr := svc.Start(ctx, label, meetings.AudioMicrophone)
					if startErr != nil {
						t.Fatalf("%s start: %v", label, startErr)
					}
					original, appendErr := svc.Append(ctx, m.MeetingID, fmt.Sprintf("现场原稿编号%d，交付时间待确认。", groups), 0)
					if appendErr != nil {
						t.Fatalf("%s caption: %v", label, appendErr)
					}
					originalHash := audioAcceptanceHash(t, []meetings.Segment{original})
					captureID := fmt.Sprintf("capture_acceptance_%04d", groups)
					var pcm [4][]byte
					var identities [4]meetings.AudioBatchIdentity
					var acks [4]meetings.AudioBatchAck
					var expected []byte
					var cursor int64
					for seq, samples := range []int{sizes[0], sizes[1], sizes[2], 1 + 16*variant} {
						pcm[seq] = make([]byte, samples*2)
						if _, err := rng.Read(pcm[seq]); err != nil {
							t.Fatal(err)
						}
						identities[seq] = batchIdentity(pcm[seq], int64(seq), cursor)
						identities[seq].CaptureSessionID = captureID
						cursor += int64(samples)
						acks[seq] = meetings.AudioBatchAck{AudioBatchIdentity: identities[seq], AudioMS: cursor / 16}
						expected = append(expected, pcm[seq]...)
					}
					assertUnchanged := func(revision int64, transcript string, status meetings.Status) {
						t.Helper()
						current, err := svc.Get(ctx, m.MeetingID)
						if err != nil || current.Revision != revision || current.Status != status || current.Transcript != transcript || audioAcceptanceHash(t, current.Segments) != originalHash {
							t.Fatalf("%s source/state changed: revision=%d status=%s transcript=%q err=%v", label, current.Revision, current.Status, current.Transcript, err)
						}
					}
					assertAck := func(seq int) {
						t.Helper()
						got, err := svc.AppendAudioBatch(ctx, m.MeetingID, pcm[seq], identities[seq])
						if err != nil || got != acks[seq] {
							t.Fatalf("%s seq=%d ACK=%#v want=%#v err=%v", label, seq, got, acks[seq], err)
						}
					}
					// Deliver all six possible orders. A gap receives an explicit
					// conflict, then only the missing contiguous prefix is retried.
					next := 0
					for _, seq := range order {
						if seq == next {
							assertAck(seq)
							commits++
							next++
						} else {
							if _, err := svc.AppendAudioBatch(ctx, m.MeetingID, pcm[seq], identities[seq]); !errors.Is(err, meetings.ErrConflict) {
								t.Fatalf("%s gap seq=%d: %v", label, seq, err)
							}
							gapRejects++
							assertUnchanged(m.Revision, "", meetings.StatusRecording)
						}
					}
					for ; next < 3; next++ {
						assertAck(next)
						commits++
					}
					dir := filepath.Join(audioRoot, m.MeetingID)
					batchPath := func(seq int) string {
						return filepath.Join(dir, fmt.Sprintf("batch_%s_%012d.json", captureID, seq))
					}
					switch fault {
					case "duplicate_ack":
						for _, seq := range order {
							assertAck(seq)
							replays++
						}
					case "lost_ack_restart":
						// The first ACK is deliberately discarded. A new service
						// has neither sink metadata nor an in-memory receipt cache.
						assertAck(3)
						commits++
						svc = newService()
						assertAck(3)
						replays++
					case "abandoned_temp":
						if err := os.WriteFile(filepath.Join(dir, ".meeting-interrupted.tmp"), []byte(`{"version":1,"pcm":"unfinished`), 0600); err != nil {
							t.Fatal(err)
						}
						svc = newService()
						assertAck(variant % 3)
						replays++
					case "rename_failure":
						// A real filesystem rename failure occurs after the temp
						// file has been written and synced. No production hook.
						if err := os.Mkdir(batchPath(3), 0700); err != nil {
							t.Fatal(err)
						}
						if _, err := svc.AppendAudioBatch(ctx, m.MeetingID, pcm[3], identities[3]); err == nil {
							t.Fatalf("%s acknowledged failed file commit", label)
						}
						if err := os.Remove(batchPath(3)); err != nil {
							t.Fatal(err)
						}
					case "payload_digest":
						changed := append([]byte(nil), pcm[3]...)
						changed[0] ^= 0xff
						if _, err := svc.AppendAudioBatch(ctx, m.MeetingID, changed, identities[3]); !errors.Is(err, meetings.ErrInvalid) {
							t.Fatalf("%s forged digest: %v", label, err)
						}
					case "identity_collision":
						seq := variant % 3
						changed := append([]byte(nil), pcm[seq]...)
						changed[0] ^= 0xff
						id := batchIdentity(changed, int64(seq), identities[seq].SampleStart)
						id.CaptureSessionID = captureID
						if _, err := svc.AppendAudioBatch(ctx, m.MeetingID, changed, id); !errors.Is(err, meetings.ErrConflict) {
							t.Fatalf("%s identity collision: %v", label, err)
						}
					case "sample_offset":
						id := identities[3]
						id.SampleStart += int64(variant + 1)
						if _, err := svc.AppendAudioBatch(ctx, m.MeetingID, pcm[3], id); !errors.Is(err, meetings.ErrConflict) {
							t.Fatalf("%s sample gap: %v", label, err)
						}
					case "sample_count":
						id := identities[3]
						id.SampleCount++
						if _, err := svc.AppendAudioBatch(ctx, m.MeetingID, pcm[3], id); !errors.Is(err, meetings.ErrInvalid) {
							t.Fatalf("%s sample count mismatch: %v", label, err)
						}
					case "request_canceled":
						canceled, cancel := context.WithCancel(ctx)
						cancel()
						if _, err := svc.AppendAudioBatch(canceled, m.MeetingID, pcm[3], identities[3]); !errors.Is(err, context.Canceled) {
							t.Fatalf("%s canceled append: %v", label, err)
						}
					case "corrupt_committed_pcm":
						seq := variant % 3
						raw, err := os.ReadFile(batchPath(seq))
						if err != nil {
							t.Fatal(err)
						}
						var record map[string]json.RawMessage
						if err := json.Unmarshal(raw, &record); err != nil {
							t.Fatal(err)
						}
						changed := append([]byte(nil), pcm[seq]...)
						changed[0] ^= 0xff
						record["pcm"], err = json.Marshal(changed)
						if err != nil {
							t.Fatal(err)
						}
						corrupt, err := json.Marshal(record)
						if err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(batchPath(seq), corrupt, 0600); err != nil {
							t.Fatal(err)
						}
						svc = newService()
						if _, err := svc.AppendAudioBatch(ctx, m.MeetingID, pcm[seq], identities[seq]); !errors.Is(err, meetings.ErrConflict) {
							t.Fatalf("%s corrupt committed bytes acknowledged: %v", label, err)
						}
						// Restore the retained original test bytes; the product is
						// expected to reject corruption, never silently repair it.
						if err := os.WriteFile(batchPath(seq), raw, 0600); err != nil {
							t.Fatal(err)
						}
						svc = newService()
					}
					faultChecks++
					assertUnchanged(m.Revision, "", meetings.StatusRecording)
					assertAck(3)
					if fault != "lost_ack_restart" {
						commits++
					} else {
						replays++
					}
					for seq := 3; seq >= 0; seq-- {
						assertAck(seq)
						replays++
					}
					// Inspect committed receipts independently of the returned
					// ACK, then verify the real catch-up reader's entire PCM hash.
					var offset int64
					for seq := 0; seq < 4; seq++ {
						raw, err := os.ReadFile(batchPath(seq))
						if err != nil {
							t.Fatal(err)
						}
						var record struct {
							Version   int    `json:"version"`
							MeetingID string `json:"meetingId"`
							meetings.AudioBatchAck
							GlobalSampleStart int64  `json:"globalSampleStart"`
							PCM               []byte `json:"pcm"`
							StoredDigest      string `json:"storedDigest"`
						}
						if err := json.Unmarshal(raw, &record); err != nil {
							t.Fatal(err)
						}
						if record.Version != 1 || record.MeetingID != m.MeetingID || record.AudioBatchAck != acks[seq] || record.GlobalSampleStart != offset || !bytes.Equal(record.PCM, pcm[seq]) || record.StoredDigest != identities[seq].Digest {
							t.Fatalf("%s durable receipt/PCM mismatch seq=%d", label, seq)
						}
						offset += identities[seq].SampleCount
					}
					stopped, err := svc.Stop(ctx, m.MeetingID, m.Revision)
					if err != nil || stopped.DurationMS < acks[3].AudioMS {
						t.Fatalf("%s stop: %#v %v", label, stopped, err)
					}
					assertUnchanged(m.Revision+1, original.Text, meetings.StatusTranscribed)
					if orderIndex == len(orders)-1 && variant == len(lengths)-1 {
						if err := store.Close(); err != nil {
							t.Fatal(err)
						}
						store, err = sqlitestore.OpenTemplated(ctx, dbPath)
						if err != nil {
							t.Fatal(err)
						}
						databaseReopens++
					}
					svc = newService()
					for _, seq := range order {
						assertAck(seq)
						replays++
					}
					id := identities[3]
					id.ChunkSeq++
					id.SampleStart += id.SampleCount
					if _, err := svc.AppendAudioBatch(ctx, m.MeetingID, pcm[3], id); !errors.Is(err, meetings.ErrNotRecording) {
						t.Fatalf("%s new audio accepted after stop: %v", label, err)
					}
					assertUnchanged(stopped.Revision, original.Text, meetings.StatusTranscribed)
					latest, err := svc.Get(ctx, m.MeetingID)
					if err != nil || latest.UpdatedAt != stopped.UpdatedAt || latest.EndedAt != stopped.EndedAt {
						t.Fatalf("%s late ACK mutated stopped metadata: %#v %v", label, latest, err)
					}
					calls := 0
					failDecode := faultIndex%2 == 0
					transcribe := func(_ context.Context, got []byte) (string, error) {
						calls++
						if len(got) != len(expected) || sha256.Sum256(got) != sha256.Sum256(expected) {
							t.Fatalf("%s catch-up lost/duplicated/reordered audio: got=%d want=%d", label, len(got), len(expected))
						}
						if failDecode && calls == 1 {
							return "", errors.New("deterministic decoder outage")
						}
						return "补转写已核对全部已确认音频。", nil
					}
					svc.SetAudioTranscriber(transcribe)
					current, err := svc.CatchUp(ctx, m.MeetingID, stopped.Revision)
					if err != nil {
						t.Fatalf("%s catch-up: %v", label, err)
					}
					if failDecode {
						failedDecodes++
						assertUnchanged(stopped.Revision+1, original.Text, meetings.StatusNeedsSummary)
						if current.SummaryError == "" || calls != 1 {
							t.Fatalf("%s missing explicit gap/call count: %#v calls=%d", label, current, calls)
						}
						svc = newService()
						svc.SetAudioTranscriber(transcribe)
						current, err = svc.CatchUp(ctx, m.MeetingID, current.Revision)
						if err != nil {
							t.Fatalf("%s retry gap: %v", label, err)
						}
					}
					wantRevision, wantCalls := stopped.Revision+1, 1
					if failDecode {
						wantRevision++
						wantCalls++
					}
					assertUnchanged(wantRevision, "补转写已核对全部已确认音频。", meetings.StatusTranscribed)
					if calls != wantCalls || current.SummaryError != "" {
						t.Fatalf("%s bad completed gap/calls: %#v calls=%d", label, current, calls)
					}
					replayed, err := svc.CatchUp(ctx, m.MeetingID, current.Revision)
					if err != nil || calls != wantCalls || !reflect.DeepEqual(replayed, current) {
						t.Fatalf("%s completed catch-up replay changed receipt: %#v %v calls=%d", label, replayed, err, calls)
					}
					groups++
				}
			}
			t.Logf("fault=%s groups=60 (6 arrival orders x 10 sample fixtures); all durable ACK, PCM hash, revision, source segment hash and terminal checks passed", fault)
		})
	}
	if groups != 600 || faultChecks != 600 || commits != 2400 || databaseReopens != 10 || failedDecodes != 300 {
		t.Fatalf("acceptance matrix incomplete: groups=%d faults=%d commits=%d reopen=%d failedDecodes=%d", groups, faultChecks, commits, databaseReopens, failedDecodes)
	}
	t.Logf("seed=%d independent_test=1 fault_subtests=10 scenario_groups=%d distinct_fault_families=10 arrival_orders=6 sample_fixtures=10 committed_batches=%d out_of_order_rejections=%d receipt_replays=%d database_reopens=%d decoder_outages_and_recoveries=%d", seed, groups, commits, gapRejects, replays, databaseReopens, failedDecodes)
}

func audioAcceptanceHash(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
