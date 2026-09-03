package app

import (
	"context"
	"encoding/json"

	"github.com/lunitide/lunitide/internal/datasourceapp"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func datasourceToolDefinitions() []gateway.ToolDefinition {
	return []gateway.ToolDefinition{
		{Name: "datasource.query", Description: "Run one SQL statement against a probed PostgreSQL/MySQL binding. Remote connections are read-only (SELECT only); a local connection also allows writes (INSERT/UPDATE/DDL). Prefer bindingId from the workbench stock map. Never invent a DSN. Unprobed connections return 连接未探测.", Schema: []byte(`{"type":"object","properties":{"connectionId":{"type":"string"},"bindingId":{"type":"string"},"sql":{"type":"string","minLength":1,"maxLength":16384},"maxRows":{"type":"integer","minimum":1,"maximum":1000}},"required":["sql"],"additionalProperties":false}`)},
	}
}

func (e *Engine) executeDatasourceQuery(ctx context.Context, args json.RawMessage) (toolruntime.Result, error) {
	if e.datasource == nil {
		return toolruntime.Result{}, datasourceapp.ErrServiceUnavailable
	}
	var p struct {
		ConnectionID string `json:"connectionId"`
		BindingID    string `json:"bindingId"`
		SQL          string `json:"sql"`
		MaxRows      int    `json:"maxRows"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return toolruntime.Result{}, datasourceapp.ErrPayloadInvalid
	}
	res, err := e.datasource.Query(ctx, datasourceapp.QueryInput{
		ConnectionID: p.ConnectionID, BindingID: p.BindingID, SQL: p.SQL, MaxRows: p.MaxRows,
	})
	if err != nil {
		return toolruntime.Result{}, err
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return toolruntime.Result{}, err
	}
	return toolruntime.Result{Output: string(raw)}, nil
}
