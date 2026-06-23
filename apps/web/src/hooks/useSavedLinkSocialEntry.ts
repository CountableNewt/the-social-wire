"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";

import { useAuth } from "@/hooks/useAuth";
import { getEntry, type EntryDetail } from "@/lib/atprotoClient";
import type { MergedLatrSave } from "@/lib/pdsClient";
import {
  latrSaveFallbackEntryDetail,
  originalEntryIdFromLatrSave,
} from "@/lib/savedLinkSocialTarget";

export const SAVED_LINK_SOCIAL_ENTRY_QUERY_KEY = (entryId: string | null) =>
  ["saved-link-social-entry", entryId ?? ""] as const;

export function useSavedLinkSocialEntry(row: MergedLatrSave | null): {
  entry: EntryDetail | null;
  isLoading: boolean;
} {
  const { getOAuthSession, session } = useAuth();
  const originalEntryId = row ? originalEntryIdFromLatrSave(row) : null;

  const entryQuery = useQuery({
    queryKey: SAVED_LINK_SOCIAL_ENTRY_QUERY_KEY(originalEntryId),
    queryFn: async () => {
      if (!originalEntryId) return null;
      const oauth = getOAuthSession();
      if (!oauth) throw new Error("OAuth session required");
      return getEntry(originalEntryId, oauth);
    },
    enabled: !!originalEntryId && !!session,
    staleTime: 5 * 60_000,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });

  const entry = useMemo(() => {
    if (!row) return null;
    if (entryQuery.data) return entryQuery.data;
    return latrSaveFallbackEntryDetail(row);
  }, [entryQuery.data, row]);

  return {
    entry,
    isLoading: !!originalEntryId && entryQuery.isLoading,
  };
}
