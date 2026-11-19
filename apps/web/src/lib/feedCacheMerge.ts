import type { InfiniteData } from "@tanstack/react-query";

import type { EntriesPage } from "@/hooks/useEntries";
import { dedupeEntryListItems } from "@/lib/rssFeedCore";
import type { ArticleListFilter } from "@/lib/entryArticleFilter";

/** Merge a fresh first page without dropping cached tail pages or their page params. */
export function mergeFeedFirstPageRefresh(
  existing: InfiniteData<EntriesPage> | undefined,
  freshPage: EntriesPage
): InfiniteData<EntriesPage> {
  if (!existing?.pages.length) {
    return { pages: [freshPage], pageParams: [undefined] };
  }

  const [firstPage, ...restPages] = existing.pages;
  const [firstParam, ...restParams] = existing.pageParams;
  const freshIds = new Set(freshPage.entries.map((entry) => entry.entryId));
  const carryOver = firstPage.entries.filter(
    (entry) => !freshIds.has(entry.entryId)
  );
  const mergedFirstPageEntries = dedupeEntryListItems([
    ...freshPage.entries,
    ...carryOver,
  ]);
  const seenEntryIds = new Set(
    mergedFirstPageEntries.map((entry) => entry.entryId)
  );
  const dedupedRestPages = restPages.map((page) => ({
    ...page,
    entries: page.entries.filter((entry) => {
      if (seenEntryIds.has(entry.entryId)) return false;
      seenEntryIds.add(entry.entryId);
      return true;
    }),
  }));

  return {
    pages: [
      {
        entries: mergedFirstPageEntries,
        cursor: freshPage.cursor ?? firstPage.cursor,
      },
      ...dedupedRestPages,
    ],
    pageParams: [firstParam, ...restParams],
  };
}

/** Seed an unread query from already-painted All rows while its server page loads. */
export function unreadFeedPlaceholder(
  allFeed: InfiniteData<EntriesPage, string | undefined> | undefined
): InfiniteData<EntriesPage, string | undefined> | undefined {
  if (!allFeed?.pages.length) return undefined;
  const entries = dedupeEntryListItems(
    allFeed.pages.flatMap((page) => page.entries)
  ).filter((entry) => !entry.isRead);
  if (entries.length === 0) return undefined;
  return {
    pages: [{ entries, cursor: undefined }],
    pageParams: [undefined],
  };
}

/**
 * A fresh unread page is authoritative for the span it covers: a cached row newer
 * than its oldest entry but missing from it was read elsewhere. Rows older than
 * that span are outside the snapshot, so they stay with their pages and the reader
 * keeps the tail it has scrolled through. A page without a cursor is the complete
 * unread set and replaces the cache.
 */
export function mergeUnreadFeedRefresh(
  existing: InfiniteData<EntriesPage> | undefined,
  freshPage: EntriesPage
): InfiniteData<EntriesPage> {
  if (!existing?.pages.length || !freshPage.cursor || !freshPage.entries.length) {
    return { pages: [freshPage], pageParams: [undefined] };
  }
  const oldest = Math.min(...freshPage.entries.map((entry) => Date.parse(entry.publishedAt)));
  if (!Number.isFinite(oldest)) return { pages: [freshPage], pageParams: [undefined] };
  const seen = new Set(freshPage.entries.map((entry) => entry.entryId));
  const outsideSnapshot = (entry: EntriesPage["entries"][number]) => {
    if (seen.has(entry.entryId)) return false;
    const publishedAt = Date.parse(entry.publishedAt);
    if (!(publishedAt < oldest)) return false;
    seen.add(entry.entryId);
    return true;
  };
  const [firstPage, ...restPages] = existing.pages;
  const [firstParam, ...restParams] = existing.pageParams;
  const carried = firstPage.entries.filter(outsideSnapshot);
  return {
    pages: [
      {
        entries: dedupeEntryListItems([...freshPage.entries, ...carried]),
        // Continue after the cached tail rather than re-reading rows already kept.
        cursor: restPages.length || carried.length ? firstPage.cursor : freshPage.cursor,
      },
      ...restPages.map((page) => ({ ...page, entries: page.entries.filter(outsideSnapshot) })),
    ],
    pageParams: [firstParam, ...restParams],
  };
}

/** Unread pages are authoritative snapshots; cached tail pages may now be read. */
export function mergeFeedRefreshForFilter(
  existing: InfiniteData<EntriesPage> | undefined,
  freshPage: EntriesPage,
  articleFilter: ArticleListFilter
): InfiniteData<EntriesPage> {
  if (articleFilter === "unread") {
    return { pages: [freshPage], pageParams: [undefined] };
  }
  return mergeFeedFirstPageRefresh(existing, freshPage);
}

/** A refresh of a feed the reader may be scrolled into keeps its unread tail. */
export function mergeVisibleFeedRefresh(
  existing: InfiniteData<EntriesPage> | undefined,
  freshPage: EntriesPage,
  articleFilter: ArticleListFilter
): InfiniteData<EntriesPage> {
  if (articleFilter === "unread") return mergeUnreadFeedRefresh(existing, freshPage);
  return mergeFeedFirstPageRefresh(existing, freshPage);
}
