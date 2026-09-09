package llmadapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

// ProxyVideoGenerator requires a connector rooted at the proxy's /v1 base.
// It never escapes a connector's pinned base or retries a submission.
type ProxyVideoGenerator interface {
	GenerateProxyVideo(context.Context, []byte, string, string) (MediaResult, error)
}

type videoEndpointNotFound struct{ cause error }

func (e *videoEndpointNotFound) Error() string { return e.cause.Error() }
func (e *videoEndpointNotFound) Unwrap() error { return e.cause }

// IsVideoEndpointNotFound is true only for a completed initial POST 404, not
// for transport/read failures, malformed acceptance, or a later poll 404.
func IsVideoEndpointNotFound(err error) bool {
	var missing *videoEndpointNotFound
	return errors.As(err, &missing)
}

func initialVideoEndpointError(raw []byte, status int, err error) error {
	var upstream *Error
	if status != http.StatusNotFound || !errors.As(err, &upstream) || upstream.Code != "HTTP_404" || upstream.Stage != StageHTTP {
		return err
	}
	// Even a contradictory HTTP response must not trigger another paid job
	// if it contains evidence that a task may have been accepted.
	var accepted struct {
		ID     string `json:"id"`
		TaskID string `json:"task_id"`
		Data   struct {
			ID     string `json:"id"`
			TaskID string `json:"task_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &accepted)
	if accepted.ID != "" || accepted.TaskID != "" || accepted.Data.ID != "" || accepted.Data.TaskID != "" {
		return err
	}
	return &videoEndpointNotFound{cause: err}
}

func (a *OpenAI) GenerateProxyVideo(ctx context.Context, secret []byte, model, prompt string) (MediaResult, error) {
	body, err := marshalBounded(map[string]any{"model": model, "prompt": prompt, "duration": 5}, a.o.MaxRequestBytes)
	if err != nil {
		return MediaResult{}, err
	}
	payload, err := io.ReadAll(body)
	if err != nil {
		return MediaResult{}, classify(err)
	}
	const taskPath = "video/generations"
	raw, status, err := a.mediaJSONRequest(ctx, secret, http.MethodPost, taskPath, payload)
	if err != nil {
		return MediaResult{}, err
	}
	var task proxyVideoCreateResponse
	if json.Unmarshal(raw, &task) != nil || (task.ID != "" && task.ID != task.TaskID) {
		return MediaResult{}, safeError("INVALID_RESPONSE", StageDecode, status, "proxy video task response is invalid")
	}
	out, done, err := task.result("")
	if err != nil || done {
		return out, err
	}
	taskID := out.ID
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
		raw, status, err = a.mediaJSONRequest(ctx, secret, http.MethodGet, taskPath+"/"+url.PathEscape(taskID), nil)
		if err != nil {
			return MediaResult{}, err
		}
		var envelope struct {
			Code string          `json:"code"`
			Data *proxyVideoTask `json:"data"`
		}
		if json.Unmarshal(raw, &envelope) != nil || envelope.Code != "success" || envelope.Data == nil {
			return MediaResult{}, safeError("INVALID_RESPONSE", StageDecode, status, "proxy video task status envelope is invalid")
		}
		out, done, err = envelope.Data.result(taskID)
		if err != nil || done {
			return out, err
		}
	}
}

type proxyVideoCreateResponse struct {
	ID string `json:"id"`
	proxyVideoTask
}

// Poll id is database metadata, not task identity. Only creation decodes it;
// polls bind to task_id, independently of the nested upstream task's id.
type proxyVideoTask struct {
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
	ResultURL string `json:"result_url"`
	Data      struct {
		Content struct {
			VideoURL string `json:"video_url"`
		} `json:"content"`
	} `json:"data"`
}

func (task proxyVideoTask) result(expectedID string) (MediaResult, bool, error) {
	invalid := func() (MediaResult, bool, error) {
		return MediaResult{}, false, safeError("INVALID_RESPONSE", StageDecode, 0, "proxy video task identity/status/result is invalid")
	}
	if !validProxyVideoTaskID(task.TaskID) || (expectedID != "" && task.TaskID != expectedID) {
		return invalid()
	}
	out := MediaResult{ID: task.TaskID, MIME: "video/mp4"}
	switch strings.ToLower(task.Status) {
	case "queued", "submitted", "pending", "running", "processing", "in_progress", "not_start":
		return out, false, nil
	case "success":
		// Nested upstream IDs/statuses are not proxy task identity or completion.
		// Only the outer SUCCESS authorizes reading the result URL.
		rawURL := task.ResultURL
		if rawURL == "" {
			rawURL = task.Data.Content.VideoURL
		}
		u, err := url.Parse(rawURL)
		if err != nil || len(rawURL) > 8192 || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Opaque != "" || u.Fragment != "" || strings.Contains(rawURL, "\\") || strings.IndexFunc(rawURL, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return invalid()
		}
		out.URL = u.String()
		return out, true, nil
	case "failure", "failed", "error", "cancelled", "canceled", "expired":
		return MediaResult{}, true, safeError("GENERATION_FAILED", StageHTTP, 0, "proxy video generation failed")
	default:
		return invalid()
	}
}

func validProxyVideoTaskID(id string) bool {
	if id == "" || len(id) > 200 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
