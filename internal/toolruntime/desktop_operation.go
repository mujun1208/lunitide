package toolruntime

import "context"

// The interactive desktop is a single shared resource across all conversations.
// Keep composite operations (focus/search/type/verify) together, while allowing
// cancellation of a queued task. Nested cc calls share their outer operation.
var desktopOperationSlot = make(chan struct{}, 1)

func acquireDesktopOperation(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case desktopOperationSlot <- struct{}{}:
		return func() { <-desktopOperationSlot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
