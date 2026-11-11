"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuth } from "./useAuth";
import type { PreferencesRecord, RepoRecord } from "@/lib/pdsClient";
import { fetchSyncPreferences } from "@/lib/syncPreferencesClient";
import { findReadLaterService } from "@/lib/readLaterServices";

export const ACCOUNT_PREFERENCES_QUERY_KEY = ["accountPreferences"] as const;
export const accountPreferencesQueryKey = (viewerDid?: string) => [...ACCOUNT_PREFERENCES_QUERY_KEY, viewerDid ?? "anonymous"] as const;

const DEFAULT_READ_LATER_SERVICE_ID = "latr-link" as const;

export function useAccountPreferences() {
  const { session, getOAuthSession } = useAuth();
  const viewerDid = session?.did;
  return useQuery({
    queryKey: accountPreferencesQueryKey(viewerDid),
    queryFn: async () => {
      const oauth = getOAuthSession();
      if (!oauth || !viewerDid || oauth.did !== viewerDid) throw new Error("Sign in again to load your preferences.");
      const record = await fetchSyncPreferences(oauth, viewerDid);
      if (getOAuthSession() !== oauth) throw new Error("Your account changed. Reload preferences.");
      return record;
    },
    select: record => {
      if (record && record.uri !== `at://${viewerDid}/app.thesocialwire.preferences/self`) throw new Error("These preferences belong to another account.");
      return record;
    },
    enabled: !!session,
    staleTime: 30_000,
    retry: 1,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
}

export function useConfiguredReadLaterService() {
  const prefs = useAccountPreferences();
  const configured = prefs.data?.value.readLaterService;
  const serviceId = configured === "semble" ? "semble" : DEFAULT_READ_LATER_SERVICE_ID;
  const sembleConnection = prefs.data?.value.readLaterConnections?.semble;

  return useMemo(
    () => ({
      isLoading: prefs.isLoading,
      data: prefs.data as RepoRecord<PreferencesRecord> | null | undefined,
      serviceId,
      service: findReadLaterService(serviceId),
      sembleConnection:
        serviceId === "semble" && sembleConnection?.collectionUri
          ? sembleConnection
          : null,
    }),
    [prefs.isLoading, prefs.data, sembleConnection, serviceId]
  );
}
