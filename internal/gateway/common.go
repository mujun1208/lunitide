package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

func marshalBounded(v any, max int) (*bytes.Reader, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, safeError("INVALID_REQUEST", StageDecode, 0, "invalid request")
	}
	if len(b) > max {
		return nil, safeError("REQUEST_TOO_LARGE", StageDecode, 0, "request exceeds size budget")
	}
	return bytes.NewReader(b), nil
}

func strictJSON(r io.Reader, dst any) error {
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		if code := networkpolicy.ErrorCode(err); code != "" {
			return classify(err)
		}
		return safeError("MALFORMED_RESPONSE", StageDecode, 0, "upstream returned malformed JSON")
	}
	if err := d.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return safeError("MALFORMED_RESPONSE", StageDecode, 0, "upstream returned trailing JSON")
	}
	return nil
}

func safeError(code string, stage Stage, status int, message string) *Error {
	return &Error{Code: code, Stage: stage, HTTPStatus: status, Message: message}
}

func classify(err error) *Error {
	if err == nil {
		return nil
	}
	var ge *Error
	if errors.As(err, &ge) {
		return ge
	}
	code := string(networkpolicy.ErrorCode(err))
	if code == "" {
		code = "CONNECTION_FAILED"
	}
	return safeError(code, StageConnect, 0, "upstream connection failed")
}

func statusError(status int) *Error {
	return safeError("HTTP_"+strconv.Itoa(status), StageHTTP, status, http.StatusText(status))
}

func doWithSecret(c Connector, req *http.Request, name, prefix string, secret []byte) (*http.Response, error) {
	if req == nil || req.URL == nil || !strings.EqualFold(req.URL.Scheme, "https") {
		return nil, safeError("HTTPS_REQUIRED", StageConnect, 0, "credentials require HTTPS")
	}
	// Go strings cannot be guaranteed to be zeroed. Minimize lifetime and remove
	// the header immediately after Do returns.
	req.Header.Set(name, prefix+string(secret))
	defer req.Header.Del(name)
	return c.Do(req)
}

func retryableBeforeConnect(err error) bool {
	if err != nil {
		c := networkpolicy.ErrorCode(err)
		return c == networkpolicy.CodeConnectionRefused || c == networkpolicy.CodeDNSError || c == networkpolicy.CodeTLSError
	}
	return false
}

func retryableStatus(status int) bool {
	return status == 429 || status == 500 || status == 502 || status == 503 || status == 504
}
func attempts(o Options, in Request, stream bool) int {
	n := o.MaxAttempts
	if in.MaxAttempts > 0 {
		n = in.MaxAttempts
	}
	if n < 1 || stream {
		return 1
	}
	return n
}
func uncertain(err error) *Error {
	if networkpolicy.ErrorCode(err) == networkpolicy.CodeTimeout {
		return safeError("OUTCOME_UNKNOWN", StageConnect, 0, "upstream outcome is unknown")
	}
	return classify(err)
}

const maxRetryDelay = 30 * time.Second

func retryDelay(resp *http.Response, attempt int, base time.Duration) time.Duration {
	if resp != nil {
		r := strings.TrimSpace(resp.Header.Get("Retry-After"))
		if n, err := strconv.Atoi(r); err == nil && n >= 0 {
			if n > int(maxRetryDelay/time.Second) {
				return maxRetryDelay
			}
			return time.Duration(n) * time.Second
		}
		if t, err := http.ParseTime(r); err == nil {
			if d := time.Until(t); d > 0 {
				if d > maxRetryDelay {
					return maxRetryDelay
				}
				return d
			}
		}
	}
	if base <= 0 {
		return 0
	}
	d := base
	for i := 1; i < attempt; i++ {
		if d >= maxRetryDelay/2 {
			return maxRetryDelay
		}
		d *= 2
	}
	if d > maxRetryDelay {
		return maxRetryDelay
	}
	return d
}

func waitRetry(ctx context.Context, d time.Duration) error {
	if d > maxRetryDelay {
		d = maxRetryDelay
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 || d >= remaining {
			return classify(context.DeadlineExceeded)
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return classify(ctx.Err())
	case <-t.C:
		return nil
	}
}

func sseData(event []byte) (string, string) {
	var typ string
	var data []string
	for _, line := range strings.Split(string(event), "\n") {
		if strings.HasPrefix(line, "event:") {
			typ = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	return typ, strings.Join(data, "\n")
}

func normalizeUsage(input, output, total int) Usage {
	if total == 0 {
		total = input + output
	}
	return Usage{InputTokens: input, OutputTokens: output, TotalTokens: total}
}

func validUsage(input, output, total int) bool {
	const maxTokens = 1 << 30
	return input >= 0 && output >= 0 && total >= 0 && input <= maxTokens && output <= maxTokens && total <= maxTokens
}
