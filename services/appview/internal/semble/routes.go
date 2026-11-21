// Package semble serves read-only Semble projections backed by public upstream data.
package semble

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

// Register serves projections on the authenticated route group.
func Register(group *httpx.Group, env config.Env, repo *atproto.RepoClient) {
	service := Service{Transport: NewHTTPTransport(env, repo.HTTPClient()), Reader: PublicReader{Repo: repo}}
	register(group, service)
}

func register(group *httpx.Group, service Service) {
	for _, kind := range []string{"collections", "collection", "connections"} {
		group.Get("/v1/semble/"+kind, func(w http.ResponseWriter, r *http.Request) error {
			return handle(w, r, service, kind)
		})
	}
}

func handle(w http.ResponseWriter, r *http.Request, service Service, kind string) error {
	w.Header().Set("Cache-Control", "no-store")
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	query := r.URL.Query()
	if kind == "collection" && query.Get("collectionUri") == "" {
		return failure(w, Invalid("Query requires `collectionUri`."))
	}
	if kind == "connections" && query.Get("url") == "" {
		return failure(w, Invalid("Query requires `url`."))
	}
	limit := 50
	if query.Has("limit") {
		var err error
		limit, err = strconv.Atoi(query.Get("limit"))
		if err != nil || limit < 1 || limit > 100 {
			return failure(w, Invalid("`limit` must be between 1 and 100."))
		}
	}
	var cursor *string
	if query.Has("cursor") {
		raw := query.Get("cursor")
		cursor = &raw
	}
	var output any
	var err error
	switch kind {
	case "collections":
		output, err = service.Collections(r.Context(), viewer, cursor, limit)
	case "collection":
		output, err = service.Collection(r.Context(), viewer, query.Get("collectionUri"), cursor, limit)
	case "connections":
		output, err = service.Connections(r.Context(), viewer, query.Get("url"), cursor, limit)
	}
	if err != nil {
		return failure(w, err)
	}
	return httpx.JSON(w, http.StatusOK, output)
}

func failure(w http.ResponseWriter, err error) error {
	var typed *Error
	if !errors.As(err, &typed) {
		typed = upstream("Semble public projection is unavailable.")
	}
	return httpx.JSON(w, typed.Status, typed)
}
