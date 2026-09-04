package toolruntime

import "encoding/json"

func AppendL0JSON(output, kind string, passed, uncertain bool, detail string) string {
	return appendL0JSON(output, kind, passed, uncertain, detail)
}

func appendL0JSON(output, kind string, passed, uncertain bool, detail string) string {
	raw, err := json.Marshal(map[string]any{
		"l0": map[string]any{
			"kind":      kind,
			"passed":    passed,
			"uncertain": uncertain,
			"detail":    detail,
		},
	})
	if err != nil {
		return output
	}
	if output == "" {
		return string(raw)
	}
	return output + "\n" + string(raw)
}
