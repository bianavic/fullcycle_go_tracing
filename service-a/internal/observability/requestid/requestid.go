package requestid

import "context"

type ctxKey int

const requestIDKey ctxKey = 0

// With returns a new context carrying the given request ID.
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// FromContext returns the request ID stored by With, or "" if none was set.
func FromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}
