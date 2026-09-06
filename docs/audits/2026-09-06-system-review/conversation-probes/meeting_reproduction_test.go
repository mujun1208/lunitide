//go:build auditprobe

// Audit reproductions: these tests PASS when the current defect is reproduced.
// They use temporary SQLite files, synthetic silence, and fake completers only.
package conversationprobes

import (
 "context"
 "errors"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/lunitide/lunitide/internal/meetings"
 sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func auditStore(t *testing.T) *sqlitestore.Store {
 t.Helper()
 meetings.SilenceLoopbackForTest(t)
 s, err := sqlitestore.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "audit.db"))
 if err != nil { t.Fatal(err) }
 t.Cleanup(func(){ _ = s.Close() })
 return s
}

type heartbeatKey struct{}
type gatedStore struct {
 *sqlitestore.Store
 entered chan struct{}
 release chan struct{}
}
func(s *gatedStore) UpdateMeeting(ctx context.Context, m meetings.Meeting) error {
 if ctx.Value(heartbeatKey{}) == true { close(s.entered); <-s.release }
 return s.Store.UpdateMeeting(ctx, m)
}

func TestReproduceHeartbeatResurrectsStoppedMeeting(t *testing.T) {
 store := &gatedStore{auditStore(t), make(chan struct{}), make(chan struct{})}
 svc := meetings.New(store)
 ctx := context.Background()
 m, err := svc.Start(ctx, "audit", "microphone")
 if err != nil { t.Fatal(err) }
 done := make(chan error, 1)
 go func(){ _, err := svc.Heartbeat(context.WithValue(ctx, heartbeatKey{}, true), m.MeetingID); done <- err }()
 select { case <-store.entered: case <-time.After(3*time.Second): t.Fatal("heartbeat did not reach write") }
 stopped, err := svc.Stop(ctx, m.MeetingID)
 if err != nil || stopped.Status != meetings.StatusTranscribed { t.Fatalf("stop: %#v %v", stopped, err) }
 close(store.release)
 if err := <-done; err != nil { t.Fatal(err) }
 got, err := svc.Get(ctx, m.MeetingID)
 if err != nil { t.Fatal(err) }
 if got.Status != meetings.StatusRecording || got.EndedAt != "" { t.Fatalf("defect no longer reproduces: %#v", got) }
 t.Log("REPRODUCED: stale heartbeat changed transcribed back to recording and cleared endedAt")
}

func TestReproduceCatchupFailureDeletesOriginalSegments(t *testing.T) {
 store := auditStore(t)
 svc := meetings.New(store)
 svc.SetAudioRoot(t.TempDir())
 ctx := context.Background()
 m, err := svc.Start(ctx, "audit", "microphone")
 if err != nil { t.Fatal(err) }
 if _, err := svc.Append(ctx, m.MeetingID, "客户确认周五交付", 0); err != nil { t.Fatal(err) }
 if _, err := svc.AppendAudio(ctx, m.MeetingID, make([]byte, 16000*2*5)); err != nil { t.Fatal(err) }
 if _, err := svc.Stop(ctx, m.MeetingID); err != nil { t.Fatal(err) }
 svc.SetAudioTranscriber(func(context.Context, []byte)(string,error){ return "", errors.New("offline decoder unavailable") })
 if _, err := svc.CatchUp(ctx, m.MeetingID); err != nil { t.Fatal(err) }
 got, err := svc.Get(ctx, m.MeetingID)
 if err != nil { t.Fatal(err) }
 if len(got.Segments) != 0 || got.Transcript != "客户确认周五交付" { t.Fatalf("defect no longer reproduces: %#v", got) }
 t.Log("REPRODUCED: failed catchup removed original timed segments; flattened transcript survives")
}

func TestReproduceSummaryOverwritesConcurrentUserEdit(t *testing.T) {
 svc := meetings.New(auditStore(t))
 ctx := context.Background()
 m, err := svc.Start(ctx, "original title", "microphone")
 if err != nil { t.Fatal(err) }
 if _, err := svc.Append(ctx, m.MeetingID, "原始逐字稿", 0); err != nil { t.Fatal(err) }
 if _, err := svc.Stop(ctx, m.MeetingID); err != nil { t.Fatal(err) }
 entered, release := make(chan struct{}), make(chan struct{})
 svc.SetCompleter(func(context.Context,string,string)(meetings.Notes,error){ close(entered); <-release; return meetings.Notes{Summary:"模型摘要"},nil })
 done := make(chan error,1)
 go func(){ _, err := svc.Summarize(ctx,m.MeetingID); done <- err }()
 select { case <-entered: case <-time.After(3*time.Second): t.Fatal("completer not called") }
 edited := "人工校正后的逐字稿"
 if _, err := svc.Update(ctx,m.MeetingID,meetings.MeetingPatch{Transcript:&edited}); err != nil { t.Fatal(err) }
 close(release)
 if err := <-done; err != nil { t.Fatal(err) }
 got, err := svc.Get(ctx,m.MeetingID)
 if err != nil { t.Fatal(err) }
 if got.Transcript != "原始逐字稿" { t.Fatalf("defect no longer reproduces: %#v",got) }
 t.Log("REPRODUCED: completed summary overwrote a successful concurrent user transcript correction")
}

func TestReproduceAudioReplayDoublesDuration(t *testing.T) {
 svc := meetings.New(auditStore(t))
 svc.SetAudioRoot(t.TempDir())
 ctx := context.Background()
 m, err := svc.Start(ctx,"audit","microphone")
 if err != nil { t.Fatal(err) }
 pcm := make([]byte,16000*2)
 first, err := svc.AppendAudio(ctx,m.MeetingID,pcm)
 if err != nil { t.Fatal(err) }
 second, err := svc.AppendAudio(ctx,m.MeetingID,pcm)
 if err != nil { t.Fatal(err) }
 if first != 1000 || second != 2000 { t.Fatalf("unexpected duration: %d -> %d",first,second) }
 if _,err := svc.Stop(ctx,m.MeetingID); err != nil { t.Fatal(err) }
 t.Log("REPRODUCED: replaying one logical audio batch appends the same second twice")
}

func TestReproduceCatchupSkipsFailedMiddleSpanOnRetry(t *testing.T) {
 svc := meetings.New(auditStore(t))
 svc.SetAudioRoot(t.TempDir())
 ctx := context.Background()
 m, err := svc.Start(ctx,"audit","microphone")
 if err != nil { t.Fatal(err) }
 if _, err := svc.AppendAudio(ctx,m.MeetingID,make([]byte,16000*2*45)); err != nil { t.Fatal(err) }
 if _, err := svc.Stop(ctx,m.MeetingID); err != nil { t.Fatal(err) }
 calls:=0
 svc.SetAudioTranscriber(func(context.Context,[]byte)(string,error){
  calls++
  if calls==1 {return "",errors.New("first twenty seconds fail")}
  if calls==2 {return strings.Repeat("这是中间成功转写的内容",10),nil}
  return strings.Repeat("这是结尾成功转写的内容",10),nil
 })
 first,err:=svc.CatchUp(ctx,m.MeetingID)
 if err!=nil {t.Fatal(err)}
 if calls!=3 || first.SummaryError!="" || len(first.Segments)!=2 {t.Fatalf("unexpected catchup: calls=%d meeting=%#v",calls,first)}
 if _,err:=svc.CatchUp(ctx,m.MeetingID);err!=nil {t.Fatal(err)}
 if calls!=3 {t.Fatalf("defect no longer reproduces: failed span was retried; calls=%d",calls)}
 t.Log("REPRODUCED: failed first 20s span is omitted silently; later success advances watermark and retry never revisits gap")
}
