package appviewworkercore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/stygian-tech/the-social-wire/packages/go/thinappviewcore"
)

type EventProjectorRuntime struct {
	DB                 *sql.DB
	PDS                *thinappviewcore.PDSClient
	Cache              *thinappviewcore.ProjectionCache
	RSS                *thinappviewcore.RSSIngestion
	Counters           thinappviewcore.CounterStore
	Retention          time.Duration
	WorkerID           string
	ReconcileReadState func(context.Context, string, bool) (bool, error)
	RestoreRepository  func(context.Context, thinappviewcore.RecoveryContext) error
	Now                func() time.Time
}

func (p *EventProjectorRuntime) clock() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}
func (p *EventProjectorRuntime) invalidateContent(ctx context.Context, site string) {
	if p.Cache == nil {
		return
	}
	if site == "" {
		_ = p.Cache.InvalidateAll(ctx)
		return
	}
	keys := thinappviewcore.PublicationEquivalenceKeys(site)
	if key := thinappviewcore.NormalizePublicationSiteURL(site); key != "" {
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		_ = p.Cache.InvalidateAll(ctx)
		return
	}
	for _, key := range keys {
		_ = p.Cache.InvalidatePublication(ctx, key)
	}
}
func (p *EventProjectorRuntime) Apply(ctx context.Context, item thinappviewcore.InboxItem) error {
	event, err := thinappviewcore.ParseProjectionEvent(item.Payload, item.Sequence, item.EventKind, item.RepoDID)
	if err != nil {
		return err
	}
	switch event.Kind {
	case "commit":
		commit := event.Commit
		if item.Collection != nil && *item.Collection != commit.Collection || item.Operation != nil && *item.Operation != commit.Operation || item.RepoRev != nil && *item.RepoRev != commit.RepoRev || item.RecordKey != nil && *item.RecordKey != commit.RKey || item.RecordCID != nil && (commit.CID == nil || *item.RecordCID != *commit.CID) {
			return thinappviewcore.ErrMetadataMismatch
		}
		cid := ""
		if commit.CID != nil {
			cid = *commit.CID
		}
		return p.Commit(ctx, event.DID, commit.Collection, commit.RKey, cid, commit.Operation, commit.RepoRev, commit.RecordJSON, event.EventTime, "")
	case "identity":
		if p.PDS != nil {
			p.PDS.Invalidate(event.DID)
		}
		return nil
	case "account":
		if p.PDS != nil {
			p.PDS.Invalidate(event.DID)
		}
		if !event.Account.Active || event.Account.Status != "active" {
			if _, err := (thinappviewcore.ContentStore{DB: p.DB}).DeleteAuthor(ctx, event.DID); err != nil {
				return err
			}
			if p.Cache != nil {
				_ = p.Cache.InvalidateAll(ctx)
			}
		}
		return nil
	case "sync":
		if p.RestoreRepository == nil {
			return errors.New("repository reconciliation unavailable")
		}
		recovery := thinappviewcore.RecoveryContext{Environment: item.Environment, SourceGeneration: item.SourceGeneration, Sequence: item.Sequence, RepoDID: item.RepoDID, WorkerID: p.WorkerID, LeaseToken: item.LeaseToken}
		if err := p.RestoreRepository(ctx, recovery); err != nil {
			return err
		}
		if p.ReconcileReadState == nil {
			return errors.New("read-state reconciliation unavailable")
		}
		if _, err := p.ReconcileReadState(ctx, event.DID, false); err != nil {
			return err
		}
		return (thinappviewcore.InboxStore{DB: p.DB}).Reconciled(ctx, item, p.WorkerID, event.Sync.RepoRev, p.clock().Add(24*time.Hour), p.clock())
	default:
		return thinappviewcore.ErrInvalidEnvelope
	}
}
func (p *EventProjectorRuntime) Commit(ctx context.Context, did, collection, key, cid, operation, rev string, raw []byte, eventAt time.Time, pds string) error {
	if collection == "app.thesocialwire.readState" {
		if key != "self" {
			return nil
		}
		if p.ReconcileReadState == nil {
			return errors.New("read-state reconciliation unavailable")
		}
		_, err := p.ReconcileReadState(ctx, did, false)
		return err
	}
	record := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &record); err != nil {
			return err
		}
	}
	if collection == "app.thesocialwire.finance.selection" || collection == "app.thesocialwire.sports.selection" {
		topic := "finance"
		if strings.Contains(collection, ".sports.") {
			topic = "sports"
		}
		mutation := thinappviewcore.ParseSelectionMutation(topic, did, key, operation, record, eventAt, rev)
		if mutation == nil {
			return nil
		}
		return (thinappviewcore.SelectionStore{DB: p.DB}).Apply(ctx, *mutation)
	}
	if collection == thinappviewcore.RSSSubscriptionCollection {
		feed, ok := record["feedUrl"].(string)
		source, _ := record["sourceType"].(string)
		if ok && (source == "" || strings.EqualFold(strings.TrimSpace(source), "rss")) && operation != "delete" && p.RSS != nil {
			if _, err := p.RSS.Ingest(ctx, feed); err != nil {
				return err
			}
		}
		if p.Cache != nil {
			_ = p.Cache.InvalidateViewer(ctx, did)
		}
		return nil
	}
	if collection == "site.standard.graph.subscription" {
		if p.Cache != nil {
			_ = p.Cache.InvalidateViewer(ctx, did)
		}
		return nil
	}
	supported := false
	for _, allowed := range []string{"site.standard.document", "site.standard.entry", "com.standard.document", "com.standard.entry"} {
		supported = supported || collection == allowed
	}
	if !supported {
		return nil
	}
	uri := "at://" + did + "/" + collection + "/" + key
	site := thinappviewcore.PublicationSiteField(record)
	content := thinappviewcore.ContentStore{DB: p.DB}
	now := p.clock()
	if operation == "delete" {
		if err := content.Delete(ctx, uri); err != nil {
			return err
		}
		if err := p.Counters.Dirty(ctx, did, site, now); err != nil {
			return err
		}
		p.invalidateContent(ctx, site)
		return nil
	}
	if pds == "" && p.PDS != nil {
		resolved, err := p.PDS.Resolve(ctx, did)
		if err == nil {
			pds = resolved
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	render := thinappviewcore.ExtractRenderFields(record, did, pds, now)
	if render.ArticleURL == nil && site != "" && p.PDS != nil && strings.HasPrefix(site, "at://") {
		parts := strings.Split(strings.TrimPrefix(site, "at://"), "/")
		if len(parts) == 3 {
			_, _, publication, err := p.PDS.FetchRecord(ctx, parts[0], parts[1], parts[2], nil)
			if err == nil {
				var value map[string]any
				if json.Unmarshal(publication, &value) == nil {
					base := ""
					for _, key := range []string{"url", "siteUrl", "site", "homepage"} {
						if candidate, ok := value[key].(string); ok {
							base = thinappviewcore.NormalizePublicationSiteURL(candidate)
							if base != "" {
								break
							}
						}
					}
					if article := thinappviewcore.ArticleURL(record, base); article != "" {
						render.ArticleURL = &article
					}
				}
			}
		}
	}
	created, err := time.Parse(time.RFC3339Nano, render.PublishedAt)
	if err != nil {
		created = now
	}
	retention := p.Retention
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	var publication *string
	if site != "" {
		publication = &site
	}
	item := thinappviewcore.IndexedContentItem{URI: uri, CID: cid, AuthorDID: did, Collection: collection, CreatedAt: created, IndexedAt: now, ExpiresAt: now.Add(retention), PublicationSite: publication, Render: render}
	existed, err := content.Exists(ctx, uri)
	if err != nil {
		return err
	}
	if err := content.Upsert(ctx, item); err != nil {
		return err
	}
	if existed {
		err = p.Counters.Dirty(ctx, did, site, now)
	} else {
		err = p.Counters.Increment(ctx, item, now)
	}
	if err != nil {
		return err
	}
	p.invalidateContent(ctx, site)
	return nil
}
