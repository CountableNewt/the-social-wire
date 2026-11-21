import type { OAuthSession } from "@atproto/oauth-client-browser";
import type { QueryClient } from "@tanstack/react-query";

import { normalizeAtRepoParam } from "@/lib/atprotoClient";
import {
  sidebarPublicationRows,
  type PublicationSidebarProjection,
  type SidebarPublicationRow,
} from "@/lib/publicationProjectionClient";
import { consumeFeedRefreshStream, type FeedRefreshSelector } from "@/lib/feedRefreshStreamClient";
import { PublicationSourceRefreshError } from "@/lib/publicationSourceRefreshError";

const RSS_AUTHOR_DID = "did:web:skyreader.rss";

/** Match the visible feed; a publication refresh must not enroll the full follow graph. */
export function readerRefreshRows(client: QueryClient, did: string, projection: PublicationSidebarProjection): SidebarPublicationRow[] {
  const all = sidebarPublicationRows(projection);
  const selected = new Map<string, SidebarPublicationRow>();
  const add = (rows: SidebarPublicationRow[]) => rows.forEach(row => selected.set(row.publicationId, row));
  for (const query of client.getQueryCache().findAll({ predicate: query => query.isActive() && query.queryKey[1] === did })) {
    const [kind, , scope, id] = query.queryKey;
    if (kind === "entries") add(all.filter(row => normalizeAtRepoParam(row.publicationId) === normalizeAtRepoParam(String(scope))));
    if (kind !== "aggregateEntries") continue;
    if (scope === "following") add(projection.followingTabPublications);
    if (scope === "subscribed") {
      add(projection.myPublications);
      add(projection.subscribedUnfoldered);
      for (const folder of projection.folderSections ?? []) add(folder.publications);
    }
    if (scope === "folder") for (const folder of projection.folderSections ?? []) {
      if (folder.folderRkey === id || folder.folderUri === id) add(folder.publications);
    }
  }
  return [...selected.values()];
}

/** Compare source work independently of row order and duplicate scope aliases. */
export function readerRefreshSourcesKey(rows: SidebarPublicationRow[]): { membership: string[]; sources: string[]; scopes: string[] } {
  const normalizeSite = (site: string) => {
    try { return new URL(site).href; } catch { return site; }
  };
  const normalizedSet = (values: string[]) => [...new Set(values)].sort();
  const sources = new Set<string>();
  for (const { appViewScope: scope } of rows) {
    if (scope.authorDid === RSS_AUTHOR_DID) {
      for (const feed of scope.publicationSiteUrls) {
        if (!feed.trim()) continue;
        sources.add(`feed:${normalizeSite(feed)}`);
      }
    } else if (scope.authorDid) sources.add(`author:${normalizeAtRepoParam(scope.authorDid)}`);
  }
  return {
    membership: [...new Set(rows.map(row => normalizeAtRepoParam(row.publicationId)))].sort(),
    sources: [...sources].sort(),
    // Enrollment can stay author-scoped while the server changes which of that
    // author's publications/URLs belong to a row. Such changes need seed cleanup.
    scopes: normalizedSet(rows.map(row => JSON.stringify([
      normalizeAtRepoParam(row.publicationId),
      normalizeAtRepoParam(row.appViewScope.authorDid ?? ""),
      row.appViewScope.publicationAtUri ? normalizeAtRepoParam(row.appViewScope.publicationAtUri) : null,
      normalizedSet(row.appViewScope.publicationScopeAtUris.map(normalizeAtRepoParam)),
      normalizedSet(row.appViewScope.publicationSiteUrls.map(normalizeSite)),
    ]))),
  };
}

/** The feeds the reader has mounted, as `refreshFeed` selectors. */
export function readerRefreshSelectors(client: QueryClient, did: string): FeedRefreshSelector[] {
  const selectors = new Map<string, FeedRefreshSelector>();
  const add = (selector: FeedRefreshSelector) => selectors.set(`${selector.kind}:${selector.id ?? ""}`, selector);
  for (const query of client.getQueryCache().findAll({ predicate: query => query.isActive() && query.queryKey[1] === did })) {
    const [kind, , scope, id] = query.queryKey;
    if (kind === "entries" && typeof scope === "string" && scope) add({ kind: "publication", id: scope });
    if (kind !== "aggregateEntries") continue;
    if (scope === "subscribed" || scope === "following") add({ kind: scope });
    if ((scope === "folder" || scope === "list") && typeof id === "string" && id) add({ kind: scope, id });
  }
  return [...selectors.values()];
}

/** How often streamed progress may repaint mounted feeds while articles land. */
export const SOURCE_REFRESH_PAINT_MS = 1000;

/** Longest one feed's refresh stream may stay open. */
export const FEED_REFRESH_STREAM_DEADLINE_MS = 300_000;

/** Stream an explicit refresh of each mounted feed, repainting as articles land. */
export async function refreshPublicationSources(args: {
  oauthSession: OAuthSession;
  rows: SidebarPublicationRow[];
  selectors: FeedRefreshSelector[];
  signal: AbortSignal;
  onProgress: (completed: number, total: number) => void;
  onBatch: () => Promise<void>;
}): Promise<void> {
  args.signal.throwIfAborted();
  let completed = 0;
  let total = 0;
  let dirty = false;
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let paint: Promise<void> | undefined;
  let paintFailure: unknown;
  const schedulePaint = () => {
    if (stopped || args.signal.aborted || paintFailure) return;
    timer = setTimeout(() => {
      if (stopped || args.signal.aborted) return;
      if (!dirty) return schedulePaint();
      dirty = false;
      // Schedule only after this paint settles: slow reads never accumulate work
      // or hold up the stream, and direct feed cache writes cannot race each other.
      paint = Promise.resolve().then(() => {
        args.signal.throwIfAborted();
        return args.onBatch();
      }).catch(error => {
        paintFailure = error;
      }).finally(schedulePaint);
    }, SOURCE_REFRESH_PAINT_MS);
  };
  const unavailable = new Set<string>();
  const streamFailures: unknown[] = [];
  if (args.selectors.length) {
    args.onProgress(0, 0);
    schedulePaint();
    try {
      for (const selector of args.selectors) {
        args.signal.throwIfAborted();
        let streamTotal = 0;
        let streamCompleted = 0;
        try {
          // Every source has its own server deadline; this bounds a stalled connection.
          const signal = AbortSignal.any([args.signal, AbortSignal.timeout(FEED_REFRESH_STREAM_DEADLINE_MS)]);
          await consumeFeedRefreshStream({
            oauthSession: args.oauthSession, selector, signal,
            onEvent: event => {
              if (event.kind === "started") {
                streamTotal = event.total;
                total += event.total;
              } else if (event.kind === "publication") {
                streamCompleted += 1;
                completed += 1;
                if (event.indexed > 0) dirty = true;
                if (event.status === "unavailable") unavailable.add(event.publicationId);
              } else return;
              args.onProgress(completed, total);
            },
          });
        } catch (error) {
          args.signal.throwIfAborted();
          // Count an interrupted stream's remaining sources as checked for progress only.
          completed += Math.max(0, streamTotal - streamCompleted);
          streamFailures.push(error);
        }
      }
    } finally {
      stopped = true;
      clearTimeout(timer);
      // Even a failed/cancelled stream must settle its started paint.
      await paint;
    }
  }
  args.signal.throwIfAborted();
  // The server reconciles counters before `done`. Flush once, including partial
  // results on failure; the caller must not issue another unconditional feed refresh.
  // This authoritative flush supersedes any intermediate paint failure if it succeeds.
  if (!streamFailures.length) args.onProgress(total, total);
  await args.onBatch();
  args.signal.throwIfAborted();
  if (streamFailures.length) {
    throw new Error("Some publications could not be checked. Articles received so far are available. Try Refresh again.", { cause: streamFailures[0] });
  }
  if (!unavailable.size) return;
  const feedHosts = new Map<string, string[]>();
  for (const row of args.rows) {
    if (row.appViewScope.authorDid !== RSS_AUTHOR_DID) continue;
    feedHosts.set(row.publicationId, row.appViewScope.publicationSiteUrls.flatMap(feed => {
      try { return [new URL(feed).hostname]; } catch { return []; }
    }));
  }
  const ids = [...unavailable];
  if (ids.every(id => feedHosts.get(id)?.length)) {
    throw new PublicationSourceRefreshError([...new Set(ids.flatMap(id => feedHosts.get(id)!))].sort(), ids.sort());
  }
  throw new Error("Some publications could not be checked. Articles received so far are available. Try Refresh again.");
}
