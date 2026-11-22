package analytics

import (
	"context"
	"errors"
	"math"
	"net/url"
	"sync"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
)

type publicationSummary struct {
	SampledAt         string           `json:"sampledAt"`
	Publications      []publicationRow `json:"publications"`
	InventoryComplete bool             `json:"inventoryComplete"`
}

type publicationRow struct {
	URI                            string `json:"uri"`
	Name                           string `json:"name"`
	URL                            string `json:"url"`
	Documents                      int    `json:"documents"`
	Subscribers                    *int   `json:"subscribers"`
	Recommendations                *int   `json:"recommendations"`
	RecommendationCoverageComplete bool   `json:"recommendationCoverageComplete"`
}

func (p Provider) publications(ctx context.Context, author string, at time.Time) (publicationSummary, error) {
	type inventory struct {
		records  []atproto.RepoRecord
		complete bool
		err      error
	}
	docs := make(chan inventory, 1)
	ctx, cancel := context.WithCancel(ctx)
	var outstanding sync.WaitGroup
	defer func() { cancel(); outstanding.Wait() }()
	outstanding.Add(1)
	go func() {
		defer outstanding.Done()
		records, complete, err := p.publicationInventory(ctx, author, articleCollection, 3)
		docs <- inventory{records, complete, err}
	}()
	records, complete, err := p.publicationInventory(ctx, author, publicationCollection, 1)
	summary := publicationSummary{SampledAt: timestamp(at), Publications: make([]publicationRow, 0), InventoryComplete: complete}
	if err != nil || len(records) == 0 {
		return summary, err
	}
	const publicationLimit = 20
	publications := records[:min(len(records), publicationLimit)]
	targets := make([]string, 0, len(publications))
	byPublication := make(map[string][]string, len(publications))
	for _, record := range publications {
		targets = append(targets, record.URI)
		byPublication[record.URI] = nil
	}
	subscribers := make(chan map[string]int, 1)
	outstanding.Add(1)
	go func() {
		defer outstanding.Done()
		subscribers <- p.counts(ctx, targets, "site.standard.graph.subscription", ".publication")
	}()
	documentInventory := <-docs
	const recommendationLimit = 40
	recommendationTargets := make([]string, 0, recommendationLimit)
	if documentInventory.err == nil {
		for _, document := range documentInventory.records {
			site := stringValue(document.Value, "site")
			if _, exists := byPublication[site]; !exists {
				continue
			}
			byPublication[site] = append(byPublication[site], document.URI)
			if len(recommendationTargets) < recommendationLimit {
				recommendationTargets = append(recommendationTargets, document.URI)
			}
		}
	}
	recommendations := p.counts(ctx, recommendationTargets, "site.standard.graph.recommend", ".document")
	subscriptions := <-subscribers
	documentsComplete := documentInventory.err == nil && documentInventory.complete
	for _, record := range publications {
		name, ok := record.Value["name"].(string)
		if !ok {
			name = "Untitled Publication"
		}
		documents := byPublication[record.URI]
		row := publicationRow{URI: record.URI, Name: name, URL: stringValue(record.Value, "url"), Documents: len(documents)}
		if count, ok := subscriptions[record.URI]; ok {
			row.Subscribers = &count
		}
		total, observed := 0, 0
		for _, uri := range documents {
			if count, ok := recommendations[uri]; ok {
				observed++
				total += min(math.MaxInt-total, count)
			}
		}
		row.RecommendationCoverageComplete = documentsComplete && observed == len(documents)
		if row.RecommendationCoverageComplete || observed > 0 {
			row.Recommendations = &total
		}
		summary.Publications = append(summary.Publications, row)
	}
	summary.InventoryComplete = complete && documentsComplete && len(records) <= publicationLimit
	return summary, ctx.Err()
}

func (p Provider) publicationInventory(ctx context.Context, author, collection string, pages int) ([]atproto.RepoRecord, bool, error) {
	var records []atproto.RepoRecord
	seen := map[string]bool{}
	cursors := map[string]bool{}
	cursor := ""
	for range pages {
		page, next, err := p.records(ctx, author, collection, cursor)
		if err != nil || len(page) > 100 {
			return nil, false, errors.New("publication inventory unavailable")
		}
		for _, record := range page {
			owner, kind, _, err := parseSubject(record.URI)
			if err != nil || owner != author || kind != collection || stringValue(record.Value, "$type") != collection {
				return nil, false, errors.New("invalid publication inventory")
			}
			if !seen[record.URI] {
				seen[record.URI] = true
				records = append(records, record)
			}
		}
		if next == "" {
			return records, true, nil
		}
		if cursors[next] {
			break
		}
		cursors[next] = true
		cursor = next
	}
	return records, false, nil
}

func (p Provider) counts(ctx context.Context, targets []string, collection, path string) map[string]int {
	counts := make(map[string]int, len(targets))
	var lock sync.Mutex
	for start := 0; start < len(targets) && ctx.Err() == nil; start += 8 {
		var group sync.WaitGroup
		for _, uri := range targets[start:min(start+8, len(targets))] {
			group.Go(func() {
				var result struct {
					Total *int `json:"total"`
				}
				err := p.publicJSON(ctx, "https://constellation.microcosm.blue/links/count/distinct-dids",
					url.Values{"target": {uri}, "collection": {collection}, "path": {path}}, 2*time.Second, 4096, &result)
				if err == nil && result.Total != nil && *result.Total >= 0 {
					lock.Lock()
					counts[uri] = *result.Total
					lock.Unlock()
				}
			})
		}
		group.Wait()
	}
	return counts
}
