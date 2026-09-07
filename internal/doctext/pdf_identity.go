package doctext

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"

	"github.com/ledongthuc/pdf"
)

// The current PDF dependency only adds the low byte when decoding a bfrange.
// A full BMP identity map therefore corrupts CJK characters. Limit this
// compatibility path to the explicit canonical identity CMap; all other
// font encodings continue to use the dependency's existing implementation.
func pdfIdentityFont(font *pdf.Font) bool {
	stream := font.V.Key("ToUnicode")
	if stream.Kind() != pdf.Stream {
		return false
	}
	r := stream.Reader()
	defer r.Close()
	raw, err := io.ReadAll(io.LimitReader(r, 64<<10))
	if err != nil || len(raw) >= 64<<10 {
		return false
	}
	text := strings.ToUpper(strings.Join(strings.Fields(string(raw)), " "))
	return strings.Count(text, "BEGINBFRANGE") == 1 && !strings.Contains(text, "BEGINBFCHAR") &&
		strings.Contains(text, "1 BEGINBFRANGE <0000> <FFFF> <0000> ENDBFRANGE") &&
		strings.Count(text, "BEGINCODESPACERANGE") == 1 &&
		strings.Contains(text, "1 BEGINCODESPACERANGE <0000> <FFFF> ENDCODESPACERANGE")
}

func pdfPageText(page pdf.Page, fonts map[string]*pdf.Font) (string, error) {
	identity := make(map[string]bool)
	for name, font := range fonts {
		if pdfIdentityFont(font) {
			identity[name] = true
		}
	}
	if len(identity) == 0 {
		return page.GetPlainText(fonts)
	}
	var b strings.Builder
	var parseErr error
	fontName := ""
	write := func(raw string) {
		if parseErr != nil {
			return
		}
		text := raw
		if identity[fontName] {
			if len(raw)%2 != 0 {
				parseErr = fmt.Errorf("%w: malformed UTF-16 PDF text", ErrNoTextLayer)
				return
			}
			chars := make([]uint16, len(raw)/2)
			for i := range chars {
				chars[i] = binary.BigEndian.Uint16([]byte(raw[i*2 : i*2+2]))
			}
			text = string(utf16.Decode(chars))
		} else if font := fonts[fontName]; font != nil {
			text = font.Encoder().Decode(raw)
		}
		if b.Len()+len(text) > maxBuildBytes {
			parseErr = ErrBudgetExceeded
			return
		}
		b.WriteString(text)
	}
	pdf.Interpret(page.V.Key("Contents"), func(stack *pdf.Stack, op string) {
		args := make([]pdf.Value, stack.Len())
		for i := len(args) - 1; i >= 0; i-- {
			args[i] = stack.Pop()
		}
		if parseErr != nil {
			return
		}
		switch op {
		case "BT", "T*":
			b.WriteByte('\n')
		case "Tf":
			if len(args) != 2 {
				parseErr = ErrNoTextLayer
				return
			}
			fontName = args[0].Name()
		case "Tj", "'", "\"":
			expected := 1
			if op == "\"" {
				expected = 3
			}
			if len(args) != expected {
				parseErr = ErrNoTextLayer
				return
			}
			if op != "Tj" {
				b.WriteByte('\n')
			}
			write(args[len(args)-1].RawString())
		case "TJ":
			if len(args) != 1 || args[0].Kind() != pdf.Array {
				parseErr = ErrNoTextLayer
				return
			}
			for i := 0; i < args[0].Len(); i++ {
				v := args[0].Index(i)
				if v.Kind() == pdf.String {
					write(v.RawString())
				}
			}
		}
		if b.Len() > maxBuildBytes {
			parseErr = ErrBudgetExceeded
		}
	})
	if parseErr != nil {
		return "", parseErr
	}
	return b.String(), nil
}
