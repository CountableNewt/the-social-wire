"use client";

import { useMemo } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { MergedLatrSave } from "@/lib/pdsClient";
import { normalizeLatrHttpsUrl } from "@/lib/latrSavedUrls";
import { usePDSClient } from "./usePDSClient";

export const LATR_SAVED_QUERY_KEY = ["latrSavedHttps"] as const;

export function useLatrMergedHttpsSaves() {
  const client = usePDSClient();
  return useQuery({
    queryKey: LATR_SAVED_QUERY_KEY,
    queryFn: async (): Promise<MergedLatrSave[]> => {
      if (!client) return [];
      return client.listMergedLatrSaves();
    },
    enabled: !!client,
    staleTime: 15_000,
  });
}

export function useSaveHttpsReadLaterMutation() {
  const client = usePDSClient();
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async (params: {
      url: string;
      title?: string;
      excerpt?: string;
    }) => {
      if (!client) throw new Error("No PDS client — not signed in");
      return client.saveHttpsReadLater(params.url, {
        title: params.title,
        excerpt: params.excerpt,
      });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: LATR_SAVED_QUERY_KEY }),
  });
}

export function useDeleteHttpsReadLaterMutation() {
  const client = usePDSClient();
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async (normalizedUrl: string) => {
      if (!client) throw new Error("No PDS client — not signed in");
      return client.deleteHttpsReadLater(normalizedUrl);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: LATR_SAVED_QUERY_KEY }),
  });
}

export function useArchiveHttpsReadLaterMutation() {
  const client = usePDSClient();
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async (normalizedUrl: string) => {
      if (!client) throw new Error("No PDS client — not signed in");
      return client.archiveHttpsReadLater(normalizedUrl);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: LATR_SAVED_QUERY_KEY }),
  });
}

/**
 * Client-only: whether merged read-later rows already include this HTTPS URL string.
 */
export function useHttpsUrlIsLatrSaved(displayUrlHttps: string | null | undefined): boolean {
  const { data: merged } = useLatrMergedHttpsSaves();
  const n = displayUrlHttps?.trim()
    ? normalizeLatrHttpsUrl(displayUrlHttps)
    : null;
  return useMemo(() => {
    if (!n || !merged?.length) return false;
    return merged.some((row) => row.kind === "external" && row.normalizedUrl === n);
  }, [n, merged]);
}
