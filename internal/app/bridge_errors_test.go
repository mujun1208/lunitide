package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/compactionapp"
	"github.com/lunitide/lunitide/internal/handoffapp"
)

func errorTestRequest() bridge.Request {
	return bridge.Request{ID: "request-id", TraceID: "correlation-id", Method: "test.method"}
}

func TestRawBridgeErrorsAreRedacted(t *testing.T) {
	secret := `C:\Users\alice\AppData\lunitide.db provider-key=secret`
	tests := []struct {
		name, code, message string
		call                func() bridge.Response
	}{
		{"attachment", "ATTACHMENT_OPERATION_FAILED", "附件操作暂时不可用", func() bridge.Response { return attachmentFailure(errorTestRequest(), errors.New(secret)) }},
		{"handoff", "HANDOFF_OPERATION_FAILED", "交接操作暂时不可用", func() bridge.Response { return handoffFailure(errorTestRequest(), errors.New(secret)) }},
		{"compaction", "COMPACTION_PREVIEW_FAILED", "压缩预览暂时不可用", func() bridge.Response {
			return compactionFailure(errorTestRequest(), "COMPACTION_PREVIEW_FAILED", "压缩预览暂时不可用", errors.New(secret))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := tt.call()
			if response.Error == nil || response.Error.Code != tt.code || response.Error.Message != tt.message {
				t.Fatalf("error = %#v", response.Error)
			}
			if strings.Contains(response.Error.Message, secret) {
				t.Fatalf("raw internal error leaked: %q", response.Error.Message)
			}
			if response.Error.CorrelationID != "correlation-id" {
				t.Fatalf("correlation ID = %q", response.Error.CorrelationID)
			}
		})
	}
}

func TestExpectedErrorsHaveStablePublicMappings(t *testing.T) {
	r := errorTestRequest()
	tests := []struct {
		response bridge.Response
		code     string
	}{
		{attachmentFailure(r, attachmentapp.ErrScopeMismatch), "ATTACHMENT_SCOPE_MISMATCH"},
		{handoffFailure(r, handoffapp.ErrDestinationSessionNotFound), "HANDOFF_DESTINATION_SESSION_NOT_FOUND"},
		{compactionFailure(r, "fallback", "fallback", compactionapp.ErrConcurrentModification), "COMPACTION_VERSION_CONFLICT"},
		{compactionFailure(r, "fallback", "fallback", compactionapp.ErrNoMessagesToSummarize), "COMPACTION_SOURCE_EMPTY"},
	}
	for _, tt := range tests {
		if tt.response.Error == nil || tt.response.Error.Code != tt.code {
			t.Fatalf("error = %#v, want code %s", tt.response.Error, tt.code)
		}
	}
}
