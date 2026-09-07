package weather

import (
	"errors"
	"net/http"
	"time"
)

func forecastExpiry(raw string, checked time.Time) (time.Time, error) {
	if until, err := http.ParseTime(raw); err == nil {
		// Preserve normal upstream freshness exactly: do not revalidate
		// before Expires, or turn an expired response into a fresh 15 minutes.
		if until.After(checked.Add(24 * time.Hour)) {
			return time.Time{}, errors.New("天气服务返回了异常的缓存有效期，请稍后重试")
		}
		return until, nil
	}
	return checked.Add(15 * time.Minute), nil
}

func forecastLastModified(raw string, checked time.Time) string {
	if len(raw) > 128 {
		return ""
	}
	for _, char := range []byte(raw) {
		if char < 32 || char > 126 {
			return ""
		}
	}
	if modified, err := http.ParseTime(raw); err == nil && !modified.After(checked) {
		// MET requires the original Last-Modified value, not an invented or
		// re-formatted timestamp. Invalid/future values are not sent back.
		return raw
	}
	return ""
}
