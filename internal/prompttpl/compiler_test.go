package prompttpl

import (
	"strings"
	"testing"
)

func TestCompileReplacesPlaceholders(t *testing.T) {
	tpl := "# {{role}}\n\nProject: {{projectName}}\n"
	got, err := Compile(tpl, map[string]string{
		"role":        "数据库专家",
		"projectName": "月汐 ERP",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "# 数据库专家\n\nProject: 月汐 ERP\n"
	if got != want {
		t.Fatalf("compiled: %q want %q", got, want)
	}
}

func TestResolveTemplateBundledDefault(t *testing.T) {
	tpl, err := ResolveTemplate("", "lunitide-prompt.tpl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tpl, "{{role}}") {
		t.Fatalf("bundled template missing role placeholder: %q", tpl)
	}
}

func TestResolveTemplateInline(t *testing.T) {
	tpl, err := ResolveTemplate("hello {{name}}", "")
	if err != nil || tpl != "hello {{name}}" {
		t.Fatalf("inline template: %q err=%v", tpl, err)
	}
}
