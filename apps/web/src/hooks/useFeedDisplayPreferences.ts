"use client";

import { isRetiredReaderFeed } from "@/lib/retiredReaderFeeds";

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import {
  accountPreferencesQueryKey,
  useAccountPreferences,
} from "@/hooks/useReadLaterPreferences";
import { usePDSClient } from "@/hooks/usePDSClient";
import { useAuth } from "@/hooks/useAuth";
import type { PDSClient, PreferencesRecord, RepoRecord } from "@/lib/pdsClient";
import { isDummyReaderDataEnabled } from "@/lib/dummyReaderData";
import { fetchSyncPreferences } from "@/lib/syncPreferencesClient";
import {
  DEFAULT_FEED_DISPLAY_PREFERENCES,
  TOP_LEVEL_FEEDS,
  loadCachedFeedDisplayPreferences,
  normalizeFeedDisplayPreferences,
  saveCachedFeedDisplayPreferences,
  type FeedDisplayPreferences,
  type TopLevelFeed,
} from "@/lib/feedPreferences";

type PreferenceMutation = {
  next: FeedDisplayPreferences;
  viewerDid?: string;
  oauth: ReturnType<ReturnType<typeof useAuth>["getOAuthSession"]>;
  client: PDSClient | null;
  previousPreferences: FeedDisplayPreferences;
  policyReady: boolean;
};

export function useFeedDisplayPreferences() {
  const { session, getOAuthSession } = useAuth();
  const client = usePDSClient();
  const queryClient = useQueryClient();
  const accountPreferences = useAccountPreferences();
  const viewerDid = session?.did;
  const auth = useRef({ viewerDid, getOAuthSession });
  useLayoutEffect(() => { auth.current = { viewerDid, getOAuthSession }; }, [viewerDid, getOAuthSession]);
  const [optimistic, setOptimistic] = useState<{ viewerDid?: string; value: FeedDisplayPreferences } | null>(null);
  const [cached, setCached] = useState<{ viewerDid: string; value: FeedDisplayPreferences } | null>(null);

  useEffect(() => {
    let cancelled = false;
    queueMicrotask(() => {
      if (cancelled) return;
      const value = viewerDid && typeof window !== "undefined" ? loadCachedFeedDisplayPreferences(window.localStorage, viewerDid) : null;
      setCached(value && viewerDid ? { viewerDid, value } : null);
    });
    return () => { cancelled = true; };
  }, [viewerDid]);

  const serverRecord = accountPreferences.data;
  const serverPreferences = serverRecord?.uri === `at://${viewerDid}/app.thesocialwire.preferences/self` ? serverRecord.value : undefined;
  const currentOptimistic = optimistic && optimistic.viewerDid === viewerDid ? optimistic.value : null;
  const currentCached = cached && cached.viewerDid === viewerDid ? cached.value : null;
  const preferences = useMemo(
    () => currentOptimistic ?? (serverPreferences ? normalizeFeedDisplayPreferences(serverPreferences) : currentCached ?? DEFAULT_FEED_DISPLAY_PREFERENCES),
    [currentCached, currentOptimistic, serverPreferences],
  );
  const automaticReadReady = !viewerDid || currentOptimistic !== null || currentCached !== null || serverRecord === null || serverPreferences !== undefined;
  const isCurrentActor = (input: PreferenceMutation) => auth.current.viewerDid === input.viewerDid && auth.current.getOAuthSession() === input.oauth;

  useEffect(() => {
    if (!session || !serverPreferences || typeof window === "undefined") return;
    const normalized = normalizeFeedDisplayPreferences(serverPreferences);
    saveCachedFeedDisplayPreferences(
      window.localStorage,
      session.did,
      normalized,
    );
  }, [serverPreferences, session]);

  const mutation = useMutation({
    mutationFn: async (input: PreferenceMutation) => {
      const { next, viewerDid: actorDid, oauth, client: actorClient } = input;
      if (!isCurrentActor(input)) throw new Error("Your account changed. Reopen Settings.");
      if (isDummyReaderDataEnabled()) return null;
      if (!actorClient || !actorDid || !oauth || oauth.did !== actorDid) throw new Error("PDS session required");
      const existing = queryClient.getQueryData<RepoRecord<PreferencesRecord> | null>(accountPreferencesQueryKey(actorDid)) ?? null;
      const saved = await actorClient.upsertPreferences(
        {
          visibleFeeds: next.visibleFeeds,
          showWire: next.showWire,
          showCircle: next.showCircle,
          showFinance: next.showFinance,
          showSports: next.showSports,
          showSocial: next.showSocial,
          showPodcasts: next.showPodcasts,
          hideSportsScores: next.hideSportsScores,
          hideFinancePerformance: next.hideFinancePerformance,
          hideFinanceCrypto: next.hideFinanceCrypto,
          feedsWithUnreadCounts: next.feedsWithUnreadCounts,
          rssArticleOpenMode: next.rssArticleOpenMode,
          automaticallyMarkAsRead: next.automaticallyMarkAsRead,
          showTopLevelFeedUnreadCounts:
            next.feedsWithUnreadCounts.length > 0,
        },
        existing,
      );
      // Reads use the Gateway cache, while writes go directly to the PDS.
      // Rewarm it before a reload can replace the committed visibility settings.
      if (saved.uri !== `at://${actorDid}/app.thesocialwire.preferences/self`) throw new Error("These preferences belong to another account.");
      if (isCurrentActor(input)) {
        try {
          await fetchSyncPreferences(oauth, actorDid, undefined, true);
        } catch {
          // A cache refresh failure cannot undo a successful PDS write.
        }
      }
      return saved;
    },
    onMutate: async (input) => {
      const key = accountPreferencesQueryKey(input.viewerDid);
      await queryClient.cancelQueries({ queryKey: key });
      if (!isCurrentActor(input)) throw new Error("Your account changed. Reopen Settings.");
      if (!input.policyReady) throw new Error("Wait for your preferences to load before changing settings.");
      const previous = queryClient.getQueryData<RepoRecord<PreferencesRecord> | null>(key);
      if (previous && previous.uri !== `at://${input.viewerDid}/app.thesocialwire.preferences/self`) throw new Error("These preferences belong to another account.");
      const normalized = normalizeFeedDisplayPreferences(input.next);
      setOptimistic({ viewerDid: input.viewerDid, value: normalized });
      if (input.viewerDid && typeof window !== "undefined") {
        setCached({ viewerDid: input.viewerDid, value: normalized });
        saveCachedFeedDisplayPreferences(window.localStorage, input.viewerDid, normalized);
      }
      queryClient.setQueryData<RepoRecord<PreferencesRecord> | null>(key, old => ({
        uri: old?.uri ?? `at://${input.viewerDid ?? ""}/app.thesocialwire.preferences/self`,
        cid: old?.cid ?? "",
        value: {
          $type: "app.thesocialwire.preferences",
          createdAt: old?.value.createdAt ?? new Date().toISOString(),
          updatedAt: new Date().toISOString(),
          ...old?.value,
          ...normalized,
          showTopLevelFeedUnreadCounts: normalized.feedsWithUnreadCounts.length > 0,
        },
      }));
      return { previous, previousPreferences: input.previousPreferences, key };
    },
    onSuccess: (saved, input) => {
      if (saved) queryClient.setQueryData(accountPreferencesQueryKey(input.viewerDid), saved);
      setOptimistic(previous => previous?.viewerDid === input.viewerDid ? null : previous);
    },
    onError: (_error, input, context) => {
      setOptimistic(previous => previous?.viewerDid === input.viewerDid ? null : previous);
      if (context) {
        queryClient.setQueryData(context.key, context.previous ?? null);
        if (input.viewerDid && typeof window !== "undefined") {
          setCached(previous => auth.current.viewerDid === input.viewerDid ? { viewerDid: input.viewerDid!, value: context.previousPreferences } : previous);
          saveCachedFeedDisplayPreferences(window.localStorage, input.viewerDid, context.previousPreferences);
        }
      }
      void queryClient.invalidateQueries({ queryKey: accountPreferencesQueryKey(input.viewerDid) });
    },
  });
  const mutatePreferences = useCallback((next: FeedDisplayPreferences) => mutation.mutate({ next, viewerDid, oauth: getOAuthSession(), client, previousPreferences: preferences, policyReady: automaticReadReady }), [mutation, viewerDid, getOAuthSession, client, preferences, automaticReadReady]);

  const setFeedVisible = useCallback(
    (feed: TopLevelFeed, visible: boolean) => {
      const visibleFeeds = visible
        ? TOP_LEVEL_FEEDS.filter(
            (candidate) =>
              candidate === feed || preferences.visibleFeeds.includes(candidate),
          )
        : preferences.visibleFeeds.filter((candidate) => candidate !== feed);
      mutatePreferences({
        ...preferences,
        visibleFeeds,
        feedsWithUnreadCounts: visible
          ? preferences.feedsWithUnreadCounts
          : preferences.feedsWithUnreadCounts.filter(
              (candidate) => candidate !== feed,
            ),
      });
    },
    [mutatePreferences, preferences],
  );

  const setFeedUnreadCountVisible = useCallback(
    (feed: TopLevelFeed, show: boolean) => {
      if (!preferences.visibleFeeds.includes(feed)) return;
      const feedsWithUnreadCounts = show
        ? TOP_LEVEL_FEEDS.filter(
            (candidate) =>
              candidate === feed ||
              preferences.feedsWithUnreadCounts.includes(candidate),
          )
        : preferences.feedsWithUnreadCounts.filter(
            (candidate) => candidate !== feed,
          );
      mutatePreferences({
        ...preferences,
        feedsWithUnreadCounts,
      });
    },
    [mutatePreferences, preferences],
  );

  const setDiscoveryFeedVisible = useCallback(
    (feed: "wire" | "circle" | "finance" | "sports" | "social" | "podcasts", visible: boolean) => {
      if (isRetiredReaderFeed(feed)) return;
      mutatePreferences({
        ...preferences,
        [({ wire: "showWire", circle: "showCircle", finance: "showFinance", sports: "showSports", social: "showSocial", podcasts: "showPodcasts" } as const)[feed]]: visible,
      });
    },
    [mutatePreferences, preferences],
  );

  const setRssArticleOpenInReader = useCallback(
    (openInReader: boolean) => {
      mutatePreferences({
        ...preferences,
        rssArticleOpenMode: openInReader ? "reader" : "original",
      });
    },
    [mutatePreferences, preferences],
  );

  return {
    preferences,
    automaticReadReady,
    setFeedVisible,
    setDiscoveryFeedVisible,
    setFeedUnreadCountVisible,
    setRssArticleOpenInReader,
    setAutomaticallyMarkAsRead: (enabled: boolean) => mutatePreferences({ ...preferences, automaticallyMarkAsRead: enabled }),
    setHideSportsScores: (hidden: boolean) => mutatePreferences({ ...preferences, hideSportsScores: hidden }),
    setHideFinanceCrypto: (hidden: boolean) => mutatePreferences({ ...preferences, hideFinanceCrypto: hidden }),
    setHideFinancePerformance: (hidden: boolean) => mutatePreferences({ ...preferences, hideFinancePerformance: hidden }),
    isLoading: accountPreferences.isLoading,
    isPending: !automaticReadReady || mutation.isPending && mutation.variables?.viewerDid === viewerDid,
    error: mutation.variables?.viewerDid === viewerDid ? mutation.error : null,
  };
}
