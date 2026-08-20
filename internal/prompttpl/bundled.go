package prompttpl

import _ "embed"

//go:embed bundled/lunitide-prompt.tpl
var bundledLunitidePrompt string

// DefaultTemplate returns the product-shipped lunitide-prompt.tpl body.
func DefaultTemplate() string {
	return bundledLunitidePrompt
}
