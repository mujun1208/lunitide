package officetools

import (
	"errors"
	"fmt"
	"testing"
)

func TestPptxRefusesExcessBulletsInsteadOfDroppingTail(t *testing.T) {
	bullets := make([]string, MaxBulletsPerSld+1)
	for i := range bullets {
		bullets[i] = fmt.Sprintf("必需内容 %d", i+1)
	}
	data, err := GenPptx("完整内容", []SlideSpec{{Title: "内容页", Layout: "content", Bullets: bullets}})
	if !errors.Is(err, ErrLimit) || len(data) != 0 {
		t.Fatalf("oversized slide was silently accepted: bytes=%d err=%v", len(data), err)
	}
}
