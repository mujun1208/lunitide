package toolruntime

import (
	"fmt"
	"strconv"
	"strings"
)

// LocationFix is a coarse device position for weather.get. It is not a street address.
type LocationFix struct {
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	AccuracyMeters float64 `json:"accuracyMeters"`
	Timezone       string  `json:"timezone"`
	Source         string  `json:"source"`
}

func parseLocationLine(line string) (LocationFix, error) {
	line = strings.TrimSpace(line)
	switch line {
	case "LOCATION_PERMISSION":
		return LocationFix{}, fmt.Errorf("请在 Windows 隐私设置里允许桌面应用使用位置")
	case "LOCATION_UNKNOWN", "":
		return LocationFix{}, fmt.Errorf("没有读到本机位置。请打开系统定位服务后再试")
	}
	parts := strings.Split(line, "|")
	if len(parts) != 4 {
		return LocationFix{}, fmt.Errorf("没有读到本机位置。请打开系统定位服务后再试")
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lon, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	acc, err3 := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
	if err1 != nil || err2 != nil || err3 != nil || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return LocationFix{}, fmt.Errorf("没有读到本机位置。请打开系统定位服务后再试")
	}
	zone := ianaTimezone(strings.TrimSpace(parts[3]))
	return LocationFix{Latitude: lat, Longitude: lon, AccuracyMeters: acc, Timezone: zone, Source: "windows"}, nil
}

func ianaTimezone(windowsID string) string {
	if zone, ok := windowsToIANA[windowsID]; ok {
		return zone
	}
	if strings.Contains(windowsID, "/") {
		return windowsID
	}
	return "UTC"
}

var windowsToIANA = map[string]string{
	"China Standard Time":     "Asia/Shanghai",
	"Taipei Standard Time":    "Asia/Taipei",
	"Tokyo Standard Time":     "Asia/Tokyo",
	"Korea Standard Time":     "Asia/Seoul",
	"Singapore Standard Time": "Asia/Singapore",
	"GMT Standard Time":       "Europe/London",
	"W. Europe Standard Time": "Europe/Berlin",
	"Romance Standard Time":   "Europe/Paris",
	"Eastern Standard Time":   "America/New_York",
	"Central Standard Time":   "America/Chicago",
	"Pacific Standard Time":   "America/Los_Angeles",
	"UTC":                     "UTC",
}
