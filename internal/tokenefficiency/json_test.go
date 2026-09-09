package tokenefficiency

import (
	"strings"
	"testing"
)

func TestCompactToolJSONPreservesExactLiterals(t *testing.T) {
	in := " {\n  \"id\": 900719925474099312345,\n \"decimal\": 1.2300e-09,\n \"code\": \"  if (a) {\\n    x();\\n  }\\n\",\n \"quote\": \"不允许修改 原文\\t≤ 5\",\n \"same\": 1, \"same\": 2, \"unknown\": null\n } "
	want := `{"id":900719925474099312345,"decimal":1.2300e-09,"code":"  if (a) {\n    x();\n  }\n","quote":"不允许修改 原文\t≤ 5","same":1,"same":2,"unknown":null}`
	if got := CompactToolJSON(in); got != want {
		t.Fatalf("literals changed: %s", got)
	}
	if got := CompactToolJSON(want); got != want {
		t.Fatal("not idempotent")
	}
}

func TestCompactToolJSONBypassesUnsupportedOrPartialInputs(t *testing.T) {
	for _, in := range []string{"  log\n content  ", "{\"partial\":  ", "[1] [2]", "12345 ", `"  literal  "`, "", "[" + strings.Repeat(" ", 1<<20) + "]"} {
		if got := CompactToolJSON(in); got != in {
			t.Fatal("unsupported input changed")
		}
	}
}
