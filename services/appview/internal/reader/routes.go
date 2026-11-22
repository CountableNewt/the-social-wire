package reader

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/isotime"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/publications"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/readstate"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

type EnrollFunc func(context.Context, *reqctx.Auth, thinstore.EnrollRequest) (int, error)

type service struct {
	db                *sqlitedb.DB
	publications      *publications.Service
	lists             *standardLists
	enroll            EnrollFunc
	bootstrapHooks    []func([]thinstore.PublicationScope)
	verifyEntry       func(context.Context, string) (bool, error)
	refreshScope      func(context.Context, *reqctx.Auth, thinstore.PublicationScope) (int, error)
	generationFetcher *readstate.LiveRecordFetcher
	confirmationSlots chan struct{}
	verifiedChunks    *readstate.VerifiedRecordCache
	recoveryClaim     *recoveryClaim
	durableRecovery   bool
	reconciles        reconcileState
	recoverySlots     chan struct{}
}

type Options struct {
	// DurableRecovery queues public PDS recovery instead of detaching request work.
	DurableRecovery bool
	Bootstrap       func([]thinstore.PublicationScope)
	VerifyEntry     func(context.Context, string) (bool, error)
	RefreshScope    func(context.Context, *reqctx.Auth, thinstore.PublicationScope) (int, error)
}

// Register serves reader routes and returns the read-state readiness gate for
// other routes that serve the viewer's projection, such as the sidebar.
func Register(group *httpx.Group, db *sqlitedb.DB, publications *publications.Service, enroll EnrollFunc, options Options) httpx.Middleware {
	s := &service{db: db, publications: publications, enroll: enroll, verifyEntry: options.VerifyEntry, refreshScope: options.RefreshScope,
		durableRecovery: options.DurableRecovery, confirmationSlots: make(chan struct{}, 4), recoverySlots: make(chan struct{}, 2), verifiedChunks: readstate.NewDefaultVerifiedRecordCache()}
	group = group.Use(s.requireReadState)
	if options.Bootstrap != nil {
		s.bootstrapHooks = []func([]thinstore.PublicationScope){options.Bootstrap}
	}
	if publications != nil && publications.Repository() != nil {
		s.lists = newStandardLists(publications)
		registerLists(group, s.lists)
		repo := publications.Repository()
		s.generationFetcher = readstate.NewLiveRecordFetcher(readstate.NewHTTPRecordGetter(repo.HTTPClient(), strictPDSResolver{repo.Resolver()}))
	}
	for _, route := range []struct {
		method, path, xrpc string
		handler            httpx.Handler
	}{
		{"GET", "/v1/appview/feed", "getFeed", s.feed},
		{"GET", "/v1/appview/entries", "listEntries", s.entries},
		{"GET", "/v1/appview/entry", "getEntry", s.entry},
		{"GET", "/v1/appview/unread-counts", "getUnreadCounts", s.unreadCounts},
		{"POST", "/v1/appview/mark-all-read", "markAllRead", s.markAllRead},
		{"POST", "/v1/appview/enroll", "enrollSources", s.enrollSources},
		{"POST", "/v1/appview/read-marks", "putReadMark", s.putReadMark},
		{"DELETE", "/v1/appview/read-marks", "deleteReadMark", s.deleteReadMark},
		{"DELETE", "/v1/appview/privacy/purge", "purgeViewerData", s.purge},
	} {
		group.Handle(route.method, route.path, authenticated(route.handler))
		method := route.method
		if method == http.MethodDelete {
			method = http.MethodPost
		}
		group.Handle(method, "/xrpc/app.thesocialwire.appview."+route.xrpc, authenticated(route.handler))
	}
	group.Get("/v1/appview/bootstrap-stream", authenticated(s.bootstrap))
	group.Post("/xrpc/app.thesocialwire.appview.refreshFeed", authenticated(s.refreshFeed))
	group.Get("/xrpc/app.thesocialwire.appview.getReadAgeOptions", authenticated(s.readAgeOptions))
	group.Post("/xrpc/app.thesocialwire.appview.markReadBefore", authenticated(s.markReadBefore))
	group.Get("/xrpc/app.thesocialwire.appview.getReadStateStatus", authenticated(s.readStateStatus))
	group.Post("/xrpc/app.thesocialwire.appview.exportReadState", authenticated(s.exportReadState))
	group.Post("/xrpc/app.thesocialwire.appview.prepareReadState", authenticated(s.prepareReadState))
	group.Post("/xrpc/app.thesocialwire.appview.confirmReadState", authenticated(s.confirmReadState))
	return s.requireReadState
}

func authenticated(next httpx.Handler) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		if reqctx.AuthFrom(r.Context()) == nil {
			switch r.URL.Path {
			case "/v1/appview/feed", "/v1/appview/entries", "/xrpc/app.thesocialwire.appview.getFeed", "/xrpc/app.thesocialwire.appview.listEntries":
				return feedError(w, r, http.StatusUnauthorized, "unauthorized", "Authentication is required.", false)
			}
			return httpx.ErrUnauthorized
		}
		return next(w, r)
	}
}

func feedError(w http.ResponseWriter, r *http.Request, status int, code, message string, retryable bool) error {
	id := reqctx.RequestID(r.Context())
	if id != "" {
		w.Header().Set("X-Request-ID", id)
	}
	return httpx.JSON(w, status, map[string]any{"error": code, "message": message, "requestId": id, "retryable": retryable})
}

func invalidFeed(w http.ResponseWriter, r *http.Request, message string) error {
	return feedError(w, r, http.StatusBadRequest, "invalid_request", message, false)
}

type pageArgs struct {
	filter     thinstore.EntryListFilter
	cursor     *string
	limit      int
	maxEntries int
}

func parsePage(query url.Values, aggregate bool) (pageArgs, string) {
	args := pageArgs{filter: thinstore.FilterAll, limit: 50}
	if query.Has("filter") {
		args.filter = thinstore.EntryListFilter(query.Get("filter"))
	}
	switch args.filter {
	case thinstore.FilterAll, thinstore.FilterUnread, thinstore.FilterRead:
	default:
		return args, "Invalid `filter`"
	}
	if query.Has("cursor") {
		value := query.Get("cursor")
		if _, _, ok := thinstore.DecodeCursor(value); !ok {
			return args, "Invalid `cursor`"
		}
		args.cursor = &value
	}
	if query.Has("limit") {
		value, err := strconv.Atoi(query.Get("limit"))
		if err != nil || value < 1 || value > 100 {
			return args, "Invalid `limit`"
		}
		args.limit = value
	}
	if aggregate && query.Has("maxEntries") {
		value, err := strconv.Atoi(query.Get("maxEntries"))
		if err != nil || value < 1 || value > 500 {
			return args, "Invalid `maxEntries`"
		}
		args.maxEntries = value
		args.cursor = nil
	}
	return args, ""
}

func (s *service) feed(w http.ResponseWriter, r *http.Request) error {
	kind, ok := thinstore.ParseFeedKind(httpx.Query(r, "kind"))
	if !ok {
		return invalidFeed(w, r, "Query requires a valid `kind`")
	}
	selector := thinstore.NewFeedSelector(kind, httpx.Query(r, "id"))
	if kind.RequiresID() && selector.ID == "" {
		return invalidFeed(w, r, "Query requires `id` for this feed kind")
	}
	if kind == thinstore.FeedList && s.lists != nil {
		return s.listFeed(w, r, selector.ID)
	}
	args, validation := parsePage(r.URL.Query(), false)
	if validation != "" {
		return invalidFeed(w, r, validation)
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	viewer := reqctx.ViewerDID(ctx)
	var page thinstore.EntryListResponse
	var updated string
	var projections bool
	err := s.db.Read(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT updated_at FROM appview_viewer_feeds
			WHERE viewer_did=? AND feed_kind=? AND feed_id=?`, viewer, selector.Kind, selector.ID).Scan(&updated)
		if errors.Is(err, sql.ErrNoRows) {
			err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM appview_viewer_feeds WHERE viewer_did=?)", viewer).Scan(&projections)
			if err != nil {
				return err
			}
			return sql.ErrNoRows
		}
		if err != nil {
			return err
		}
		scopeRows, err := scopes(ctx, tx, viewer, string(selector.Kind), selector.ID)
		if err != nil {
			return err
		}
		page, err = listEntries(ctx, tx, viewer, scopeRows, args.filter, args.cursor, args.limit, true)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		if projections && kind != thinstore.FeedList {
			return feedError(w, r, http.StatusNotFound, "feed_unavailable", "Feed is not available to this viewer.", false)
		}
		return feedError(w, r, http.StatusServiceUnavailable, "feed_projection_warming", "The viewer feed projection is warming.", true)
	}
	if err != nil {
		return feedFailure(w, r, err)
	}
	if value, err := time.Parse(time.RFC3339Nano, updated); err == nil {
		updated = value.Format(time.RFC3339)
	}
	w.Header().Set("X-AppView-Feed-Source", "materialized_projection")
	w.Header().Set("X-AppView-Membership-Updated-At", updated)
	ms := float64(time.Since(start)) / float64(time.Millisecond)
	w.Header().Set("Server-Timing", fmt.Sprintf("db;dur=%.1f, appview_feed;dur=%.1f", ms, ms))
	return httpx.OK(w, page)
}

func feedFailure(w http.ResponseWriter, r *http.Request, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return feedError(w, r, http.StatusGatewayTimeout, "feed_deadline_exceeded", "The feed request exceeded its deadline.", true)
	}
	var typed *httpx.Error
	if errors.As(err, &typed) {
		return typed
	}
	return err
}

func requestScope(r *http.Request, viewer string) thinstore.PublicationScope {
	author := httpx.Query(r, "authorDid")
	if author == "" {
		author = viewer
	}
	var publication *string
	id := author
	if value := httpx.Query(r, "publicationAtUri"); value != "" {
		id, publication = value, &value
	}
	return thinstore.NewPublicationScope(viewer, id, author, publication, httpx.QueryAll(r, "publicationScopeAtUris", true),
		httpx.QueryAll(r, "publicationSiteUrls", true), nil, time.Time{})
}

func (s *service) entries(w http.ResponseWriter, r *http.Request) error {
	if !r.URL.Query().Has("authorDid") {
		return invalidFeed(w, r, "Query requires `authorDid`")
	}
	args, validation := parsePage(r.URL.Query(), true)
	if validation != "" {
		return invalidFeed(w, r, validation)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	viewer := reqctx.ViewerDID(ctx)
	limit := args.limit
	if args.maxEntries > 0 {
		limit = args.maxEntries
	}
	var page thinstore.EntryListResponse
	err := s.db.Read(ctx, func(tx *sql.Tx) error {
		var err error
		page, err = listEntries(ctx, tx, viewer, []thinstore.PublicationScope{requestScope(r, viewer)}, args.filter, args.cursor, limit, false)
		return err
	})
	if err != nil {
		return feedFailure(w, r, err)
	}
	return httpx.OK(w, page)
}

type entryResponse struct {
	EntryID       string       `json:"entryId"`
	Title         string       `json:"title"`
	Summary       *string      `json:"summary,omitempty"`
	PublishedAt   isotime.Time `json:"publishedAt"`
	ThumbnailURL  *string      `json:"thumbnailUrl,omitempty"`
	IsRead        bool         `json:"isRead"`
	ContentHTML   *string      `json:"contentHtml,omitempty"`
	OriginalURL   *string      `json:"originalUrl,omitempty"`
	ContentLabels []string     `json:"contentLabels,omitempty"`
}

func (s *service) entry(w http.ResponseWriter, r *http.Request) error {
	if !r.URL.Query().Has("entryId") {
		return httpx.Errorf(http.StatusBadRequest, "Query requires `entryId`")
	}
	uri := httpx.Query(r, "entryId")
	ctx := r.Context()
	viewer := reqctx.ViewerDID(ctx)
	if s.verifyEntry != nil {
		present, err := s.verifyEntry(ctx, uri)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil && !present {
			return httpx.Errorf(http.StatusNotFound, "Entry not found in AppView index")
		}
	}
	var result entryResponse
	err := s.db.Read(ctx, func(tx *sql.Tx) error {
		migrated, err := readAuthority(ctx, tx, viewer)
		if err != nil {
			return err
		}
		var created, render, author string
		var site *string
		var read, override bool
		err = tx.QueryRowContext(ctx, `SELECT ci.created_at, ci.render_json, ci.author_did, ci.publication_site,
			rm.subject_uri IS NOT NULL, uo.subject_uri IS NOT NULL FROM content_items ci
			LEFT JOIN read_marks rm ON rm.viewer_did=? AND rm.subject_uri=ci.uri
			LEFT JOIN appview_unread_overrides uo ON uo.viewer_did=? AND uo.subject_uri=ci.uri
			WHERE ci.uri=?`, viewer, viewer, uri).Scan(&created, &render, &author, &site, &read, &override)
		if err != nil {
			return err
		}
		position, err := time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return err
		}
		if !migrated && !read && !override {
			read, err = floorReadState(ctx, tx, viewer, uri, author, site, position)
			if err != nil {
				return err
			}
		}
		item, fields, ok := makeEntry(uri, render, position, read)
		if !ok {
			return sql.ErrNoRows
		}
		content := fields.ContentHTML
		if content == nil {
			content = fields.Summary
		}
		result = entryResponse{
			item.EntryID, item.Title, item.Summary, isotime.Time{Time: item.PublishedAt}, item.ThumbnailURL, item.IsRead,
			content, item.OriginalURL, item.ContentLabels,
		}
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.Errorf(http.StatusNotFound, "Entry not found in AppView index")
	}
	if err != nil {
		return err
	}
	return httpx.OK(w, result)
}

func floorReadState(ctx context.Context, tx *sql.Tx, viewer, uri, author string, site *string, position time.Time) (bool, error) {
	all, err := scopes(ctx, tx, viewer, "", "")
	if err != nil {
		return false, err
	}
	for _, scope := range all {
		if !thinstore.ContentMatchesScope(author, site, scope) {
			continue
		}
		var raw string
		var entryID *string
		err := tx.QueryRowContext(ctx, "SELECT read_floor_at,read_floor_uri FROM appview_publication_read_floors WHERE viewer_did=? AND publication_id=?",
			viewer, scope.PublicationID).Scan(&raw, &entryID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, err
		}
		at, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return false, err
		}
		if (thinstore.ReadWatermarkBoundary{CreatedAt: at, EntryID: entryID}).Contains(position, uri) {
			return true, nil
		}
	}
	return false, nil
}

func (s *service) unreadCounts(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	viewer := reqctx.ViewerDID(ctx)
	ids := httpx.QueryAll(r, "publicationIds", true)
	result := map[string]int{}
	err := s.db.Read(ctx, func(tx *sql.Tx) error {
		var requested []thinstore.PublicationScope
		if len(ids) == 0 {
			requested = []thinstore.PublicationScope{requestScope(r, viewer)}
		} else {
			all, err := scopes(ctx, tx, viewer, "", "")
			if err != nil {
				return err
			}
			byID := make(map[string]thinstore.PublicationScope, len(all))
			for _, scope := range all {
				byID[scope.PublicationID] = scope
			}
			for _, id := range ids {
				scope, ok := byID[id]
				if !ok {
					decoded, _ := url.PathUnescape(id)
					scope, ok = byID[decoded]
				}
				if ok {
					requested = append(requested, scope)
				}
			}
		}
		var err error
		result, err = counts(ctx, tx, viewer, requested)
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"counts": result})
}

func (s *service) putReadMark(w http.ResponseWriter, r *http.Request) error {
	var input thinstore.ReadMarkRequest
	if err := httpx.DecodeJSON(r, &input, 0); err != nil {
		return err
	}
	actedAt := time.Now()
	if input.ReadAt != nil {
		actedAt = *input.ReadAt
	}
	if err := changeReadMark(r.Context(), s.db, reqctx.ViewerDID(r.Context()), input.SubjectURI, actedAt, true); err != nil {
		return err
	}
	return emptyMutation(w, r)
}

func (s *service) deleteReadMark(w http.ResponseWriter, r *http.Request) error {
	var input thinstore.ReadMarkDeleteRequest
	if err := httpx.DecodeJSON(r, &input, 0); err != nil {
		return err
	}
	if err := changeReadMark(r.Context(), s.db, reqctx.ViewerDID(r.Context()), input.SubjectURI, time.Now(), false); err != nil {
		return err
	}
	return emptyMutation(w, r)
}

func emptyMutation(w http.ResponseWriter, r *http.Request) error {
	if strings.HasPrefix(r.URL.Path, "/xrpc/") {
		return httpx.OK(w, struct{}{})
	}
	w.WriteHeader(http.StatusOK)
	return nil
}

type bulkScope struct {
	Kind          string `json:"kind"`
	PublicationID string `json:"publicationId"`
	FolderRKey    string `json:"folderRkey"`
}

func (s *service) markAllRead(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Scope *bulkScope `json:"scope"`
	}
	if err := httpx.DecodeJSON(r, &body, 0); err != nil {
		return err
	}
	if body.Scope == nil || body.Scope.Kind == "" {
		return httpx.ErrBadRequest
	}
	ctx := r.Context()
	auth := reqctx.AuthFrom(ctx)
	requested, err := s.bulkScopes(ctx, auth, *body.Scope)
	if err != nil {
		return err
	}
	result, err := markAll(ctx, s.db, auth.DID, requested)
	if err != nil {
		return err
	}
	return httpx.OK(w, result)
}

func publicationScopes(viewer string, rows []publications.Row) []thinstore.PublicationScope {
	scopes := make([]thinstore.PublicationScope, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		if !seen[row.PublicationID] {
			scopes = append(scopes, thinstore.NewPublicationScope(viewer, row.PublicationID, row.Scope.AuthorDID,
				row.Scope.PublicationAtURI, row.Scope.PublicationScopeAtURIs, row.Scope.PublicationSiteURLs, nil, time.Time{}))
			seen[row.PublicationID] = true
		}
	}
	return scopes
}

func (s *service) purge(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	viewer := reqctx.ViewerDID(ctx)
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		if err := requireLegacyWrite(ctx, tx, viewer); err != nil {
			return err
		}
		for _, table := range []string{"read_marks", "appview_unread_overrides", "appview_publication_read_floors",
			"appview_unread_counters", "sidebar_projection_cache"} {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE viewer_did = ?", viewer); err != nil {
				return err
			}
		}
		return invalidateCaches(ctx, tx, viewer)
	})
	if err != nil {
		return err
	}
	return emptyMutation(w, r)
}

func (s *service) enrollSources(w http.ResponseWriter, r *http.Request) error {
	var body thinstore.EnrollRequest
	if err := httpx.DecodeJSON(r, &body, 0); err != nil {
		return err
	}
	if s.enroll == nil {
		return httpx.Status(http.StatusServiceUnavailable)
	}
	indexed, err := s.enroll(r.Context(), reqctx.AuthFrom(r.Context()), body)
	var partial *thinstore.EnrollmentPartialFailure
	if errors.As(err, &partial) {
		return enrollmentPartialResponse(w, partial)
	}
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]int{"indexed": indexed})
}
