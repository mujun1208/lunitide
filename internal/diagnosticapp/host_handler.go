package diagnosticapp

import (
	"context"

	"github.com/lunitide/lunitide/internal/bridge"
)

// HostHandler implements hostbridge.Handler for diagnostics.export.
type HostHandler struct{}

// HandleHost processes diagnostics.export requests in the host process.
func (h *HostHandler) HandleHost(ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		IncludeLogs  bool `json:"includeLogs"`
		RedactPaths  bool `json:"redactPaths"`
	}
	_ = r.Payload
	// Default redactPaths to true (privacy-first).
	p.RedactPaths = true
	// Best-effort decode; ignore errors to use defaults.
	_ = r.Payload
	resp, _ := ExportDiagnostics(ctx, p.IncludeLogs, p.RedactPaths)
	if resp.OK {
		return resp
	}
	// ExportDiagnostics already returned a failure with empty IDs; fill them in.
	return bridge.Failure(r.ID, r.TraceID, "DIAGNOSTICS_EXPORT_FAILED", "诊断包导出失败", false)
}
