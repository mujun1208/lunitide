package config

import (
	"os"
	"strings"
)

// OfficeFlags uses the existing process-level config mechanism. Closing a
// capability forbids new writes; stored versions remain readable/exportable.
type OfficeFlags struct{ Studio, Spec, Render, PPTXPatch, DOCXPatch, XLSXPatch, Bundle bool }

func DefaultOfficeFlags() OfficeFlags { return OfficeFlags{true, true, true, true, true, true, true} }
func LoadOfficeFlagsFromEnv() OfficeFlags {
	f := DefaultOfficeFlags()
	for key, p := range map[string]*bool{
		"LUNITIDE_OFFICE_STUDIO": &f.Studio, "LUNITIDE_OFFICE_SPEC": &f.Spec, "LUNITIDE_OFFICE_RENDER": &f.Render,
		"LUNITIDE_OFFICE_PATCH_PPTX": &f.PPTXPatch, "LUNITIDE_OFFICE_PATCH_DOCX": &f.DOCXPatch, "LUNITIDE_OFFICE_PATCH_XLSX": &f.XLSXPatch, "LUNITIDE_OFFICE_BUNDLE": &f.Bundle,
	} {
		if v, ok := os.LookupEnv(key); ok {
			*p = envTruthy(strings.TrimSpace(v))
		}
	}
	return f
}
