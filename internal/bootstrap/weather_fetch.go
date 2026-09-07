package bootstrap

import (
	"context"
	"errors"
	"net/url"

	"github.com/lunitide/lunitide/internal/buildinfo"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/weather"
)

func weatherFetchOptions(rawURL, lastModified string) (networkpolicy.FetchOptions, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme+"://"+u.Host+u.Path != weather.Endpoint || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return networkpolicy.FetchOptions{}, errors.New("天气传输仅接受已配置的 MET 预报接口")
	}
	return networkpolicy.FetchOptions{
		UserAgent:       "Lunitide/" + buildinfo.Version + " (weather; contact: 114921798@qq.com)",
		IfModifiedSince: lastModified,
	}, nil
}

func fetchWeather(ctx context.Context, rawURL, lastModified string) (networkpolicy.FetchResult, error) {
	options, err := weatherFetchOptions(rawURL, lastModified)
	if err != nil {
		return networkpolicy.FetchResult{}, err
	}
	return networkpolicy.Fetch(ctx, rawURL, options)
}
