package doctext_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/doctext"
)

func mappedPDFFixture(cmap string) []byte {
	content := "BT /F1 12 Tf <4E2D6587> Tj /F2 12 Tf ( English 123) Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 4 0 R /F2 7 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /Fixture /Encoding /Identity-H /ToUnicode 6 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmap), cmap),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

func TestPDFUnicodeIdentityCompatibilityKeepsOtherFontMappings(t *testing.T) {
	space := "begincmap\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"
	for _, tc := range []struct{ name, mapping, want string }{
		{"identity", "1 beginbfrange\n<0000> <FFFF> <0000>\nendbfrange", "中文 English 123"},
		{"explicit-mapping", "2 beginbfchar\n<4E2D> <597D>\n<6587> <7684>\nendbfchar", "好的 English 123"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := doctext.Extract("mapped.pdf", mappedPDFFixture(space+tc.mapping+"\nendcmap"), "")
			if err != nil || strings.TrimSpace(result.Text) != tc.want {
				t.Fatalf("font mapping changed: %q err=%v", result.Text, err)
			}
		})
	}
}
