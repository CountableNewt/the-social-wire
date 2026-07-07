import type { QueryClient } from "@tanstack/react-query";

import { normalizeAtRepoParam } from "@/lib/atprotoClient";
import type { ParsedBootstrapStreamEvent } from "@/lib/bootstrapStreamModels";
import { dedupeEntryListItems } from "@/lib/rssFeedCore";
import {
  mergeSidebarProjections,
  publicationIdsFromProjection,
  type PublicationSidebarProjection,
} from "@/lib/publicationProjectionClient";
import {
  ENTRIES_QUERY_KEY,
  ENTRIES_QUERY_STALE_MS,
  type EntriesPage,
} from "@/hooks/useEntries";

type SidebarRow = PublicationSidebarProjection["allPublicationRows"][number];

export function applySidebarPriorityEvent(
  current: PublicationSidebarProjection | undefined,
  payload: PublicationSidebarProjection
): PublicationSidebarProjection {
  if (!current) return payload;

  const priorityRowsById = new Map<string, SidebarRow>();
  const addPriorityRow = (row: SidebarRow) => {
    priorityRowsById.set(normalizeAtRepoParam(row.publicationId), row);
  };

  for (const row of payload.allPublicationRows) addPriorityRow(row);
  for (const row of payload.myPublications) addPriorityRow(row);
  for (const row of payload.subscribedUnfoldered) addPriorityRow(row);
  for (const row of payload.followingTabPublications) addPriorityRow(row);
  for (const section of payload.folderSections ?? []) {
    for (const row of section.publications) addPriorityRow(row);
  }

  const mergeRow = (row: SidebarRow) => {
    const priority = priorityRowsById.get(
      normalizeAtRepoParam(row.publicationId)
    );
    if (!priority) return row;
    return {
      ...row,
      ...priority,
      unreadCount: priority.unreadCount ?? row.unreadCount,
    };
  };

  const appendNewPriorityRows = (rows: SidebarRow[]) => {
    const seen = new Set(
      rows.map((row) => normalizeAtRepoParam(row.publicationId))
    );
    const merged = rows.map(mergeRow);
    for (const row of payload.allPublicationRows) {
      const key = normalizeAtRepoParam(row.publicationId);
      if (seen.has(key)) continue;
      seen.add(key);
      merged.push(row);
    }
    return merged;
  };

  const mergeList = (
    currentRows: SidebarRow[],
    priorityRows: SidebarRow[]
  ) => {
    const seen = new Set(
      currentRows.map((row) => normalizeAtRepoParam(row.publicationId))
    );
    const merged = currentRows.map(mergeRow);
    for (const row of priorityRows) {
      const key = normalizeAtRepoParam(row.publicationId);
      if (seen.has(key)) continue;
      seen.add(key);
      merged.push(row);
    }
    return merged;
  };

  const unreadCountsByPublicationId = {
    ...(current.unreadCountsByPublicationId ?? {}),
    ...(payload.unreadCountsByPublicationId ?? {}),
  };

  return {
    ...current,
    refreshedAt: payload.refreshedAt,
    allPublicationRows: appendNewPriorityRows(current.allPublicationRows),
    myPublications: mergeList(current.myPublications, payload.myPublications),
    subscribedUnfoldered: mergeList(
      current.subscribedUnfoldered,
      payload.subscribedUnfoldered
    ),
    followingTabPublications: mergeList(
      current.followingTabPublications,
      payload.followingTabPublications
    ),
    folderSections: current.folderSections?.map((section) => ({
      ...section,
      publications: section.publications.map(mergeRow),
    })),
    enrollAuthorDids: [
      ...new Set([...current.enrollAuthorDids, ...payload.enrollAuthorDids]),
    ],
    unreadCountsByPublicationId,
  };
}

export function applyUnreadCountsEvent(
  projection: PublicationSidebarProjection,
  counts: Record<string, number>,
  options?: { replacePublicationIds?: readonly string[] }
): PublicationSidebarProjection {
  const unreadCountsByPublicationId = {
    ...(projection.unreadCountsByPublicationId ?? {}),
    ...counts,
  };

  if (options?.replacePublicationIds?.length) {
    for (const publicationId of options.replacePublicationIds) {
      const fresh = counts[publicationId] ?? 0;
      unreadCountsByPublicationId[publicationId] = Math.max(0, fresh);
    }
  }

  const applyRow = (
    row: PublicationSidebarProjection["allPublicationRows"][number]
  ) => {
    if (options?.replacePublicationIds?.includes(row.publicationId)) {
      const count = counts[row.publicationId] ?? 0;
      return { ...row, unreadCount: count > 0 ? count : 0 };
    }
    const count = counts[row.publicationId];
    if (count == null) return row;
    return { ...row, unreadCount: count };
  };

  return {
    ...projection,
    unreadCountsByPublicationId,
    allPublicationRows: projection.allPublicationRows.map(applyRow),
    myPublications: projection.myPublications.map(applyRow),
    subscribedUnfoldered: projection.subscribedUnfoldered.map(applyRow),
    followingTabPublications: projection.followingTabPublications.map(applyRow),
    folderSections: projection.folderSections?.map((section) => ({
      ...section,
      publications: section.publications.map(applyRow),
    })),
  };
}

export function applySidebarFoldersEvent(
  projection: PublicationSidebarProjection,
  payload: {
    folderSections: NonNullable<PublicationSidebarProjection["folderSections"]>;
    allPublicationRows: PublicationSidebarProjection["allPublicationRows"];
  }
): PublicationSidebarProjection {
  return mergeSidebarProjections(projection, {
    ...projection,
    folderSections: payload.folderSections,
    allPublicationRows: payload.allPublicationRows,
    folders: [],
    publicationPrefs: [],
    myPublications: [],
    subscribedUnfoldered: [],
    followingTabPublications: [],
    enrollAuthorDids: [],
    refreshedAt: projection.refreshedAt,
  });
}

export function writeStreamedEntriesPage(
  queryClient: QueryClient,
  payload: { publicationId: string; entries: EntriesPage["entries"]; cursor?: string },
  articleFilter: "all" | "unread" = "all"
): void {
  const publicationKey = normalizeAtRepoParam(payload.publicationId);
  const page: EntriesPage = {
    entries: dedupeEntryListItems(payload.entries),
    cursor: payload.cursor,
  };
  queryClient.setQueryData(
    [...ENTRIES_QUERY_KEY(publicationKey), articleFilter] as const,
    {
      pages: [page],
      pageParams: [undefined],
    }
  );
  queryClient.setQueryDefaults(
    [...ENTRIES_QUERY_KEY(publicationKey), articleFilter] as const,
    { staleTime: ENTRIES_QUERY_STALE_MS }
  );
}

export function applyBootstrapStreamEvent(args: {
  projection: PublicationSidebarProjection | undefined;
  event: ParsedBootstrapStreamEvent;
}): {
  projection: PublicationSidebarProjection | undefined;
  selectedPublicationId: string | null;
  streamError: string | null;
  streamDone: boolean;
} {
  let { projection } = args;
  let selectedPublicationId: string | null = null;
  let streamError: string | null = null;
  let streamDone = false;

  switch (args.event.kind) {
    case "sidebarPriority":
      projection = applySidebarPriorityEvent(projection, args.event.payload);
      break;
    case "unreadCounts":
      if (projection) {
        projection = applyUnreadCountsEvent(
          projection,
          args.event.payload.counts,
          {
            replacePublicationIds:
              args.event.payload.replacePublicationIds ??
              publicationIdsFromProjection(projection),
          }
        );
      }
      break;
    case "selectedPublication":
      selectedPublicationId = args.event.payload.publicationId;
      break;
    case "sidebarFolders":
      if (projection) {
        projection = applySidebarFoldersEvent(projection, args.event.payload);
      }
      break;
    case "error":
      streamError = args.event.payload.message;
      break;
    case "done":
      streamDone = true;
      break;
    case "warning":
    case "entriesPage":
      break;
  }

  return { projection, selectedPublicationId, streamError, streamDone };
}
