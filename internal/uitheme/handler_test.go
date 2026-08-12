package uitheme

import (
	"context"
	"encoding/json"
	"github.com/lunitide/lunitide/internal/bridge"
	"testing"
)

func TestHandlerAppliesOnlyExplicitTheme(t *testing.T) {
	var values []bool
	h := &Handler{}
	h.Bind(func(d bool) bool { values = append(values, d); return true })
	for _, x := range []struct {
		p  string
		ok bool
	}{{`{"theme":"dark"}`, true}, {`{"theme":"light"}`, true}, {`{"theme":"system"}`, false}, {`{"theme":"dark","capability":"shell"}`, false}} {
		r := h.HandleHost(context.Background(), bridge.Request{ID: "request", TraceID: "trace", Payload: json.RawMessage(x.p)})
		if r.OK != x.ok {
			t.Fatalf("payload %s: OK=%v", x.p, r.OK)
		}
	}
	if len(values) != 2 || !values[0] || values[1] {
		t.Fatalf("values=%v", values)
	}
}
