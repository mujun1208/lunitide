package webviewhost

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/lunitide/lunitide/internal/hostbridge"
)

const TrustedOrigin = "https://app.lunitide.local"

const MaxUIQueue = 512

type BoundedQueue[T any] struct {
	items  []T
	max    int
	posted bool
}

func NewBoundedQueue[T any](max int) *BoundedQueue[T] { return &BoundedQueue[T]{max: max} }
func (q *BoundedQueue[T]) Push(v T) (accepted, notify bool) {
	if q.max < 1 || len(q.items) >= q.max {
		return false, false
	}
	q.items = append(q.items, v)
	if !q.posted {
		q.posted = true
		return true, true
	}
	return true, false
}
func (q *BoundedQueue[T]) Drain() []T { out := q.items; q.items = nil; q.posted = false; return out }
func (q *BoundedQueue[T]) Len() int   { return len(q.items) }

// ExternalActionAllowed defaults browser-initiated external actions to deny.
func ExternalActionAllowed(_ string) bool { return false }

// MicrophonePermissionAllowed is deliberately narrow: only the trusted main
// application document may request microphone access. WebView2 does not
// reliably mark getUserMedia requests as user initiated, so renderer controls
// the short-lived capture lifecycle instead of depending on that hint.
func MicrophonePermissionAllowed(raw string, microphone bool) bool {
	return microphone && NavigationAllowed(raw)
}

// NavigationAllowed is deliberately exact: only HTTPS documents on the fixed
// virtual host are permitted. Userinfo and non-default ports are rejected.
func NavigationAllowed(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || strings.ToLower(u.Host) != TrustedVirtualHost || u.User != nil {
		return false
	}
	return true
}

func NavigationInvalidatesGeneration(raw string) bool { return NavigationAllowed(raw) }

// GenerationCurrent gates asynchronous UI work captured before navigation.
// Keeping this COM-free makes the navigation race policy regression-testable.
func GenerationCurrent(captured, current uint64, closed bool) bool {
	return !closed && captured == current
}

// Dispatch performs potentially blocking Gateway/RPC work away from the UI
// thread and schedules replies back onto that thread. Rejected messages never
// produce a reply.
func Dispatch(ctx context.Context, gateway *hostbridge.Gateway, generation uint64, message hostbridge.Message, ui func(func()) bool, reply func([]byte) bool) {
	go func() {
		response, handled := gateway.HandleGeneration(ctx, generation, message)
		if !handled {
			return
		}
		raw, err := json.Marshal(response)
		if err != nil {
			return
		}
		if !ui(func() {
			if !reply(raw) || !response.OK {
				return
			}
			var request struct {
				Method string `json:"method"`
			}
			if json.Unmarshal(message.JSON, &request) != nil || (request.Method != "chat.start" && request.Method != "terminal.start" && request.Method != "tts.stream" && request.Method != "talk.start") {
				return
			}
			payload, _ := json.Marshal(response.Payload)
			var result struct {
				StreamID   string `json:"streamId"`
				TerminalID string `json:"terminalId"`
			}
			if json.Unmarshal(payload, &result) == nil {
				if result.StreamID == "" {
					result.StreamID = result.TerminalID
				}
			}
			if result.StreamID != "" {
				gateway.ActivateStream(generation, result.StreamID)
			}
		}) {
			gateway.CancelStreams(context.Background())
		}
	}()
}

// DeliverRoutedEvent guarantees ownership is returned exactly once, including
// encoding failure and consumer shutdown paths.
func DeliverRoutedEvent(routed hostbridge.RoutedEvent, marshal func(any) ([]byte, error), deliver func([]byte) bool) (delivered bool) {
	defer func() { routed.Acknowledge(delivered) }()
	raw, err := marshal(routed.Event)
	if err != nil {
		return false
	}
	return deliver(raw)
}
