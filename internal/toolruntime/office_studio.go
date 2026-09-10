package toolruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type executionKey struct{}

// WithExecutionKey binds durable publications to the originating tool call,
// including approval resume, so a lost reply cannot create a second file.
func WithExecutionKey(ctx context.Context, sessionID, callID string) context.Context {
	sum := sha256.Sum256([]byte(sessionID + "\x00" + callID))
	return context.WithValue(ctx, executionKey{}, hex.EncodeToString(sum[:]))
}

func ExecutionKey(ctx context.Context) string {
	key, _ := ctx.Value(executionKey{}).(string)
	return key
}

// SetOfficeExecutor retains the normal mutation gate, hooks and audit while
// delegating immutable Office version publication to the application service.
func (r *Runtime) SetOfficeExecutor(f func(context.Context, string, string, json.RawMessage) ([]byte, string, string, error)) {
	r.officeExec = f
}

func (r *Runtime) SetDocumentText(f func(ctx context.Context, name string, raw []byte, media string) (text, kind, method string, pages int, err error)) {
	r.documentText = f
}
