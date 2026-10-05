package requestid

import "context"

type ctxKey int

const requestIDKey ctxKey = 0

func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func FromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}
