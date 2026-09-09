package app

import (
	"github.com/lunitide/lunitide/internal/config"
)

// Config is captured at composition time, not inferred anew from each turn.
func (e *Engine) SetOfficeFlags(f config.OfficeFlags) { e.officeFlags = &f }
func (e *Engine) officeCapabilities() config.OfficeFlags {
	if e.officeFlags == nil {
		return config.DefaultOfficeFlags()
	}
	return *e.officeFlags
}
func (e *Engine) officeWritesEnabled(method, kind string) bool {
	f := e.officeCapabilities()
	if !f.Studio {
		return false
	}
	switch method {
	case "office.generate":
		return f.Spec
	case "office.artifact.validate":
		return f.Render
	case "office.patch", "office.artifact.patch":
		switch kind {
		case "pptx":
			return f.PPTXPatch
		case "docx":
			return f.DOCXPatch
		case "xlsx":
			return f.XLSXPatch
		default:
			return false
		}
	case "office.bundle.create":
		return f.Bundle
	}
	return true
}
