package bootstrap

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/buildinfo"
	"github.com/lunitide/lunitide/internal/weather"
)

func TestWeatherFetcherIdentifiesOnlyConfiguredMETEndpoint(t *testing.T) {
	const modified = "Mon, 07 Sep 2026 01:00:00 GMT"
	options, err := weatherFetchOptions(weather.Endpoint+"?lat=31.8600&lon=117.2700", modified)
	if err != nil || !strings.Contains(options.UserAgent, "Lunitide/"+buildinfo.Version) || !strings.Contains(options.UserAgent, "114921798@qq.com") || options.IfModifiedSince != modified || options.Policy.AllowHTTP {
		t.Fatalf("weather transport configuration invalid: %v", err)
	}
	for _, raw := range []string{"https://example.com/", "http://api.met.no/weatherapi/locationforecast/2.0/compact", "https://api.met.no.evil.test/weatherapi/locationforecast/2.0/compact", "https://api.met.no/other", "https://user@api.met.no/weatherapi/locationforecast/2.0/compact", weather.Endpoint + "#fragment"} {
		if got, err := weatherFetchOptions(raw, modified); err == nil || got.UserAgent != "" {
			t.Fatalf("contact header configured for unrelated URL: %s", raw)
		}
	}
}
