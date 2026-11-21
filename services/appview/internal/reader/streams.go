package reader

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/publications"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
	"golang.org/x/sync/errgroup"
)

func beginStream(w http.ResponseWriter) func(string, any) error {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	controller := http.NewResponseController(w)
	return func(kind string, payload any) error {
		if err := encoder.Encode(map[string]any{"kind": kind, kind: payload}); err != nil {
			return err
		}
		return controller.Flush()
	}
}

func (s *service) bootstrap(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	auth := reqctx.AuthFrom(ctx)
	sidebar, err := s.publications.Cached(ctx, auth.DID)
	if err != nil {
		return err
	}
	source := "projection_cache"
	if sidebar == nil {
		source = "live_projection"
		sidebar, err = s.publications.Load(ctx, auth, false)
		if err != nil {
			return err
		}
	}
	var lists <-chan standardListsResponse
	if s.lists != nil && httpx.Query(r, "includeLists") == "true" {
		ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		response := make(chan standardListsResponse, 1)
		lists = response
		go func() {
			defer close(response)
			if result, err := s.lists.list(ctx, auth.DID, false); err == nil {
				response <- result
			}
		}()
	}
	emit := beginStream(w)
	if err := emit("sidebarPriority", sidebar.Priority()); err != nil {
		return err
	}
	requested := publicationScopes(auth.DID, sidebar.AllRows)
	for _, warm := range s.bootstrapHooks {
		warm(requested)
	}
	var unread map[string]int
	err = s.db.Read(ctx, func(tx *sql.Tx) error {
		var err error
		unread, err = counts(ctx, tx, auth.DID, requested)
		return err
	})
	if err != nil {
		return emit("error", map[string]string{"message": "The sidebar unread projection could not be loaded."})
	}
	replace := make([]string, 0, len(requested))
	for _, scope := range requested {
		replace = append(replace, scope.PublicationID)
		if unread[scope.PublicationID] == 0 {
			delete(unread, scope.PublicationID)
		}
	}
	if err := emit("unreadCounts", map[string]any{"counts": unread, "replacePublicationIds": replace, "accuracy": "exact",
		"countedAt": time.Now().UTC().Format(time.RFC3339)}); err != nil {
		return err
	}
	if err := emit("sidebarFolders", map[string]any{"folderSections": sidebar.FolderSections, "allPublicationRows": sidebar.AllRows}); err != nil {
		return err
	}
	ordered := append(append(append([]publications.Row{}, sidebar.OwnRows...), sidebar.UnfolderedRows...), sidebar.FollowingRows...)
	for _, row := range ordered {
		id := row.PublicationID
		if unread[id] <= 0 {
			continue
		}
		if err := emit("selectedPublication", map[string]string{"publicationId": id}); err != nil {
			return err
		}
		var page thinstore.EntryListResponse
		err := s.db.Read(ctx, func(tx *sql.Tx) error {
			var err error
			page, err = listEntries(ctx, tx, auth.DID, publicationScopes(auth.DID, []publications.Row{row}), thinstore.FilterAll, nil, 50, false)
			return err
		})
		if err != nil {
			return emit("error", map[string]string{"message": "The initial article page could not be loaded."})
		}
		if err := emit("entriesPage", struct {
			thinstore.EntryListResponse
			PublicationID string `json:"publicationId"`
			Source        string `json:"source"`
		}{page, id, "live_projection"}); err != nil {
			return err
		}
		break
	}
	if lists != nil {
		if result, ok := <-lists; ok {
			if err := emit("lists", result); err != nil {
				return err
			}
		}
	}
	if err := emit("done", map[string]string{"refreshedAt": sidebar.RefreshedAt, "source": source}); err != nil {
		return err
	}
	return nil
}

func (s *service) refreshFeed(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	}
	if err := httpx.DecodeJSON(r, &body, 0); err != nil {
		return err
	}
	kind, ok := thinstore.ParseFeedKind(body.Kind)
	if !ok {
		return invalidFeed(w, r, "Input requires a valid `kind`")
	}
	selector := thinstore.NewFeedSelector(kind, body.ID)
	if kind.RequiresID() && selector.ID == "" {
		return invalidFeed(w, r, "Input requires `id` for this feed kind")
	}
	if s.enroll == nil {
		return httpx.Status(http.StatusServiceUnavailable)
	}
	ctx := r.Context()
	auth := reqctx.AuthFrom(ctx)
	requested, err := scopes(ctx, s.db.Reader(), auth.DID, string(kind), selector.ID)
	if err != nil {
		return err
	}
	emit := beginStream(w)
	if err := emit("started", map[string]int{"total": len(requested)}); err != nil {
		return err
	}
	type checked struct {
		id      string
		indexed int
		err     error
	}
	results := make(chan checked, len(requested))
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(8)
	go func() {

		for _, scope := range requested {
			group.Go(func() error {
				input := thinstore.EnrollRequest{AuthorDIDs: []string{scope.AuthorDID}}
				if scope.AuthorDID == atproto.RSSAuthorDID {
					input = thinstore.EnrollRequest{FeedURLs: scope.PublicationSiteURLs}
				}
				var indexed int
				var err error
				if s.refreshScope != nil {
					indexed, err = s.refreshScope(ctx, auth, scope)
				} else {
					indexed, err = s.enroll(ctx, auth, input)
				}
				results <- checked{scope.PublicationID, indexed, err}
				return nil
			})
		}
		_ = group.Wait()
		close(results)
	}()

	var indexed, unavailable, completed int
	for result := range results {
		status := "checked"
		if result.err != nil {
			status = "unavailable"
			unavailable++
		} else if result.indexed > 0 {
			status = "updated"
		}
		indexed += result.indexed
		completed++
		if err := emit("publication", map[string]any{"publicationId": result.id, "status": status,
			"indexed": result.indexed, "completed": completed, "total": len(requested)}); err != nil {
			return err
		}
	}
	return emit("done", map[string]int{"indexed": indexed, "unavailable": unavailable, "total": len(requested)})
}
