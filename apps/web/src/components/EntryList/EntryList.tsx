"use client";

import { useEffect, useMemo } from "react";
import { Skeleton } from "@/components/ui/skeleton";
import { useEntries } from "@/hooks/useEntries";
import {
  sortEntryListItemsNewestFirst,
  type EntryListItem,
} from "@/lib/atprotoClient";
import {
  filterEntriesForArticleFilter,
  type ArticleListFilter,
} from "@/lib/entryArticleFilter";
import { EntryListVirtualPane } from "./EntryListVirtualPane";

export type { ArticleListFilter };

interface EntryListProps {
  pubId: string;
  selectedEntryId: string | null;
  onSelectEntry: (entryId: string) => void;
  isEntryRead: (entryId: string) => boolean;
  readIndicatorsEnabled: boolean;
  /** When false, read/unread visuals are suppressed without changing persisted state. */
  articleFilter: ArticleListFilter;
  markEntryRead: (entryId: string) => void;
  markEntryUnread: (entryId: string) => void;
}

export function EntryList({
  pubId,
  selectedEntryId,
  onSelectEntry,
  isEntryRead,
  readIndicatorsEnabled,
  articleFilter,
  markEntryRead,
  markEntryUnread,
}: EntryListProps) {
  const effectiveFilter: ArticleListFilter = useMemo(() => {
    if (!readIndicatorsEnabled) return "all";
    return articleFilter;
  }, [readIndicatorsEnabled, articleFilter]);

  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useEntries(pubId, effectiveFilter);

  const allEntries: EntryListItem[] = useMemo(() => {
    const flat = data?.pages.flatMap((p) => p.entries) ?? [];
    return sortEntryListItemsNewestFirst(flat);
  }, [data?.pages]);

  const visibleEntries: EntryListItem[] = useMemo(() => {
    return filterEntriesForArticleFilter(
      allEntries,
      effectiveFilter,
      isEntryRead
    );
  }, [allEntries, effectiveFilter, isEntryRead]);

  /** Unread: remount when membership changes (mark read removes a row). All: stable per pub + filter only. */
  const virtualPaneKey = useMemo(() => {
    if (effectiveFilter === "unread") {
      return `${pubId}:unread:${visibleEntries.map((e) => e.entryId).join("\x1e")}`;
    }
    return `${pubId}:${effectiveFilter}`;
  }, [pubId, effectiveFilter, visibleEntries]);

  useEffect(() => {
    if (effectiveFilter !== "unread" || !readIndicatorsEnabled) return;
    if (!hasNextPage || isFetchingNextPage) return;
    if (visibleEntries.length > 0) return;
    if (allEntries.length === 0 || isLoading) return;
    void fetchNextPage();
  }, [
    effectiveFilter,
    readIndicatorsEnabled,
    hasNextPage,
    isFetchingNextPage,
    visibleEntries.length,
    allEntries.length,
    isLoading,
    fetchNextPage,
  ]);

  if (isLoading && allEntries.length === 0) {
    return (
      <div className="space-y-2 p-4">
        {Array.from({ length: 6 }).map((_, i) => (
          <Skeleton key={i} className="h-28 w-full rounded-md" />
        ))}
      </div>
    );
  }

  if (allEntries.length === 0) {
    return (
      <div className="flex h-full items-center justify-center p-8 text-center text-sm text-muted-foreground">
        No entries found for this publication.
      </div>
    );
  }

  if (
    effectiveFilter === "unread" &&
    visibleEntries.length === 0 &&
    allEntries.length > 0 &&
    (hasNextPage || isFetchingNextPage)
  ) {
    return (
      <div className="space-y-2 p-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-28 w-full rounded-md" />
        ))}
      </div>
    );
  }

  if (effectiveFilter === "unread" && visibleEntries.length === 0) {
    return (
      <div className="flex h-full items-center justify-center p-8 text-center text-sm text-muted-foreground">
        No unread entries for this publication.
      </div>
    );
  }

  return (
    <EntryListVirtualPane
      key={virtualPaneKey}
      visibleEntries={visibleEntries}
      selectedEntryId={selectedEntryId}
      onSelectEntry={onSelectEntry}
      isEntryRead={isEntryRead}
      readIndicatorsEnabled={readIndicatorsEnabled}
      hasNextPage={hasNextPage}
      isFetchingNextPage={isFetchingNextPage}
      fetchNextPage={fetchNextPage}
      markEntryRead={markEntryRead}
      markEntryUnread={markEntryUnread}
    />
  );
}
