package app

import (
	"context"
	"errors"
	"net/url"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

type videoProxyAdapter struct {
	llmadapter.Adapter
	proxy func(context.Context) (llmadapter.Adapter, error)
}

func (a *videoProxyAdapter) GenerateVideo(ctx context.Context, secret []byte, model, prompt string) (llmadapter.MediaResult, error) {
	if _, ok := adapterAs[llmadapter.VideoGenerator](a.Adapter); !ok {
		return llmadapter.MediaResult{}, errors.New("adapter does not support video generation")
	}
	out, err := generateVideoThrough(ctx, a.Adapter, secret, model, prompt)
	if !llmadapter.IsVideoEndpointNotFound(err) || ctx.Err() != nil {
		return out, err
	}
	proxy, err := a.proxy(ctx)
	if err != nil {
		return llmadapter.MediaResult{}, err
	}
	generator, ok := proxy.(llmadapter.ProxyVideoGenerator)
	if !ok {
		return llmadapter.MediaResult{}, errors.New("adapter does not support proxy video generation")
	}
	return generator.GenerateProxyVideo(ctx, secret, model, prompt)
}

func (e *Engine) withVideoProxy(primary llmadapter.Adapter, p provider.Provider) llmadapter.Adapter {
	return &videoProxyAdapter{Adapter: primary, proxy: func(ctx context.Context) (llmadapter.Adapter, error) {
		u, err := url.Parse(p.BaseURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("invalid proxy video origin")
		}
		// Build a separate policy-checked connector on the credential's same
		// origin. Never use ../ or mutate the primary connector's pinned base.
		proxy := p
		proxy.BaseURL = (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/v1"}).String()
		return e.adapter(ctx, proxy)
	}}
}
