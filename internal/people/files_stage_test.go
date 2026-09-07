package people_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"testing"

	"github.com/lunitide/lunitide/internal/people"
	"github.com/oklog/ulid/v2"
)

func TestStageScreenshotLostReceiptsDoNotCorruptFile(t *testing.T) {
	n := newDurableNode(t)
	ctx := context.Background()
	id := ulid.Make().String()
	want := bytes.Repeat([]byte{137, 80, 78, 71, 0, 255, 128}, 19000)
	var ready people.StageResult
	for offset, index := 0, 0; offset < len(want); index++ {
		end := min(offset+32*1024, len(want))
		in := people.StageInput{UploadID: id, FileName: "截图.png", FileMIME: "image/png", Index: index, Last: end == len(want), ContentBase64: base64.StdEncoding.EncodeToString(want[offset:end])}
		got, err := n.service.StageFile(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		// The host may commit a chunk and lose its reply, including the last chunk.
		replay, err := n.service.StageFile(ctx, in)
		if err != nil || got != replay {
			t.Fatalf("chunk %d replay: %+v != %+v, %v", index, got, replay, err)
		}
		ready = got
		offset = end
	}
	got, err := os.ReadFile(ready.LocalPath)
	if err != nil || !ready.Ready || ready.Bytes != int64(len(want)) || !bytes.Equal(got, want) {
		t.Fatalf("screenshot changed: %d / %d bytes, %v", len(got), len(want), err)
	}
}

func TestStageRejectsOutOfOrderConflictsAndRestartOverwrite(t *testing.T) {
	n := newDurableNode(t)
	ctx := context.Background()
	in := people.StageInput{UploadID: ulid.Make().String(), FileName: "截图.png", FileMIME: "image/png", ContentBase64: "YWJj"}
	gap := in
	gap.Index = 1
	if _, err := n.service.StageFile(ctx, gap); err == nil {
		t.Fatal("accepted missing first chunk")
	}
	if _, err := n.service.StageFile(ctx, in); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*people.StageInput){
		func(p *people.StageInput) { p.ContentBase64 = "ZGVm" },
		func(p *people.StageInput) { p.Last = true },
		func(p *people.StageInput) { p.FileName = "other.png" },
		func(p *people.StageInput) { p.Index = 2 },
	} {
		bad := in
		mutate(&bad)
		if _, err := n.service.StageFile(ctx, bad); err == nil {
			t.Fatal("accepted conflicting chunk")
		}
	}
	last := in
	last.Index = 1
	last.Last = true
	ready, err := n.service.StageFile(ctx, last)
	if err != nil {
		t.Fatal(err)
	}
	n.service.Close()
	n.store.Close()
	n.open(t)
	if _, err := n.service.StageFile(ctx, in); err == nil {
		t.Fatal("restarted service overwrote existing upload")
	}
	got, err := os.ReadFile(ready.LocalPath)
	if err != nil || string(got) != "abcabc" {
		t.Fatalf("restart changed bytes: %q, %v", got, err)
	}
}
