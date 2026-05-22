import type { InfiniteData, QueryClient } from "@tanstack/react-query";

import { ENTRIES_QUERY_KEY, type EntriesPage } from "@/hooks/useEntries";
import {
  normalizeAtRepoParam,
  type EntryListItem,
} from "@/lib/atprotoClient";

/**
 * Flattens paginated entry list cache into a single list (may contain duplicates
 * across pages if the cache merged oddly).
 */
export function flattenCachedInfiniteEntries(
  data: InfiniteData<EntriesPage> | undefined
): EntryListItem[] {
  if (!data?.pages?.length) return [];
  const out: EntryListItem[] = [];
  for (const page of data.pages) {
    out.push(...page.entries);
  }
  return out;
}

/**
 * Counts distinct entries that are not yet read. Duplicates by `entryId` are ignored.
 */
export function countUnreadCachedEntries(
  entries: EntryListItem[],
  isEntryRead: (entryId: string) => boolean
): number {
  const seen = new Set<string>();
  let count = 0;
  for (const entry of entries) {
    if (seen.has(entry.entryId)) continue;
    seen.add(entry.entryId);
    if (!isEntryRead(entry.entryId)) count += 1;
  }
  return count;
}

/**
 * Sums per-publication unread totals for a list of publications (e.g. one folder).
 */
export function sumUnreadForPublications(
  publications: Array<{ publicationId: string }>,
  publicationUnreadCounts: Map<string, number>
): number {
  let sum = 0;
  for (const pub of publications) {
    sum += publicationUnreadCounts.get(pub.publicationId) ?? 0;
  }
  return sum;
}

/** Cached entry rows for a publication (any article-list filter variant). */
export function getCachedEntriesForPublication(
  queryClient: QueryClient,
  publicationId: string
): EntryListItem[] {
  const normalized = normalizeAtRepoParam(publicationId);
  const queries = queryClient.getQueriesData<InfiniteData<EntriesPage>>({
    queryKey: ENTRIES_QUERY_KEY(normalized),
  });
  const seen = new Set<string>();
  const out: EntryListItem[] = [];
  for (const [, data] of queries) {
    for (const entry of flattenCachedInfiniteEntries(data)) {
      if (seen.has(entry.entryId)) continue;
      seen.add(entry.entryId);
      out.push(entry);
    }
  }
  return out;
}

/**
 * Distinct entry AT-URIs present in the TanStack infinite-query cache for the given publications.
 */
export function distinctCachedEntryIdsForPublications(
  queryClient: QueryClient,
  publications: Array<{ publicationId: string }>
): string[] {
  const seen = new Set<string>();
  for (const pub of publications) {
    for (const entry of getCachedEntriesForPublication(
      queryClient,
      pub.publicationId
    )) {
      seen.add(entry.entryId);
    }
  }
  return [...seen];
}
