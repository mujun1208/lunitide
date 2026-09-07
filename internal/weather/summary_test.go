package weather

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestWeatherSummaryPreservesSevenDaysAndValidJSON(t *testing.T) {
	r := Result{Kind: "weather_forecast", Source: Endpoint, Attribution: "MET Norway; GeoNames", License: "CC BY 4.0", Location: Location{Name: "合肥", Timezone: "Asia/Shanghai"}, RetrievedAt: time.Now(), UpdatedAt: time.Now(), Notice: "预报而非实测"}
	for i := range 7 {
		day := Day{Date: fmt.Sprintf("2026-09-%02d", 7+i), From: "2026-09-07T00:00:00+08:00", Through: "2026-09-07T23:00:00+08:00", Minimum: 18, Maximum: 26, Samples: 24}
		for j := range 8 {
			day.Conditions = append(day.Conditions, fmt.Sprintf("%d_%s", j, strings.Repeat("a", 60)))
		}
		r.Days = append(r.Days, day)
	}
	b, err := r.JSONSummary()
	if err != nil || len(b) > 3840 || !json.Valid(b) {
		t.Fatalf("invalid summary (%d bytes): %v", len(b), err)
	}
	var got Result
	if err := json.Unmarshal(b, &got); err != nil || len(got.Days) != 7 || !got.DetailsTruncated || got.Days[6].Date != r.Days[6].Date || got.Days[6].Maximum != 26 || got.Source != Endpoint {
		t.Fatalf("lost required forecast: %+v %v", got, err)
	}
	if len(r.Days[0].Conditions) != 8 {
		t.Fatal("formatting mutated the original forecast")
	}
	r.Notice = strings.Repeat("x", 5000)
	if _, err := r.JSONSummary(); err == nil {
		t.Fatal("unbounded summary accepted")
	}
}

func TestWeatherSummaryKeepsNormalConditions(t *testing.T) {
	r := Result{Days: []Day{{Conditions: []string{"rain"}}}}
	b, err := r.JSONSummary()
	if err != nil || !strings.Contains(string(b), "rain") || strings.Contains(string(b), "detailsTruncated") {
		t.Fatalf("unnecessary truncation: %s %v", b, err)
	}
}
