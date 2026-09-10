package meetings

import (
	"context"
	"strings"
)

type meetingIDCtxKey struct{}

func WithMeetingID(ctx context.Context, id string) context.Context {
	id = strings.TrimSpace(id)
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, meetingIDCtxKey{}, id)
}

func MeetingIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(meetingIDCtxKey{}).(string)
	return strings.TrimSpace(id)
}
