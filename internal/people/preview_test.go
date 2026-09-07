package people_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/people"
	"github.com/oklog/ulid/v2"
)

func TestScreenshotSendPreviewReopenAndReceiveConsent(t *testing.T) {
	a, b := newDurableNode(t), newDurableNode(t)
	thread := pairDurable(t, a, b)
	ctx := context.Background()
	pic := image.NewRGBA(image.Rect(0, 0, 320, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 320; x++ {
			pic.SetRGBA(x, y, color.RGBA{uint8(x*17 + y), uint8(x + y*13), uint8(x * y), 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pic); err != nil {
		t.Fatal(err)
	}
	raw := encoded.Bytes()
	var staged people.StageResult
	id := ulid.Make().String()
	for offset, index := 0, 0; offset < len(raw); index++ {
		end := min(offset+32*1024, len(raw))
		var err error
		staged, err = a.service.StageFile(ctx, people.StageInput{UploadID: id, Index: index, Last: end == len(raw), FileName: "截图.png", FileMIME: "image/png", ContentBase64: base64.StdEncoding.EncodeToString(raw[offset:end])})
		if err != nil {
			t.Fatal(err)
		}
		offset = end
	}
	msg, offer, err := a.service.Send(ctx, people.SendInput{ThreadID: thread.ThreadID, Kind: "image", FileName: "截图.png", FileMIME: "image/png", LocalPath: staged.LocalPath, RequestKey: ulid.Make().String()})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(msg.DestPath) != ".png" {
		t.Fatalf("OS cannot associate screenshot: %s", msg.DestPath)
	}
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
	if data, err := a.service.PreviewFile(ctx, offer.OfferID); err != nil || data != want {
		t.Fatalf("outgoing preview: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var received people.FileOffer
	for {
		received, err = b.store.GetOfferByMessage(ctx, msg.MessageID)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := b.service.PreviewFile(ctx, received.OfferID); err == nil {
		t.Fatal("preview bypassed receive consent")
	}
	if _, err := b.service.DecideFile(ctx, received.OfferID, true); err != nil {
		t.Fatal(err)
	}
	if data, err := b.service.PreviewFile(ctx, received.OfferID); err != nil || data != want {
		t.Fatalf("received preview: %v", err)
	}
	a.service.Close()
	a.store.Close()
	a.open(t)
	_, history, err := a.service.OpenThread(ctx, thread.ThreadID)
	if err != nil || len(history) == 0 {
		t.Fatalf("reopen: %v", err)
	}
	if data, err := a.service.PreviewFile(ctx, offer.OfferID); err != nil || data != want {
		t.Fatalf("preview after restart: %v", err)
	}
	for _, kind := range []string{"text", "emoji"} {
		if _, _, err := a.service.Send(ctx, people.SendInput{ThreadID: thread.ThreadID, Kind: kind, Body: "继续🙂", RequestKey: ulid.Make().String()}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLegacyAttachmentOpenKeepsOriginalAndAddsFileAssociation(t *testing.T) {
	n := newDurableNode(t)
	path := filepath.Join(n.stage, ulid.Make().String())
	if err := os.WriteFile(path, []byte("old file"), 0600); err != nil {
		t.Fatal(err)
	}
	var opened string
	restore := people.ReplaceOpenPathForTest(func(p string) error { opened = p; return nil })
	defer restore()
	for _, name := range []string{"合同.pdf", "报表.xlsx", "资料.zip", "视频.mp4", "音频.mp3"} {
		got, err := n.service.OpenFile(path, name)
		if err != nil || got != opened || !strings.HasSuffix(got, name) {
			t.Fatalf("open %s: %s %v", name, got, err)
		}
		data, err := os.ReadFile(got)
		if err != nil || string(data) != "old file" {
			t.Fatalf("copied bytes changed: %v", err)
		}
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "old file" {
		t.Fatal("old history attachment changed")
	}
}
