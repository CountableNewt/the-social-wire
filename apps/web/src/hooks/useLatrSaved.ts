"use client";

import { useMemo } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { LatrSaveListState, MergedLatrSave } from "@/lib/pdsClient";
import { normalizeLatrHttpsUrl } from "@/lib/latrSavedUrls";
import { createReadLaterProvider } from "@/lib/readLaterProvider";
import { useAuth } from "./useAuth";
import { usePDSClient } from "./usePDSClient";

export const LATR_SAVED_QUERY_KEY = ["latrSavedHttps"] as const;
export const LATR_ARCHIVED_QUERY_KEY = ["latrArchivedHttps"] as const;

function useReadLaterProvider() {
  const client = usePDSClient();
  const { session, getOAuthSession } = useAuth();

  return useMemo(() => {
    if (!client || !session) return null;
    const oauthSession = getOAuthSession();
    if (!oauthSession) return null;
    return createReadLaterProvider(oauthSession, client, session.did);
  }, [client, session, getOAuthSession]);
}

function latrSavesQueryKey(state: LatrSaveListState) {
  return state === "archived" ? LATR_ARCHIVED_QUERY_KEY : LATR_SAVED_QUERY_KEY;
}

export function useLatrMergedHttpsSaves(state: LatrSaveListState = "active") {
  const client = usePDSClient();
  return useQuery({
    queryKey: latrSavesQueryKey(state),
    queryFn: async ({ signal }): Promise<MergedLatrSave[]> => {
      if (!client) return [];
      return client.listMergedLatrSaves({ state, signal });
    },
    enabled: !!client,
    staleTime: 15_000,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
}

function invalidateLatrSaveQueries(qc: ReturnType<typeof useQueryClient>) {
  void qc.invalidateQueries({ queryKey: LATR_SAVED_QUERY_KEY });
  void qc.invalidateQueries({ queryKey: LATR_ARCHIVED_QUERY_KEY });
}

export function useSaveHttpsReadLaterMutation() {
  const provider = useReadLaterProvider();
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async (params: {
      url: string;
      title?: string;
      excerpt?: string;
    }) => {
      if (!provider) throw new Error("No read-later provider — not signed in");
      return provider.saveHttpsUrl(params.url, {
        title: params.title,
        excerpt: params.excerpt,
      });
    },
    onSuccess: () => invalidateLatrSaveQueries(qc),
  });
}

export function useDeleteLatrSaveMutation() {
  const provider = useReadLaterProvider();
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async (itemRkey: string) => {
      if (!provider) throw new Error("No read-later provider — not signed in");
      return provider.deleteSaveItem(itemRkey);
    },
    onSuccess: () => invalidateLatrSaveQueries(qc),
  });
}

export function useArchiveLatrSaveMutation() {
  const provider = useReadLaterProvider();
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async (itemRkey: string) => {
      if (!provider) throw new Error("No read-later provider — not signed in");
      return provider.archiveSaveItem(itemRkey);
    },
    onSuccess: () => invalidateLatrSaveQueries(qc),
  });
}

export function useUnarchiveLatrSaveMutation() {
  const provider = useReadLaterProvider();
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async (itemRkey: string) => {
      if (!provider) throw new Error("No read-later provider — not signed in");
      return provider.unarchiveSaveItem(itemRkey);
    },
    onSuccess: () => invalidateLatrSaveQueries(qc),
  });
}

/** @deprecated Prefer useDeleteLatrSaveMutation. */
export function useDeleteHttpsReadLaterMutation() {
  const provider = useReadLaterProvider();
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async (normalizedUrl: string) => {
      if (!provider) throw new Error("No read-later provider — not signed in");
      return provider.deleteHttpsUrl(normalizedUrl);
    },
    onSuccess: () => invalidateLatrSaveQueries(qc),
  });
}

/** @deprecated Prefer useArchiveLatrSaveMutation. */
export function useArchiveHttpsReadLaterMutation() {
  const provider = useReadLaterProvider();
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async (normalizedUrl: string) => {
      if (!provider) throw new Error("No read-later provider — not signed in");
      return provider.archiveHttpsUrl(normalizedUrl);
    },
    onSuccess: () => invalidateLatrSaveQueries(qc),
  });
}

export function useSaveReadLaterEntryMutation() {
  const provider = useReadLaterProvider();
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async (params: {
      entryId: string;
      url?: string;
      title?: string;
      excerpt?: string;
    }) => {
      if (!provider) throw new Error("No read-later provider — not signed in");
      if (params.url?.trim()) {
        return provider.saveHttpsUrl(params.url, {
          title: params.title,
          excerpt: params.excerpt,
        });
      }
      return provider.saveNativeSubject(params.entryId, params.url);
    },
    onSuccess: () => invalidateLatrSaveQueries(qc),
  });
}

/**
 * Whether an entry is already in the active read-later list (HTTPS URL or native subject).
 */
export function useEntryIsLatrSaved(
  entryId: string,
  displayUrlHttps?: string | null
): boolean {
  const { data: merged } = useLatrMergedHttpsSaves("active");
  const normalizedUrl = displayUrlHttps?.trim()
    ? normalizeLatrHttpsUrl(displayUrlHttps)
    : null;
  return useMemo(() => {
    if (!merged?.length) return false;
    return merged.some((row) => {
      if (row.kind === "native" && row.subjectUri === entryId) return true;
      if (
        row.kind === "external" &&
        normalizedUrl &&
        row.normalizedUrl === normalizedUrl
      ) {
        return true;
      }
      return false;
    });
  }, [entryId, merged, normalizedUrl]);
}

/**
 * Client-only: whether merged read-later rows already include this HTTPS URL string.
 */
export function useHttpsUrlIsLatrSaved(displayUrlHttps: string | null | undefined): boolean {
  const { data: merged } = useLatrMergedHttpsSaves("active");
  const n = displayUrlHttps?.trim()
    ? normalizeLatrHttpsUrl(displayUrlHttps)
    : null;
  return useMemo(() => {
    if (!n || !merged?.length) return false;
    return merged.some((row) => row.kind === "external" && row.normalizedUrl === n);
  }, [n, merged]);
}
