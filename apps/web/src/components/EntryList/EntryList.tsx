"use client";

import { effectiveEntryReadState } from "@/lib/pendingReadStateOverlay";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
} from "react";
import { Skeleton } from "@/components/ui/skeleton";
import { RefreshCw } from "lucide-react";
import {
  useAggregateFeedEntries,
  useEntries,
} from "@/hooks/useEntries";
import { useProactiveFeedRefresh } from "@/hooks/useProactiveFeedRefresh";
import { useScrollActivity } from "@/hooks/useScrollActivity";
import {
  sortEntryListItemsNewestFirst,
  type EntryListItem,
} from "@/lib/atprotoClient";
import { dedupeEntryListItems } from "@/lib/rssFeedCore";
import {
  filterEntriesForArticleFilter,
  shouldShowNoUnreadEntriesState,
  type ArticleListFilter,
} from "@/lib/entryArticleFilter";
import { EntryListVirtualPane } from "./EntryListVirtualPane";
import type { AggregateAppViewFeed } from "@/lib/thinAppViewClient";
import {
  mergeAggregateEntryPagesCached,
} from "@/lib/aggregateEntryPages";
import {
  markSubscribedPaginationTriggered,
  markSubscribedRowsRendered,
} from "@/lib/subscribedFeedPerf";
import { useSidebarProjection } from "@/contexts/PublicationSidebarContext";
import { CursorSingleFlight } from "@/lib/cursorSingleFlight";

export type { ArticleListFilter };

interface EntryListProps {
  pubId?: string;
  aggregateFeed?: AggregateAppViewFeed;
  selectedEntryId: string | null;
  /** Entry whose destination is being resolved from the PDS after a click. */
  resolvingEntryId?: string | null;
  onSelectEntry: (entryId: string, entry?: EntryListItem) => void;
  isEntryRead: (entryId: string) => boolean;
  pendingEntryReadState?: (entryId: string) => boolean | undefined;
  readIndicatorsEnabled: boolean;
  /** When false, read/unread visuals are suppressed without changing persisted state. */
  articleFilter: ArticleListFilter;
  markEntryRead: (entryId: string) => void;
  markEntryUnread: (entryId: string) => void;
}

function virtualFeedIdentity(
  aggregateFeed: AggregateAppViewFeed | undefined,
  pubId: string | undefined,
): string {
  return pubId ?? `${aggregateFeed?.kind}:${aggregateFeed?.id ?? ""}`;
}

export function EntryList({
  pubId,
  aggregateFeed,
  selectedEntryId,
  resolvingEntryId = null,
  onSelectEntry,
  isEntryRead,
  pendingEntryReadState,
  readIndicatorsEnabled,
  articleFilter,
  markEntryRead,
  markEntryUnread,
}: EntryListProps) {
  const effectiveFilter: ArticleListFilter = useMemo(() => {
    if (!readIndicatorsEnabled) return "all";
    return articleFilter;
  }, [readIndicatorsEnabled, articleFilter]);
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const isScrolling = useScrollActivity(scrollContainerRef);
  // Scroll anchoring keeps the visible article in place when rows land above it,
  // so background refreshes only wait out an active scroll gesture.
  const canBackgroundRefresh = useCallback(() => !isScrolling(), [isScrolling]);

  // AppView performs the bounded unread scan. The local read-state overlay still
  // suppresses rows immediately while read-mark write-through catches up.
  const {
    data,
    isLoading,
    isFetching: publicationQueryIsFetching,
    isError,
    error,
    refetch,
    refreshInBackground,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    scopePending,
  } = useEntries(pubId ?? null, effectiveFilter, canBackgroundRefresh);
  const aggregateQuery = useAggregateFeedEntries(
    aggregateFeed ?? null,
    effectiveFilter,
    canBackgroundRefresh,
  );
  const activeData = aggregateFeed
    ? aggregateQuery.data
    : data;
  const activeIsFetching = aggregateFeed
    ? aggregateQuery.isFetching
    : publicationQueryIsFetching;
  const activeLoading = aggregateFeed
    ? aggregateQuery.isLoading
    : isLoading;
  const activeIsError = aggregateFeed
    ? aggregateQuery.isError
    : isError;
  const activeError = aggregateFeed
    ? aggregateQuery.error
    : error;
  const activeRefetch = aggregateFeed
    ? aggregateQuery.refetch
    : refetch;
  const activeFetchNextPage = aggregateFeed
    ? aggregateQuery.fetchNextPage
    : fetchNextPage;
  const activeHasNextPage = aggregateFeed
    ? aggregateQuery.hasNextPage
    : hasNextPage;
  const activeIsFetchingNextPage = aggregateFeed
    ? aggregateQuery.isFetchingNextPage
    : isFetchingNextPage;
  const activeIsFetchNextPageError = aggregateFeed
    ? aggregateQuery.isFetchNextPageError
    : isFetchNextPageError;
  const { allPublicationRows } = useSidebarProjection();

  const publicationById = useMemo(() => {
    const map = new Map<
      string,
      { name: string; faviconUrl?: string }
    >();
    for (const publication of allPublicationRows) {
      map.set(publication.publicationId, {
        name: publication.title,
        faviconUrl: publication.iconUrl ?? publication.avatarUrl ?? undefined,
      });
    }
    return map;
  }, [allPublicationRows]);

  useProactiveFeedRefresh(
    pubId ?? null,
    effectiveFilter,
    !aggregateFeed &&
      !isLoading &&
      !scopePending &&
      (data?.pages.length ?? 0) > 0,
    refreshInBackground,
  );

  const aggregateAccumulator = useMemo(() => {
    if (!aggregateFeed) return undefined;
    return mergeAggregateEntryPagesCached(aggregateQuery.data?.pages ?? []);
  }, [aggregateFeed, aggregateQuery.data?.pages]);
  const aggregateEntries = aggregateAccumulator?.entries ?? [];

  const publicationEntries: EntryListItem[] = useMemo(() => {
    const flat = dedupeEntryListItems(
      data?.pages.flatMap((p) => p.entries) ?? [],
    );
    return sortEntryListItemsNewestFirst(flat);
  }, [data?.pages]);

  const allEntries = aggregateFeed ? aggregateEntries : publicationEntries;

  const previousAggregateEntryCountRef = useRef({
    feedKey: virtualFeedIdentity(aggregateFeed, pubId),
    count: 0,
  });
  useLayoutEffect(() => {
    if (aggregateFeed?.kind !== "subscribed") return;
    const feedKey = virtualFeedIdentity(aggregateFeed, pubId);
    const previous =
      previousAggregateEntryCountRef.current.feedKey === feedKey
        ? previousAggregateEntryCountRef.current.count
        : 0;
    const appendedRows =
      aggregateEntries.length - previous;
    previousAggregateEntryCountRef.current = {
      feedKey,
      count: aggregateEntries.length,
    };
    markSubscribedRowsRendered({
      appendedRows,
      mergeDurationMs: aggregateAccumulator?.mergeDurationMs ?? 0,
    });
  }, [aggregateAccumulator, aggregateFeed, aggregateEntries.length, pubId]);

  const visibleEntries: EntryListItem[] = useMemo(() => {
    const authoritativeReadIds = new Set(
      allEntries.filter((entry) => entry.isRead).map((entry) => entry.entryId)
    );
    return filterEntriesForArticleFilter(
      allEntries,
      effectiveFilter,
      (entryId) => effectiveEntryReadState(entryId, authoritativeReadIds.has(entryId), isEntryRead, pendingEntryReadState)
    );
  }, [allEntries, effectiveFilter, isEntryRead, pendingEntryReadState]);

  /** Remount only when the user changes publication/filter; data churn must not reset scroll. */
  const virtualPaneKey = useMemo(() => {
    return `${virtualFeedIdentity(aggregateFeed, pubId)}:${effectiveFilter}`;
  }, [aggregateFeed, pubId, effectiveFilter]);

  const nextPageRequestRef = useRef(new CursorSingleFlight());

  useEffect(() => {
    nextPageRequestRef.current.reset();
  }, [virtualPaneKey]);

  const fetchNextPageOnce = useCallback((): Promise<unknown> => {
    const cursor =
      activeData?.pages[activeData.pages.length - 1]?.cursor ?? "__first_page__";
    if (!activeHasNextPage || activeIsFetchingNextPage) {
      return Promise.resolve();
    }

    return nextPageRequestRef.current.run({
      feedKey: virtualPaneKey,
      cursor,
      request: () => {
        if (aggregateFeed?.kind === "subscribed") {
          markSubscribedPaginationTriggered();
        }
        return activeFetchNextPage();
      },
    });
  }, [
    activeData,
    activeFetchNextPage,
    activeHasNextPage,
    activeIsFetchingNextPage,
    aggregateFeed?.kind,
    virtualPaneKey,
  ]);

  // An empty page can still carry a continuation cursor. Scan independently of
  // virtual row measurements, which may retain an old feed's scroll offset.
  useEffect(() => {
    if (
      visibleEntries.length === 0 &&
      activeHasNextPage &&
      !activeIsFetching &&
      !activeIsError
    ) {
      void fetchNextPageOnce();
    }
  }, [
    visibleEntries.length,
    activeData?.pages.length,
    activeHasNextPage,
    activeIsFetching,
    activeIsError,
    fetchNextPageOnce,
  ]);

  if (
    (activeLoading || (!aggregateFeed && scopePending)) &&
    allEntries.length === 0
  ) {
    return (
      <div className="space-y-1.5 p-2">
        {Array.from({ length: 6 }).map((_, i) => (
          <Skeleton key={i} className="h-36 w-full rounded-lg" />
        ))}
      </div>
    );
  }

  if (activeIsError && visibleEntries.length === 0) {
    return (
      <div role="alert" className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center text-sm text-muted-foreground">
        <p>
          {activeError instanceof Error
            ? activeError.message
            : "Could not load entries."}
        </p>
        <button
          type="button"
          disabled={activeIsFetching}
          aria-busy={activeIsFetching}
          className="inline-flex min-h-11 items-center gap-2 text-primary underline-offset-4 hover:underline"
          onClick={() => void activeRefetch().catch(() => undefined)}
        >
          {activeIsFetching ? <RefreshCw aria-hidden="true" className="size-4 animate-spin" /> : null}
          <span role="status" aria-live="polite">{activeIsFetching ? "Retrying…" : "Retry"}</span>
        </button>
      </div>
    );
  }

  if (visibleEntries.length === 0 && (activeHasNextPage || activeIsFetching)) {
    return (
      <div role="status" className="flex h-full items-center justify-center p-8 text-center text-sm text-muted-foreground">
        {effectiveFilter === "unread" ? "Checking for unread stories…" : "Loading stories…"}
      </div>
    );
  }

  if (allEntries.length === 0) {
    return (
      <div className="flex h-full items-center justify-center p-8 text-center text-sm text-muted-foreground">
        {effectiveFilter === "unread"
          ? "No unread stories. You're all caught up."
          : "No entries found for this publication."}
      </div>
    );
  }

  if (
    shouldShowNoUnreadEntriesState({
      effectiveFilter,
      visibleEntryCount: visibleEntries.length,
      hasNextPage: activeHasNextPage,
      isFetchingNextPage: activeIsFetchingNextPage,
    })
  ) {
    return (
      <div className="flex h-full items-center justify-center p-8 text-center text-sm text-muted-foreground">
        No unread stories. You&apos;re all caught up.
      </div>
    );
  }

  return (
    <EntryListVirtualPane
      key={virtualPaneKey}
      visibleEntries={visibleEntries}
      selectedEntryId={selectedEntryId}
      resolvingEntryId={resolvingEntryId}
      onSelectEntry={onSelectEntry}
      isEntryRead={isEntryRead}
      pendingEntryReadState={pendingEntryReadState}
      readIndicatorsEnabled={readIndicatorsEnabled}
      hasNextPage={activeHasNextPage}
      isFetchingNextPage={activeIsFetchingNextPage}
      isFetchNextPageError={activeIsFetchNextPageError}
      fetchNextPage={fetchNextPageOnce}
      scrollStateKey={virtualPaneKey}
      scrollContainerRef={scrollContainerRef}
      publicationById={publicationById}
      markEntryRead={markEntryRead}
      markEntryUnread={markEntryUnread}
    />
  );
}
