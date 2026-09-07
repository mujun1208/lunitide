package app

import "github.com/lunitide/lunitide/internal/llmadapter"

func weatherToolDefinition() llmadapter.ToolDefinition {
	return llmadapter.ToolDefinition{Name: "weather.get", Description: "Get structured MET Norway weather forecasts using free public JSON, no API key or webpage scraping. Place names resolve offline via GeoNames (country is optional ISO-2; ambiguous names return placeIds). Use place or known coordinates, never invent a location. days=1..7 starting today. Results are forecast samples with source/update/local date, not station observations or full-day extremes. Respect remaining-day coverage; report missing locations/data honestly.", Schema: []byte(`{"type":"object","properties":{"place":{"type":"string","maxLength":200},"country":{"type":"string","maxLength":2},"placeId":{"type":"string","maxLength":12},"latitude":{"type":"number","minimum":-90,"maximum":90},"longitude":{"type":"number","minimum":-180,"maximum":180},"timezone":{"type":"string","maxLength":64,"description":"IANA timezone for coordinates; city names carry their own timezone"},"days":{"type":"integer","minimum":1,"maximum":7}},"additionalProperties":false}`)}
}
