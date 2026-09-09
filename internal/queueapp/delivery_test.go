package queueapp

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/queueinput"
)

func TestDeliveryPartsPreservesLegacyReceiptKeysAndNewLongInput(t *testing.T) {
	for _, n := range []int{2048, 2049, 8000, 8001, message.MaxRunes} {
		original := strings.Repeat("🙂", n)
		parts := DeliveryParts(Delivery{Items: []queueinput.Message{{ID: "queue-id", Payload: original}}})
		wantParts := 1
		if n <= 8000 {
			wantParts = (n + 2047) / 2048
		}
		if len(parts) != wantParts {
			t.Fatalf("%d: parts=%d want=%d", n, len(parts), wantParts)
		}
		var all strings.Builder
		for i, p := range parts {
			if p.Key != "queue-id:"+string(rune('0'+i)) {
				t.Fatalf("legacy key changed: %s", p.Key)
			}
			if _, err := message.NormalizeText(p.Text); err != nil {
				t.Fatal(err)
			}
			all.WriteString(p.Text)
		}
		if all.String() != original {
			t.Fatal("delivery lost text")
		}
	}
}
