"use client";

import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";

import {
  effectivePublicationUnreadCount,
  lookupUnreadCountInMap,
} from "@/lib/unreadCounts";
import type { DiscoveredPublication } from "@/lib/atprotoClient";

/**
 * Per-publication unread counts merging AppView baseline with local read state
 * for cached feed rows (via {@link usePublicationSidebarData}).
 */
export function useSidebarUnreadCounts(
  publications: DiscoveredPublication[],
  unreadCountsByPublicationId: Map<string, number> | undefined,
  options?: {
    isEntryRead?: (entryId: string) => boolean;
  }
): Map<string, number> {
  const queryClient = useQueryClient();
  const isEntryRead = options?.isEntryRead;

  return useMemo(() => {
    const map = new Map<string, number>();
    for (const pub of publications) {
      const serverCount = unreadCountsByPublicationId
        ? lookupUnreadCountInMap(unreadCountsByPublicationId, pub.publicationId)
        : 0;
      map.set(
        pub.publicationId,
        isEntryRead
          ? effectivePublicationUnreadCount(
              serverCount,
              queryClient,
              pub.publicationId,
              isEntryRead,
              { capRaiseToServerCount: true }
            )
          : serverCount
      );
    }
    return map;
  }, [publications, unreadCountsByPublicationId, isEntryRead, queryClient]);
}
