package officeapp

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/doctext"
)

func TestArtifactSnapshotRawTail(t *testing.T) {
	ctx := context.Background()
	svc, _, task := studioServiceFixture(t)
	v := generatedWord(t, svc, task, "artifact-snapshot-raw-tail")
	got, raw, err := svc.ReadVersion(ctx, task.ID, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	extracted, err := doctext.Extract(got.Name, raw, got.MediaType)
	if err != nil {
		t.Fatal(err)
	}
	extractSHA := digest([]byte(extracted.Text))
	rawSHA := digest(raw)
	if rawSHA != got.SHA256 {
		t.Fatalf("fixture blob SHA %s != version SHA %s", rawSHA, got.SHA256)
	}
	if extractSHA == rawSHA {
		t.Fatal("fixture extract SHA collided with raw blob SHA")
	}

	snap, err := svc.SnapshotOfficeArtifact(ctx, task.ID, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.SHA256 != rawSHA {
		t.Fatalf("office snapshot SHA %s want raw blob %s", snap.SHA256, rawSHA)
	}
	if snap.SHA256 == extractSHA {
		t.Fatal("office blob SHA reused extract/paginated text SHA as the file digest")
	}
	if snap.Bytes != int64(len(raw)) {
		t.Fatalf("office snapshot bytes %d want %d", snap.Bytes, len(raw))
	}
	if snap.ContentRef != "office-version:"+got.ID {
		t.Fatalf("office snapshot contentRef %q want office-version:%s", snap.ContentRef, got.ID)
	}
}
