package weather

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

//go:embed cities.json.gz
var cityData []byte

type Location struct {
	ID        string   `json:"id,omitempty"`
	Name      string   `json:"name"`
	Names     []string `json:"names,omitempty"`
	Latitude  float64  `json:"lat"`
	Longitude float64  `json:"lon"`
	Country   string   `json:"country,omitempty"`
	Admin     string   `json:"admin,omitempty"`
	Timezone  string   `json:"timezone"`
}

var locations = sync.OnceValues(func() ([]Location, error) {
	r, err := gzip.NewReader(bytes.NewReader(cityData))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var cities []Location
	err = json.NewDecoder(io.LimitReader(r, 24<<20)).Decode(&cities)
	return cities, err
})

func normalizeName(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Exact names only: a missing/ambiguous name must never silently resolve to a
// different city. The compact catalog supports place IDs for disambiguation.
func Resolve(place, country, id string) (Location, error) {
	cities, err := locations()
	if err != nil {
		return Location{}, fmt.Errorf("城市目录不可用: %w", err)
	}
	query := normalizeName(place)
	country = strings.ToUpper(strings.TrimSpace(country))
	var matches []Location
	for _, city := range cities {
		if country != "" && city.Country != country {
			continue
		}
		matched := id != "" && id == city.ID
		if id == "" {
			for _, name := range city.Names {
				if query == normalizeName(name) {
					matched = true
					break
				}
			}
		}
		if matched {
			city.Names = nil
			matches = append(matches, city)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return Location{}, fmt.Errorf("未找到城市 %q，请提供更完整的城市名、国家代码或已知经纬度", place)
	}
	if len(matches) > 5 {
		matches = matches[:5]
	}
	b, _ := json.Marshal(matches)
	return Location{}, fmt.Errorf("城市名称有歧义，请确认位置并使用对应 placeId: %s", b)
}
