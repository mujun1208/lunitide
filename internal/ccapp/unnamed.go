package ccapp

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func unnamedUIName(target string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(target)), " (unnamed)")
}

// PreferPasteText reports CJK or long strings that must go through paste
// instead of per-key SendInput (D-O1).
func PreferPasteText(text string) bool {
	if utf8.RuneCountInString(text) > 16 {
		return true
	}
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
