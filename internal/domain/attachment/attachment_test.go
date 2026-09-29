package attachment

import (
	"strings"
	"testing"
	"time"
)

func storedAttachment(size int64) Attachment {
	return Attachment{
		ID:           "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ProjectID:    "01ARZ3NDEKTSV4RRFFQ69G5FAW",
		FileRef:      "files/a.docx",
		OriginalName: "需求.docx",
		MIME:         "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		Size:         size,
		SHA256:       strings.Repeat("ab", 32),
		ParseStatus:  StatusPending,
		CreatedAt:    time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC),
	}
}

func TestAttachmentStoresFilesAboveTheOldTenMegabyteCap(t *testing.T) {
	if err := storedAttachment(11 << 20).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := storedAttachment(MaxStoredBytes).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := storedAttachment(MaxStoredBytes + 1).Validate(); err == nil {
		t.Fatal("file above the upload cap was accepted")
	}
}
