// Package prompttpl compiles lunitide-prompt.tpl placeholders into prompt text.
package prompttpl

import (
	"fmt"
	"strings"
)

const maxTemplateBytes = 256 << 10

// Compile replaces {{key}} placeholders in tpl with vars values.
// Unknown placeholders are left unchanged; empty vars values render as empty strings.
func Compile(tpl string, vars map[string]string) (string, error) {
	if len(tpl) == 0 {
		return "", fmt.Errorf("prompttpl: template is empty")
	}
	if len(tpl) > maxTemplateBytes {
		return "", fmt.Errorf("prompttpl: template exceeds %d bytes", maxTemplateBytes)
	}
	if vars == nil {
		vars = map[string]string{}
	}
	out := tpl
	for key, value := range vars {
		if key == "" {
			continue
		}
		out = strings.ReplaceAll(out, "{{"+key+"}}", value)
	}
	return out, nil
}

// ResolveTemplate returns inline template text or a bundled template by ref name.
func ResolveTemplate(inline, ref string) (string, error) {
	inline = strings.TrimSpace(inline)
	ref = strings.TrimSpace(ref)
	switch {
	case inline != "":
		return inline, nil
	case ref == "" || ref == "lunitide-prompt.tpl":
		return DefaultTemplate(), nil
	default:
		return "", fmt.Errorf("prompttpl: unknown template ref %q", ref)
	}
}
