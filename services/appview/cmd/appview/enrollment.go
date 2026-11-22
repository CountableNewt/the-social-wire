package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/ingest"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reader"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

func enrollment(db *sqlitedb.DB, ingestion *ingest.Service, discovery *ingest.Microcosm) reader.EnrollFunc {
	if discovery == nil {
		return ingestion.Enroll
	}
	return func(ctx context.Context, auth *reqctx.Auth, input thinstore.EnrollRequest) (int, error) {
		if auth != nil && len(input.AuthorDIDs) > 0 {
			if err := queueKnownPublications(ctx, db, discovery, auth.DID, input.AuthorDIDs); err != nil {
				return 0, err
			}
		}
		return ingestion.Enroll(ctx, auth, input)
	}
}

func queueKnownPublications(ctx context.Context, db *sqlitedb.DB, discovery *ingest.Microcosm, viewer string, authors []string) error {
	if viewer == "" {
		return nil
	}
	unique := make([]string, 0, min(8, len(authors)))
	seen := map[string]bool{}
	for _, author := range authors {
		author = strings.TrimSpace(author)
		if author == "" || seen[author] {
			continue
		}
		unique = append(unique, author)
		seen[author] = true
		if len(unique) == cap(unique) {
			break
		}
	}
	encoded, err := json.Marshal(unique)
	if err != nil {
		return err
	}
	rows, err := db.Reader().QueryContext(ctx, `
		SELECT author_did, scope_keys FROM appview_publication_scopes
		WHERE viewer_did=? AND author_did IN (SELECT value FROM json_each(?))
		ORDER BY publication_id LIMIT 8
	`, viewer, string(encoded))
	if err != nil {
		return err
	}
	scopes := []ingest.DiscoveryScope{}
	for rows.Next() {
		var scope ingest.DiscoveryScope
		var keys string
		if err := rows.Scan(&scope.Author, &keys); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal([]byte(keys), &scope.Keys); err != nil {
			rows.Close()
			return err
		}
		scopes = append(scopes, scope)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, scope := range scopes {
		if err := discovery.Schedule(ctx, scope); err != nil {
			return err
		}
	}
	return nil
}

func bootstrapDiscovery(lifecycle context.Context, provider *ingest.Microcosm, logger *slog.Logger) func([]thinstore.PublicationScope) {
	return func(scopes []thinstore.PublicationScope) {
		if provider == nil {
			return
		}
		ctx, cancel := context.WithTimeout(lifecycle, 2*time.Second)
		defer cancel()
		for _, scope := range scopes[:min(len(scopes), 8)] {
			if err := provider.Schedule(ctx, ingest.DiscoveryScope{Author: scope.AuthorDID, Keys: scope.ScopeKeys}); err != nil {
				logger.Debug("Publication discovery enqueue deferred")
				return
			}
		}
	}
}

func refreshScope(ingestion *ingest.Service, provider *ingest.Microcosm) func(context.Context, *reqctx.Auth, thinstore.PublicationScope) (int, error) {
	return func(ctx context.Context, auth *reqctx.Auth, scope thinstore.PublicationScope) (int, error) {
		if scope.AuthorDID == thinstore.RssAuthorDID {
			return ingestion.Enroll(ctx, auth, thinstore.EnrollRequest{FeedURLs: scope.PublicationSiteURLs})
		}
		if provider == nil {
			return ingestion.RefreshAuthors(ctx, []string{scope.AuthorDID})
		}
		return provider.Check(ctx, ingest.DiscoveryScope{Author: scope.AuthorDID, Keys: scope.ScopeKeys})
	}
}
