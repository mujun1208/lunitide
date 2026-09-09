package llmadapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (a *OpenAI) GenerateImage(ctx context.Context, secret []byte, model, prompt string) (MediaResult, error) {
	return a.generateMedia(ctx, secret, model, prompt, []string{"images/generations"}, parseImageMedia)
}

func (a *OpenAI) GenerateVideo(ctx context.Context, secret []byte, model, prompt string) (MediaResult, error) {
	if looksLikeVolcVideoModel(model) {
		return a.generateVolcVideo(ctx, secret, model, prompt)
	}
	return a.generateMedia(ctx, secret, model, prompt, []string{"videos/generations", "video/generations", "videos"}, parseVideoMedia)
}

func looksLikeVolcVideoModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(m, "seedance") || strings.Contains(m, "doubao-video")
}

func (a *OpenAI) generateVolcVideo(ctx context.Context, secret []byte, model, prompt string) (MediaResult, error) {
	body, err := marshalBounded(map[string]any{
		"model":   model,
		"content": []map[string]string{{"type": "text", "text": prompt}},
	}, a.o.MaxRequestBytes)
	if err != nil {
		return MediaResult{}, err
	}
	payload, err := io.ReadAll(body)
	if err != nil {
		return MediaResult{}, classify(err)
	}
	const taskPath = "contents/generations/tasks"
	raw, status, requestErr := a.mediaJSONRequest(ctx, secret, http.MethodPost, taskPath, payload)
	if requestErr != nil {
		return MediaResult{}, initialVideoEndpointError(raw, status, requestErr)
	}
	out, taskID, taskStatus, ok := parseVolcVideoTask(raw)
	if !ok {
		return MediaResult{}, safeError("INVALID_RESPONSE", StageDecode, status, "video task response missing valid id/status")
	}
	if out.URL != "" || len(out.Data) > 0 {
		return out, nil
	}
	if videoTaskFailed(taskStatus) {
		return MediaResult{}, safeError("GENERATION_FAILED", StageHTTP, status, "video generation failed")
	}
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(1500 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return MediaResult{}, classify(ctx.Err())
			case <-timer.C:
			}
		}
		pollPath := taskPath + "/" + url.PathEscape(taskID)
		pollRaw, pollStatus, pollErr := a.mediaJSONRequest(ctx, secret, http.MethodGet, pollPath, nil)
		if pollErr != nil {
			return MediaResult{}, pollErr
		}
		var pollID string
		out, pollID, taskStatus, ok = parseVolcVideoTask(pollRaw)
		if !ok || pollID != taskID {
			return MediaResult{}, safeError("INVALID_RESPONSE", StageDecode, pollStatus, "video task status is invalid")
		}
		if out.URL != "" || len(out.Data) > 0 {
			return out, nil
		}
		if videoTaskFailed(taskStatus) {
			return MediaResult{}, safeError("GENERATION_FAILED", StageHTTP, pollStatus, "video generation failed")
		}
	}
}

func (a *OpenAI) mediaJSONRequest(ctx context.Context, secret []byte, method, requestPath string, payload []byte) ([]byte, int, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := a.c.NewRequest(ctx, method, requestPath, body)
	if err != nil {
		return nil, 0, classify(err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := doWithSecret(a.c, req, "Authorization", "Bearer ", secret)
	if err != nil {
		return nil, 0, uncertain(err)
	}
	const maxTaskResponseBytes = 2 << 20
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxTaskResponseBytes+1))
	resp.Body.Close()
	if readErr != nil {
		return nil, resp.StatusCode, uncertain(readErr)
	}
	if len(raw) > maxTaskResponseBytes {
		return nil, resp.StatusCode, safeError("RESPONSE_TOO_LARGE", StageDecode, resp.StatusCode, "video task response exceeds size budget")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return raw, resp.StatusCode, statusErrorReason(resp.StatusCode, boundedReason(bytes.NewReader(raw)))
	}
	return raw, resp.StatusCode, nil
}

func parseVolcVideoTask(raw []byte) (MediaResult, string, string, bool) {
	var task struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Content struct {
			VideoURL string `json:"video_url"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &task) != nil {
		return MediaResult{}, "", "", false
	}
	id, status := strings.TrimSpace(task.ID), strings.ToLower(strings.TrimSpace(task.Status))
	validID := id != "" && len(id) <= 200
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			validID = false
		}
	}
	out := MediaResult{ID: id, MIME: "video/mp4"}
	switch status {
	case "", "submitted", "queued", "pending", "running":
		return out, id, status, validID
	case "succeeded":
		u, err := url.Parse(strings.TrimSpace(task.Content.VideoURL))
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
			return out, id, status, false
		}
		out.URL = u.String()
		return out, id, status, validID
	default:
		return out, id, status, validID && videoTaskFailed(status)
	}
}

func videoTaskFailed(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "error", "cancelled", "canceled", "expired":
		return true
	default:
		return false
	}
}

func (a *OpenAI) generateMedia(ctx context.Context, secret []byte, model, prompt string, paths []string, parse func([]byte) (MediaResult, bool)) (MediaResult, error) {
	params := map[string]any{"model": model, "prompt": prompt, "n": 1}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "gpt-image-") {
		params["size"] = "1024x1024"
	}
	if strings.Contains(strings.ToLower(model), "seedream") {
		// Seedream 4/5 use symbolic sizes; the generic 1024 default is invalid for 5.
		params["size"] = "2K"
		if strings.Contains(strings.ToLower(model), "seedream-3") {
			params["size"] = "1024x1024"
		}
		params["response_format"] = "url"
		delete(params, "n")
	}
	body, err := marshalBounded(params, a.o.MaxRequestBytes)
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
		req.Header.Set("Accept", "application/json")
		resp, doErr := doWithSecret(a.c, req, "Authorization", "Bearer ", secret)
		if doErr != nil {
			return MediaResult{}, uncertain(doErr)
		}
		const maxMediaResponseBytes = 8 << 20
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxMediaResponseBytes+1))
		resp.Body.Close()
		if len(raw) > maxMediaResponseBytes {
			return MediaResult{}, safeError("RESPONSE_TOO_LARGE", StageDecode, resp.StatusCode, "media response exceeds size budget")
		}
		if readErr != nil {
			return MediaResult{}, uncertain(readErr)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			last = statusErrorReason(resp.StatusCode, boundedReason(bytes.NewReader(raw)))
			if resp.StatusCode == http.StatusNotFound {
				continue
			}
			return MediaResult{}, last
		}
		out, ok := parse(raw)
		if !ok {
			return MediaResult{}, safeError("INVALID_RESPONSE", StageDecode, resp.StatusCode, "generation response missing media")
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
