package app

import (
	"bytes"
	"encoding/json"
	"strings"
)

var mcpSearchConcepts = [][]string{
	{"天气", "气温", "weather", "forecast", "temperature"},
	{"火车", "高铁", "车票", "train", "railway"},
	{"机票", "航班", "飞机票", "flight", "airfare"},
	{"股票", "股价", "行情", "stock", "equity", "quote"},
	{"地图", "路线", "导航", "map", "directions", "route"},
}

func mcpSearchScore(query, text string) int {
	query, text = strings.ToLower(strings.TrimSpace(query)), strings.ToLower(text)
	if strings.Contains(text, query) {
		return 100
	}
	score := 0
	for _, concept := range mcpSearchConcepts {
		requested, matches := false, false
		for _, term := range concept {
			requested = requested || strings.Contains(query, term)
			matches = matches || strings.Contains(text, term)
		}
		if requested && matches {
			score += 20
		}
	}
	for _, term := range strings.Fields(query) {
		if len(term) >= 3 && strings.Contains(text, term) {
			score++
		}
	}
	return score
}

func mcpInputSchema(raw json.RawMessage) json.RawMessage {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '{' && json.Valid(raw) {
		return raw
	}
	return json.RawMessage(`{"type":"object","additionalProperties":true}`)
}
