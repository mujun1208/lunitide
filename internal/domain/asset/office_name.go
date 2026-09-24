package asset

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const OfficeTemplateFallbackDescription = "办公模版"

func OfficeTemplateLabel(fileName, modelAnswer string) (string, string) {
	name, desc := parseOfficeModelAnswer(modelAnswer)
	if _, err := NormalizeName(name); err != nil {
		name = officeFileStem(fileName)
	}
	name, _ = NormalizeName(name)
	if name == "" {
		name = "办公模版"
	}
	desc = strings.Join(strings.Fields(desc), " ")
	if desc == "" || utf8.RuneCountInString(desc) > 2000 {
		desc = OfficeTemplateFallbackDescription
	}
	return name, desc
}

func officeFileStem(fileName string) string {
	base := filepath.Base(strings.ReplaceAll(strings.TrimSpace(fileName), `\`, "/"))
	ext := filepath.Ext(base)
	switch strings.ToLower(ext) {
	case ".pptx", ".docx", ".xlsx":
		base = strings.TrimSpace(base[:len(base)-len(ext)])
	}
	base = strings.Join(strings.Fields(base), " ")
	if base == "" {
		return "办公模版"
	}
	if utf8.RuneCountInString(base) > 200 {
		base = string([]rune(base)[:200])
	}
	return base
}

func parseOfficeModelAnswer(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if strings.HasPrefix(raw, "{") {
		var parsed struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if json.Unmarshal([]byte(raw), &parsed) == nil {
			return clipOfficeLabel(parsed.Name, 40), clipOfficeLabel(parsed.Description, 200)
		}
	}
	var name, desc string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "名称："):
			name = strings.TrimPrefix(line, "名称：")
		case strings.HasPrefix(line, "名称:"):
			name = strings.TrimPrefix(line, "名称:")
		case strings.HasPrefix(line, "描述："):
			desc = strings.TrimPrefix(line, "描述：")
		case strings.HasPrefix(line, "描述:"):
			desc = strings.TrimPrefix(line, "描述:")
		}
	}
	return clipOfficeLabel(name, 40), clipOfficeLabel(desc, 200)
}

func clipOfficeLabel(value string, limit int) string {
	value = strings.Trim(strings.Join(strings.Fields(value), " "), `"'“”`)
	if value == "" {
		return ""
	}
	if utf8.RuneCountInString(value) > limit {
		value = string([]rune(value)[:limit])
	}
	return value
}
