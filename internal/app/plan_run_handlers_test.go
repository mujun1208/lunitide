package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lunitide/lunitide/internal/agentorchestration"
	"github.com/lunitide/lunitide/internal/bridge"
)

const (
	planRunPlanID  = "01ARZ3NDEKTSV4RRFFQ69G5FA0"
	planRunNodeID  = "01ARZ3NDEKTSV4RRFFQ69G5FA1"
	planRunRootID  = "01ARZ3NDEKTSV4RRFFQ69G5FA2"
	planRunChildID = "01ARZ3NDEKTSV4RRFFQ69G5FA3"
)

func planRunEngine(t *testing.T) *Engine {
	t.Helper()
	ids := []string{planRunRootID, planRunChildID}
	c, err := agentorchestration.New(agentorchestration.NewMemoryRepository(), agentorchestration.Limits{MaxDepth: 3, MaxConcurrency: 8}, func() string { id := ids[0]; ids = ids[1:]; return id })
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(nil, "test")
	e.SetAgentCoordinator(c)
	return e
}

func planRunRequest(method bridge.Method, payload string) bridge.Request {
	return validRequest(string(method), payload)
}

func requireNoExecution(t *testing.T, response bridge.Response) map[string]any {
	t.Helper()
	if !response.OK {
		t.Fatalf("response=%#v", response)
	}
	raw, err := json.Marshal(response.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if started, ok := payload["executionStarted"].(bool); !ok || started {
		t.Fatalf("executionStarted=%#v payload=%s", payload["executionStarted"], raw)
	}
	return payload
}

func TestPlanRunHandlersRejectNonStrictPayloads(t *testing.T) {
	e := planRunEngine(t)
	for _, tc := range []struct {
		method  bridge.Method
		payload string
	}{
		{bridge.MethodPlanTodoCreate, `{"planId":"` + planRunPlanID + `","nodeId":"` + planRunNodeID + `","role":"planner","title":"root"}`},
		{bridge.MethodPlanTodoCreate, `{"planId":"` + planRunPlanID + `","nodeId":"` + planRunNodeID + `","role":"planner","title":"root","description":"","extra":true}`},
		{bridge.MethodPlanRunStart, `{"runId":"` + planRunRootID + `","extra":true}`},
		{bridge.MethodPlanRunJoin, `{"runId":"` + planRunRootID + `","mode":"some"}`},
		{bridge.MethodPlanRunSpawn, `{"parentRunId":"` + planRunRootID + `","nodeId":"` + planRunNodeID + `","role":"worker","title":"child"}`},
	} {
		r := e.Handle(context.Background(), planRunRequest(tc.method, tc.payload))
		if r.OK || r.Error == nil || r.Error.Code != "BRIDGE_SCHEMA_INVALID" {
			t.Fatalf("%s accepted %s: %#v", tc.method, tc.payload, r)
		}
	}
}

func TestPlanRunHandlersCoordinateWithoutExternalExecution(t *testing.T) {
	e := planRunEngine(t)
	created := requireNoExecution(t, e.Handle(context.Background(), planRunRequest(bridge.MethodPlanTodoCreate, `{"planId":"`+planRunPlanID+`","nodeId":"`+planRunNodeID+`","role":"planner","title":"root","description":""}`)))
	if created["run"].(map[string]any)["status"] != "queued" {
		t.Fatalf("created=%#v", created)
	}
	requireNoExecution(t, e.Handle(context.Background(), planRunRequest(bridge.MethodPlanRunStart, `{"runId":"`+planRunRootID+`"}`)))
	spawned := requireNoExecution(t, e.Handle(context.Background(), planRunRequest(bridge.MethodPlanRunSpawn, `{"parentRunId":"`+planRunRootID+`","nodeId":"`+planRunNodeID+`","role":"worker","title":"child","description":""}`)))
	child := spawned["run"].(map[string]any)
	if child["parentRunId"] != planRunRootID || child["depth"] != float64(1) {
		t.Fatalf("child=%#v", child)
	}
	joined := requireNoExecution(t, e.Handle(context.Background(), planRunRequest(bridge.MethodPlanRunJoin, `{"runId":"`+planRunRootID+`","mode":"all"}`)))
	if joined["run"].(map[string]any)["status"] != "joining" {
		t.Fatalf("joined=%#v", joined)
	}
	cancelled := requireNoExecution(t, e.Handle(context.Background(), planRunRequest(bridge.MethodPlanRunCancel, `{"runId":"`+planRunRootID+`"}`)))
	if cancelled["run"].(map[string]any)["status"] != "cancel_requested" {
		t.Fatalf("cancelled=%#v", cancelled)
	}
}
