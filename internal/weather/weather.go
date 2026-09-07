// Package weather reads free MET Norway JSON forecasts. City resolution stays
// offline; only the requested coordinates are sent through the governed HTTP
// fetcher. It does not scrape search pages or present forecasts as observations.
package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

const Endpoint = "https://api.met.no/weatherapi/locationforecast/2.0/compact"

type Request struct {
	Place     string   `json:"place"`
	Country   string   `json:"country"`
	PlaceID   string   `json:"placeId"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Timezone  string   `json:"timezone"`
	Days      int      `json:"days"`
}

type Day struct {
	Date       string   `json:"date"`
	From       string   `json:"from"`
	Through    string   `json:"through"`
	Minimum    float64  `json:"sampleMinC"`
	Maximum    float64  `json:"sampleMaxC"`
	Samples    int      `json:"samples"`
	Conditions []string `json:"conditions,omitempty"`
}
type Result struct {
	Kind             string    `json:"kind"`
	Source           string    `json:"source"`
	Attribution      string    `json:"attribution"`
	License          string    `json:"license"`
	Location         Location  `json:"location"`
	RetrievedAt      time.Time `json:"retrievedAt"`
	CheckedAt        time.Time `json:"checkedAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
	Cached           bool      `json:"cached"`
	Notice           string    `json:"notice"`
	Days             []Day     `json:"days"`
	DetailsTruncated bool      `json:"detailsTruncated,omitempty"`
}

// JSONSummary preserves complete dates, temperatures and provenance within the
// model's tool-result budget. Optional condition lists may be omitted; slicing
// JSON bytes here would give the model an invalid or misleading forecast.
func (r Result) JSONSummary() ([]byte, error) {
	const maxBytes = 3840
	encoded, err := json.Marshal(r)
	if err != nil || len(encoded) <= maxBytes {
		return encoded, err
	}
	r.Days = append([]Day(nil), r.Days...)
	for i := range r.Days {
		r.Days[i].Conditions = nil
	}
	r.DetailsTruncated = true
	encoded, err = json.Marshal(r)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxBytes {
		return nil, errors.New("天气结果超过可完整展示的范围，请缩短查询天数")
	}
	return encoded, nil
}

type condition struct {
	Summary struct {
		Symbol string `json:"symbol_code"`
	} `json:"summary"`
}
type forecast struct {
	Properties struct {
		Meta struct {
			Updated time.Time         `json:"updated_at"`
			Units   map[string]string `json:"units"`
		} `json:"meta"`
		Timeseries []struct {
			Time time.Time `json:"time"`
			Data struct {
				Instant struct {
					Details struct {
						Temperature *float64 `json:"air_temperature"`
					} `json:"details"`
				} `json:"instant"`
				Next1 condition `json:"next_1_hours"`
				Next6 condition `json:"next_6_hours"`
			} `json:"data"`
		} `json:"timeseries"`
	} `json:"properties"`
}
type cacheEntry struct {
	data             forecast
	fetched, expires time.Time
	checked          time.Time
	lastModified     string
}
type Client struct {
	mu    sync.Mutex
	cache map[string]cacheEntry
	gate  chan struct{}
	Fetch func(context.Context, string) (networkpolicy.FetchResult, error)
	// Optional conditional transport; simple embedders and test fakes can
	// keep the original Fetch callback and receive ordinary 200 responses.
	FetchConditional func(context.Context, string, string) (networkpolicy.FetchResult, error)
	Now              func() time.Time
}

func New(fetch func(context.Context, string) (networkpolicy.FetchResult, error), now func() time.Time) *Client {
	if now == nil {
		now = time.Now
	}
	return &Client{cache: make(map[string]cacheEntry), gate: make(chan struct{}, 1), Fetch: fetch, Now: now}
}

func (c *Client) Get(ctx context.Context, req Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if len(req.Place) > 200 || len(req.Country) > 2 || len(req.PlaceID) > 12 || len(req.Timezone) > 64 || req.Days < 0 || req.Days > 7 {
		return Result{}, errors.New("天气参数超出范围")
	}
	if req.Days == 0 {
		req.Days = 3
	}
	var location Location
	var err error
	if req.Latitude != nil || req.Longitude != nil {
		if req.Latitude == nil || req.Longitude == nil || req.PlaceID != "" || req.Place != "" {
			return Result{}, errors.New("请提供城市或完整经纬度，不要混用")
		}
		lat, lon := *req.Latitude, *req.Longitude
		if math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			return Result{}, errors.New("经纬度无效")
		}
		location = Location{Name: "指定坐标", Latitude: math.Trunc(lat*10000) / 10000, Longitude: math.Trunc(lon*10000) / 10000, Timezone: req.Timezone}
		if location.Timezone == "" {
			location.Timezone = "UTC"
		}
	} else {
		if strings.TrimSpace(req.Place) == "" && req.PlaceID == "" {
			return Result{}, errors.New("请提供城市名称或已知经纬度")
		}
		location, err = Resolve(req.Place, req.Country, req.PlaceID)
		if err != nil {
			return Result{}, err
		}
	}
	zone, err := time.LoadLocation(location.Timezone)
	if err != nil {
		return Result{}, errors.New("无效的 IANA 时区")
	}
	query := fmt.Sprintf("%s?lat=%.4f&lon=%.4f", Endpoint, location.Latitude, location.Longitude)
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	entry, cached, err := c.load(ctx, query)
	if err != nil {
		return Result{}, err
	}
	now := c.Now().In(zone)
	earliest := now.Add(-time.Hour)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	if earliest.Before(dayStart) {
		earliest = dayStart
	}
	cutoff := time.Date(now.Year(), now.Month(), now.Day()+req.Days, 0, 0, 0, 0, zone)
	groups := map[string]*Day{}
	for _, point := range entry.data.Properties.Timeseries {
		temp := point.Data.Instant.Details.Temperature
		if temp == nil || *temp < -100 || *temp > 70 || point.Time.Before(earliest) || !point.Time.Before(cutoff) {
			continue
		}
		local := point.Time.In(zone)
		key := local.Format("2006-01-02")
		day := groups[key]
		if day == nil {
			day = &Day{Date: key, From: local.Format(time.RFC3339), Minimum: *temp, Maximum: *temp}
			groups[key] = day
		}
		day.Through = local.Format(time.RFC3339)
		day.Samples++
		day.Minimum = math.Min(day.Minimum, *temp)
		day.Maximum = math.Max(day.Maximum, *temp)
		symbol := point.Data.Next1.Summary.Symbol
		if symbol == "" {
			symbol = point.Data.Next6.Summary.Symbol
		}
		symbol = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(symbol, "_day"), "_night"), "_polartwilight")
		if symbol != "" && len(symbol) < 64 {
			found := false
			for _, item := range day.Conditions {
				if item == symbol {
					found = true
				}
			}
			if !found && len(day.Conditions) < 8 {
				day.Conditions = append(day.Conditions, symbol)
			}
		}
	}
	days := make([]Day, 0, len(groups))
	for _, d := range groups {
		days = append(days, *d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	if len(days) == 0 {
		return Result{}, errors.New("天气服务没有所请求日期的有效预报，不能用旧数据作当前天气")
	}
	return Result{Kind: "weather_forecast", Source: query, Attribution: "Weather: MET Norway; city coordinates: GeoNames", License: "https://creativecommons.org/licenses/by/4.0/", Location: location, RetrievedAt: entry.fetched, CheckedAt: entry.checked, UpdatedAt: entry.data.Properties.Meta.Updated, Cached: cached, Notice: "预报而非实测；摄氏温度范围取所列 from/through 时间内的预报采样，今天可能只含剩余时段。按实际日期、范围和更新时间回答，不推算缺失时段。", Days: days}, nil
}

func (c *Client) load(ctx context.Context, query string) (cacheEntry, bool, error) {
	if err := ctx.Err(); err != nil {
		return cacheEntry{}, false, err
	}
	if entry, ok := c.cached(query); ok {
		return entry, true, nil
	}
	// Coalesce misses without goroutines surviving a cancelled request.
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return cacheEntry{}, false, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return cacheEntry{}, false, err
	}
	if entry, ok := c.cached(query); ok {
		return entry, true, nil
	}
	if c.Fetch == nil && c.FetchConditional == nil {
		return cacheEntry{}, false, errors.New("天气网络服务未启用")
	}
	c.mu.Lock()
	previous, hasPrevious := c.cache[query]
	c.mu.Unlock()
	modified := ""
	if c.FetchConditional != nil && hasPrevious && !c.Now().Before(previous.checked) {
		modified = previous.lastModified
	}
	var page networkpolicy.FetchResult
	var err error
	if c.FetchConditional != nil {
		page, err = c.FetchConditional(ctx, query, modified)
	} else {
		page, err = c.Fetch(ctx, query)
	}
	if err != nil {
		return cacheEntry{}, false, err
	}
	if err = ctx.Err(); err != nil {
		return cacheEntry{}, false, err
	}
	u, err := url.Parse(page.FinalURL)
	if err != nil || u.Scheme != "https" || u.Host != "api.met.no" || u.Path != "/weatherapi/locationforecast/2.0/compact" {
		return cacheEntry{}, false, errors.New("天气服务返回了非预期地址")
	}
	expected, _ := url.Parse(query)
	actualParams, paramsErr := url.ParseQuery(u.RawQuery)
	if paramsErr != nil || len(actualParams) != 2 || len(actualParams["lat"]) != 1 || len(actualParams["lon"]) != 1 || actualParams.Get("lat") != expected.Query().Get("lat") || actualParams.Get("lon") != expected.Query().Get("lon") {
		return cacheEntry{}, false, errors.New("天气服务返回的位置与请求位置不符")
	}
	if page.Status == http.StatusNotModified {
		if !hasPrevious || modified == "" {
			return cacheEntry{}, false, errors.New("天气服务返回未更新，但本地没有对应的有效预报可复用")
		}
		checked := c.Now().UTC()
		expires, err := forecastExpiry(page.Expires, checked)
		if err != nil {
			return cacheEntry{}, false, err
		}
		previous.checked, previous.expires = checked, expires
		if revised := forecastLastModified(page.LastModified, checked); revised != "" {
			previous.lastModified = revised
		}
		c.store(query, previous)
		return previous, true, nil
	}
	if page.Status != http.StatusOK {
		return cacheEntry{}, false, fmt.Errorf("天气服务暂不可用（HTTP %d），请稍后重试", page.Status)
	}
	if page.Truncated || len(page.Body) > 1<<20 || !strings.Contains(strings.ToLower(page.ContentType), "json") {
		return cacheEntry{}, false, errors.New("天气接口内容不完整或格式无效")
	}
	var data forecast
	decodeErr := json.Unmarshal(page.Body, &data)
	fetched := c.Now().UTC()
	if decodeErr != nil || len(data.Properties.Timeseries) == 0 || len(data.Properties.Timeseries) > 1000 || data.Properties.Meta.Updated.IsZero() || data.Properties.Meta.Updated.After(fetched.Add(10*time.Minute)) || data.Properties.Meta.Units["air_temperature"] != "celsius" {
		return cacheEntry{}, false, errors.New("天气接口没有有效预报数据")
	}
	expires, err := forecastExpiry(page.Expires, fetched)
	if err != nil {
		return cacheEntry{}, false, err
	}
	entry := cacheEntry{data: data, fetched: fetched, checked: fetched, expires: expires, lastModified: forecastLastModified(page.LastModified, fetched)}
	c.store(query, entry)
	return entry, false, nil
}

func (c *Client) store(query string, entry cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.cache[query]; !exists && len(c.cache) >= 64 {
		var oldestKey string
		var oldest time.Time
		for k, v := range c.cache {
			if oldestKey == "" || v.fetched.Before(oldest) {
				oldestKey, oldest = k, v.fetched
			}
		}
		delete(c.cache, oldestKey)
	}
	c.cache[query] = entry
}

func (c *Client) cached(query string) (cacheEntry, bool) {
	now := c.Now()
	c.mu.Lock()
	entry, ok := c.cache[query]
	c.mu.Unlock()
	return entry, ok && !now.Before(entry.fetched) && !now.Before(entry.checked) && now.Before(entry.expires)
}
