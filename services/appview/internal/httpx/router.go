// Package httpx is the small routing layer every module registers routes on.
//
// Handlers and middleware return errors, as Hummingbird's did, so an outer
// middleware (XRPC error envelopes, request tracing) can observe and render
// failures raised by inner middleware or handlers. The router writes any error
// that reaches the top.
package httpx

import (
	"context"
	"net/http"
	"slices"
	"sync"
)

// Handler serves a request and may return an error for an outer layer to render.
type Handler func(w http.ResponseWriter, r *http.Request) error

// Middleware wraps a Handler.
type Middleware func(next Handler) Handler

// Router dispatches with net/http's ServeMux patterns ("GET /path",
// "POST /xrpc/{nsid}") under router-wide middleware.
type Router struct {
	mux        *http.ServeMux
	middleware []Middleware
	handler    Handler
	build      sync.Once
}

// NewRouter returns an empty router.
func NewRouter() *Router {
	return &Router{mux: http.NewServeMux()}
}

// Use appends router-wide middleware. Router-wide middleware also runs for
// unmatched paths, like Hummingbird router middleware. Call before serving.
func (rt *Router) Use(middleware ...Middleware) {
	rt.middleware = append(rt.middleware, middleware...)
}

// Group returns a route group with additional middleware, applied in order
// (the first runs outermost).
func (rt *Router) Group(middleware ...Middleware) *Group {
	return &Group{router: rt, middleware: slices.Clone(middleware)}
}

// Get registers a GET (and HEAD) route with no group middleware.
func (rt *Router) Get(path string, h Handler) { rt.Group().Get(path, h) }

// Post registers a POST route with no group middleware.
func (rt *Router) Post(path string, h Handler) { rt.Group().Post(path, h) }

// Handle registers a route with no group middleware.
func (rt *Router) Handle(method, path string, h Handler) { rt.Group().Handle(method, path, h) }

type outcomeKey struct{}

type outcome struct {
	err     error
	matched bool
}

func (rt *Router) dispatch(w http.ResponseWriter, r *http.Request) error {
	result := &outcome{}
	rt.mux.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), outcomeKey{}, result)))
	return result.err
}

// ServeHTTP runs router middleware around route dispatch and renders any
// error that escapes them.
func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt.build.Do(func() {
		h := rt.dispatch
		for i := len(rt.middleware) - 1; i >= 0; i-- {
			h = rt.middleware[i](h)
		}
		rt.handler = h
	})
	recorder := Record(w)
	if err := rt.handler(recorder, r); err != nil {
		WriteError(recorder, err)
	}
}

// Group registers routes that share middleware.
type Group struct {
	router     *Router
	middleware []Middleware
}

// Group returns a nested group whose middleware runs inside this group's.
func (g *Group) Group(middleware ...Middleware) *Group {
	return &Group{router: g.router, middleware: append(slices.Clone(g.middleware), middleware...)}
}

// Use returns a nested group with additional middleware.
func (g *Group) Use(middleware ...Middleware) *Group { return g.Group(middleware...) }

// Handle registers method and path, e.g. Handle("GET", "/v1/lists", h).
func (g *Group) Handle(method, path string, h Handler) {
	for i := len(g.middleware) - 1; i >= 0; i-- {
		h = g.middleware[i](h)
	}
	g.router.mux.Handle(method+" "+path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, _ := r.Context().Value(outcomeKey{}).(*outcome)
		err := h(w, r)
		if result != nil {
			result.matched = true
			result.err = err
		} else if err != nil {
			WriteError(w, err)
		}
	}))
}

// Get registers a GET route; ServeMux also routes HEAD to it.
func (g *Group) Get(path string, h Handler) { g.Handle(http.MethodGet, path, h) }

// Post registers a POST route.
func (g *Group) Post(path string, h Handler) { g.Handle(http.MethodPost, path, h) }

// Put registers a PUT route.
func (g *Group) Put(path string, h Handler) { g.Handle(http.MethodPut, path, h) }

// Patch registers a PATCH route.
func (g *Group) Patch(path string, h Handler) { g.Handle(http.MethodPatch, path, h) }

// Delete registers a DELETE route.
func (g *Group) Delete(path string, h Handler) { g.Handle(http.MethodDelete, path, h) }
