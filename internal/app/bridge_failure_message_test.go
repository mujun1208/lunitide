package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
)

func TestDeliverableFailureEnglishFallbackIsChinese(t *testing.T) {
	r := bridge.Request{ID: "del-zh", Method: "deliverable.get"}
	got := deliverableFailure(r, errors.New("sql: database is closed"))
	if got.OK || got.Error == nil || got.Error.Code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("unexpected: %+v", got)
	}
	if strings.Contains(got.Error.Message, "sql:") || !officeUserMessageHasHan(got.Error.Message) {
		t.Fatalf("deliverable leaked English: %q", got.Error.Message)
	}
}

func TestProjectAttachmentFailureEnglishFallbackIsChinese(t *testing.T) {
	r := bridge.Request{ID: "att-zh", Method: "projectAttachment.get"}
	got := projectAttachmentFailure(r, errors.New("disk full"))
	if got.OK || got.Error == nil || got.Error.Code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("unexpected: %+v", got)
	}
	if strings.Contains(got.Error.Message, "disk full") || !officeUserMessageHasHan(got.Error.Message) {
		t.Fatalf("project attachment leaked English: %q", got.Error.Message)
	}
}
