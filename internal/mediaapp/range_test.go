package mediaapp

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMediaPlaybackURLOrigin(t *testing.T) {
	if !strings.HasPrefix(PlaybackOrigin, "https://media.lunitide.local/v1/assets/") {
		t.Fatalf("origin %q", PlaybackOrigin)
	}
}

func TestRangeRejectsWithoutTicket(t *testing.T) {
	svc := New(nil)
	if _, _, err := svc.ResolveTicket("missing", "owner"); err == nil {
		t.Fatal("missing ticket must fail closed")
	}
}

func TestServeFileRangeStatusAndCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip.bin")
	payload := bytesOf(4000)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	full, err := ServeFileRange(path, "", "audio/mpeg")
	if err != nil || full.Status != http.StatusOK || full.AcceptRanges != "bytes" || full.ContentType != "audio/mpeg" {
		t.Fatalf("200 %+v err=%v", full, err)
	}
	if int64(len(full.Body)) != 4000 {
		t.Fatalf("uncapped small body %d", len(full.Body))
	}

	partial, err := ServeFileRange(path, "bytes=0-99", "audio/mpeg")
	if err != nil || partial.Status != http.StatusPartialContent || partial.ContentRange != "bytes 0-99/4000" || len(partial.Body) != 100 {
		t.Fatalf("206 %+v err=%v", partial, err)
	}

	if _, err := ServeFileRange(path, "bytes=0-1,2-3", "audio/mpeg"); err != ErrRangeMulti {
		t.Fatalf("multi-range %v", err)
	}
	if _, err := ServeFileRange(path, "bytes=9000-9001", "audio/mpeg"); err != ErrRangeUnsatisfiable {
		t.Fatalf("416 %v", err)
	}

	large := filepath.Join(t.TempDir(), "large.bin")
	if err := os.WriteFile(large, bytesOf(MaxRangeBytes+2048), 0o600); err != nil {
		t.Fatal(err)
	}
	capped, err := ServeFileRange(large, "bytes=0-999999", "video/mp4")
	if err != nil || capped.Status != http.StatusPartialContent || int64(len(capped.Body)) != MaxRangeBytes {
		t.Fatalf("cap %+v err=%v", capped, err)
	}

	first, err := ServeFileRange(large, "", "video/mp4")
	if err != nil || first.Status != http.StatusPartialContent || int64(len(first.Body)) != MaxRangeBytes || !strings.HasPrefix(first.ContentRange, "bytes 0-") {
		t.Fatalf("large GET must advertise total size via 206 %+v err=%v", first, err)
	}

	gone, err := ServeMediaHTTP(large, "HEAD", "bytes=0-9", "video/mp4")
	if err != nil || gone.Status != http.StatusPartialContent || gone.Body != nil || gone.ContentLength != 10 {
		t.Fatalf("HEAD %+v err=%v", gone, err)
	}
	unsat, err := ServeMediaHTTP(path, "GET", "bytes=9000-9001", "audio/mpeg")
	if err != nil || unsat.Status != http.StatusRequestedRangeNotSatisfiable || unsat.ContentRange != "bytes */4000" {
		t.Fatalf("416 %+v err=%v", unsat, err)
	}
}

func TestServeTicketRangeUsesResolvedPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owned.bin")
	if err := os.WriteFile(path, []byte("abcd"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := New(nil)
	svc.mu.Lock()
	svc.tickets["tok"] = ticket{Token: "tok", Path: path, MIME: "audio/wav", Owner: "local-user", Created: time.Now().UTC(), Expires: time.Now().UTC().Add(time.Minute)}
	svc.mu.Unlock()
	got, err := svc.ServeTicketRange("tok", "local-user", "bytes=1-2", "audio/wav")
	if err != nil || string(got.Body) != "bc" || got.Status != http.StatusPartialContent {
		t.Fatalf("%+v err=%v", got, err)
	}
	if _, err := svc.ServeTicketRange("tok", "other", "", "audio/wav"); err == nil {
		t.Fatal("owner mismatch must fail closed")
	}
}

func TestTicketIdleRefreshAndHardCap(t *testing.T) {
	svc := New(nil)
	now := time.Now().UTC()
	svc.mu.Lock()
	svc.tickets["fresh"] = ticket{Token: "fresh", Path: "p", MIME: "audio/mpeg", Owner: "o", Created: now, Expires: now.Add(ticketInitialTTL)}
	svc.tickets["old"] = ticket{Token: "old", Path: "p", MIME: "audio/mpeg", Owner: "o", Created: now.Add(-ticketHardCap - time.Second), Expires: now.Add(time.Hour)}
	svc.mu.Unlock()
	if _, _, err := svc.ResolveTicket("old", "o"); err == nil {
		t.Fatal("hard cap must expire tickets")
	}
	if _, mime, err := svc.ResolveTicket("fresh", "o"); err != nil || mime != "audio/mpeg" {
		t.Fatalf("fresh err=%v mime=%s", err, mime)
	}
	svc.mu.Lock()
	row := svc.tickets["fresh"]
	svc.mu.Unlock()
	if !row.Expires.After(now.Add(time.Minute)) {
		t.Fatalf("successful resolve must extend idle TTL: %s", row.Expires)
	}
}

func bytesOf(n int) []byte {
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = byte(i)
	}
	return buf
}
