"use client";

import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from "@tanstack/react-query";
import { listEntries, getEntry, repoAndPublicationFilterFromPubId } from "@/lib/atprotoClient";
import type { EntryListItem, EntryDetail } from "@/lib/atprotoClient";
import { useAuth } from "./useAuth";

export type { EntryListItem, EntryDetail };

export const ENTRIES_QUERY_KEY = (authorDid: string) =>
  ["entries", authorDid] as const;
export const ENTRY_DETAIL_QUERY_KEY = (entryId: string) =>
  ["entry", entryId] as const;

type EntriesPage = { entries: EntryListItem[]; cursor?: string };

/**
 * Returns a paginated list of entries for a publication sidebar selection.
 *
 * `publicationKey` is either an **author DID** (legacy discovery row) or a **publication record
 * AT-URI** (distinct publication on an account).
 */
export function useEntries(publicationKey: string | null) {
  const { session, getOAuthSession } = useAuth();
  const queryClient = useQueryClient();

  return useInfiniteQuery({
    queryKey: ENTRIES_QUERY_KEY(publicationKey ?? ""),
    queryFn: async ({ pageParam, signal }) => {
      if (!publicationKey) return { entries: [], cursor: undefined };
      const oauth = getOAuthSession() ?? undefined;
      const key = ENTRIES_QUERY_KEY(publicationKey);
      const { repoDid, publicationAtUri } =
        repoAndPublicationFilterFromPubId(publicationKey);
      const isFirstInfinitePage = pageParam === undefined;
      return listEntries(
        repoDid,
        pageParam as string | undefined,
        50,
        oauth,
        {
          signal,
          publicationAtUri,
          onProgress: isFirstInfinitePage
            ? ({ entries, cursor }) => {
                queryClient.setQueryData<InfiniteData<EntriesPage> | undefined>(
                  key,
                  (old) => {
                    const page: EntriesPage = { entries, cursor };
                    if (!old?.pages.length) {
                      return {
                        pages: [page],
                        pageParams: [undefined],
                      };
                    }
                    const nextPages = [...old.pages];
                    nextPages[0] = page;
                    return { ...old, pages: nextPages };
                  }
                );
              }
            : undefined,
        }
      );
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.cursor,
    enabled: !!publicationKey && !!session,
    staleTime: 2 * 60_000,
    gcTime: 1000 * 60 * 60 * 24,
  });
}

/**
 * Returns the full content for a single entry by its AT-URI.
 */
export function useEntry(entryId: string | null) {
  const { session, getOAuthSession } = useAuth();

  return useQuery({
    queryKey: ENTRY_DETAIL_QUERY_KEY(entryId ?? ""),
    queryFn: async () => {
      if (!entryId) return null;
      return getEntry(entryId, getOAuthSession() ?? undefined);
    },
    enabled: !!entryId && !!session,
    staleTime: 5 * 60_000,
  });
}
