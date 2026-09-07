package mcp6

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/mcp"
)

func TestTusharePresetUsesOfficialLeasedCredentialTemplate(t *testing.T) {
	p, ok := PresetByID("tushare")
	if !ok || p.URL != "https://api.tushare.pro/mcp/token={{credential}}" || !p.NeedsCredential || p.Transport != "https" || len(p.Args) != 0 || !strings.Contains(p.Description, "非复权日线") {
		t.Fatalf("incomplete financial preset: %+v", p)
	}
	if err := mcp.ValidateBaseURL(p.URL); err != nil {
		t.Fatal(err)
	}
}
