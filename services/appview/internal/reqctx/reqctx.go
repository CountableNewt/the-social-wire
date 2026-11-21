// Package reqctx carries per-request state (authenticated viewer, request ID,
// trace context) through context.Context, as GatewayRequestContext did.
package reqctx

import "context"

// Auth is the verified caller, set by the auth middleware.
type Auth struct {
	DID string
	// AuthorizationForwardingValue is the full Authorization header value
	// ("DPoP <jwt>") forwarded to the viewer's PDS.
	AuthorizationForwardingValue string
	// DPoPProof is the gateway-bound proof from the DPoP header.
	DPoPProof string
	// UpstreamDPoPProof is a PDS-bound proof from X-ATProto-Upstream-DPoP.
	UpstreamDPoPProof string
}

// Trace is the W3C trace context for a request.
type Trace struct {
	TraceID string
	SpanID  string
	Sampled bool
}

type authKey struct{}
type requestIDKey struct{}
type traceKey struct{}

// WithAuth returns ctx carrying auth.
func WithAuth(ctx context.Context, auth *Auth) context.Context {
	return context.WithValue(ctx, authKey{}, auth)
}

// AuthFrom returns the verified caller, or nil on unauthenticated routes.
func AuthFrom(ctx context.Context) *Auth {
	auth, _ := ctx.Value(authKey{}).(*Auth)
	return auth
}

// ViewerDID returns the verified caller's DID, or "".
func ViewerDID(ctx context.Context) string {
	if auth := AuthFrom(ctx); auth != nil {
		return auth.DID
	}
	return ""
}

// WithRequestID returns ctx carrying id.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the request ID, or "".
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// WithTrace returns ctx carrying trace.
func WithTrace(ctx context.Context, trace Trace) context.Context {
	return context.WithValue(ctx, traceKey{}, trace)
}

// TraceFrom returns the request's trace context.
func TraceFrom(ctx context.Context) (Trace, bool) {
	trace, ok := ctx.Value(traceKey{}).(Trace)
	return trace, ok
}
