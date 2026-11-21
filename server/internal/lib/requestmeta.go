package lib

import "context"

// RequestMeta is the request provenance an audit row records: the correlation
// id, the client address and the user agent. It is deliberately not a fiber
// type so repositories can read it from the context without importing fiber.
type RequestMeta struct {
	RequestID string
	IP        string
	UserAgent string
}

type requestMetaKey struct{}

// WithRequestMeta attaches provenance to the request context. Middlewares call
// it once per request; repositories read it back when appending an audit row.
func WithRequestMeta(ctx context.Context, meta RequestMeta) context.Context {
	return context.WithValue(ctx, requestMetaKey{}, meta)
}

// RequestMetaFrom returns the provenance stored on ctx, or the zero value when
// the context did not come from an HTTP request (tests, background jobs).
func RequestMetaFrom(ctx context.Context) RequestMeta {
	meta, _ := ctx.Value(requestMetaKey{}).(RequestMeta)
	return meta
}
