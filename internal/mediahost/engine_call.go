package mediahost

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/bridge"
)

func engineCall(ctx context.Context, engine EngineCaller, method string, payload json.RawMessage, outerDeadlineMS int) (bridge.Response, error) {
	if engine == nil {
		return bridge.Response{}, errors.New("engine unavailable")
	}
	return engine.Call(ctx, bridge.Request{
		Version:    bridge.Version,
		Kind:       "request",
		ID:         ulid.Make().String(),
		TraceID:    ulid.Make().String(),
		Method:     method,
		SentAt:     time.Now().UTC(),
		Payload:    payload,
		DeadlineMS: bridge.InnerDeadlineMS(method, outerDeadlineMS),
	})
}
