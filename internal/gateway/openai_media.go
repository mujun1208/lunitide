package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func (a *OpenAI) GenerateImage(ctx context.Context, secret []byte, model, prompt string) (MediaResult, error) {
	return a.generateMedia(ctx, secret, model, prompt, []string{"images/generations"}, parseImageMedia)
}

func (a *OpenAI) GenerateVideo(ctx context.Context, secret []byte, model, prompt string) (MediaResult, error) {
	return a.generateMedia(ctx, secret, model, prompt, []string{"videos/generations", "video/generations", "videos"}, parseVideoMedia)
}

func (a *OpenAI) generateMedia(ctx context.Context, secret []byte, model, prompt string, paths []string, parse func([]byte) (MediaResult, bool)) (MediaResult, error) {
	body, err := marshalBounded(map[string]any{"model": model, "prompt": prompt, "n": 1}, a.o.MaxRequestBytes)
	if err != nil {
		return MediaResult{}, err
	}
	payload, err := io.ReadAll(body)
	if err != nil {
		return MediaResult{}, classify(err)
	}
	var last error
	for _, p := range paths {
		req, reqErr := a.c.NewRequest(ctx, http.MethodPost, p, bytes.NewReader(payload))
		if reqErr != nil {
			last = classify(reqErr)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, doErr := doWithSecret(a.c, req, "Authorization", "Bearer ", secret)
		if doErr != nil {
			last = uncertain(doErr)
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if readErr != nil {
			last = uncertain(readErr)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			last = statusErrorReason(resp.StatusCode, boundedReason(bytes.NewReader(raw)))
			continue
		}
		out, ok := parse(raw)
		if !ok {
			last = safeError("INVALID_RESPONSE", StageDecode, resp.StatusCode, "generation response missing media")
			continue
		}
		return out, nil
	}
	if last == nil {
		last = safeError("UPSTREAM_UNAVAILABLE", StageHTTP, 0, "generation endpoint unavailable")
	}
	return MediaResult{}, last
}

func parseImageMedia(raw []byte) (MediaResult, bool) {
	var wrap struct {
		ID   string `json:"id"`
		Data []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &wrap) != nil || len(wrap.Data) == 0 {
		return MediaResult{}, false
	}
	item := wrap.Data[0]
	out := MediaResult{URL: strings.TrimSpace(item.URL), ID: strings.TrimSpace(wrap.ID), MIME: "image/png"}
	if item.B64JSON != "" {
		data, err := base64.StdEncoding.DecodeString(item.B64JSON)
		if err != nil {
			return MediaResult{}, false
		}
		out.Data = data
	}
	return out, out.URL != "" || len(out.Data) > 0
}

func parseVideoMedia(raw []byte) (MediaResult, bool) {
	if out, ok := parseImageMedia(raw); ok {
		if out.MIME == "image/png" && len(out.Data) > 0 {
			out.MIME = "video/mp4"
		} else if out.MIME == "image/png" {
			out.MIME = "video/mp4"
		}
		return out, true
	}
	var alt struct {
		ID     string `json:"id"`
		URL    string `json:"url"`
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &alt) != nil {
		return MediaResult{}, false
	}
	url := strings.TrimSpace(alt.URL)
	if url == "" {
		return MediaResult{}, false
	}
	return MediaResult{URL: url, ID: strings.TrimSpace(alt.ID), MIME: "video/mp4"}, true
}
